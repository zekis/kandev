package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// discoveryHTTPFixture keeps the real token client and admission coordinator;
// only the external HTTP boundary is replaced.
type discoveryHTTPFixture struct {
	mu      sync.Mutex
	paths   []string
	respond func(*http.Request) (int, string)
}

func (f *discoveryHTTPFixture) transport(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.Path)
	code, body := f.respond(r)
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func (f *discoveryHTTPFixture) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, p := range f.paths {
		if p == path {
			count++
		}
	}
	return count
}

func newDiscoveryAdmissionClient(f *discoveryHTTPFixture) (*TokenClient, *RateAdmission) {
	coordinator := NewRateCoordinator(nil, nil)
	tracker, admission := coordinator.coordinate(defaultGitHubHost, AuthPrincipal{Kind: AuthPrincipalHuman, Login: "owner"}, nil)
	client := NewPATClient("test-token").WithRateTracker(tracker).withRateAdmission(admission)
	client.httpClient = &http.Client{Transport: roundTripperFunc(f.transport)}
	return client, admission
}

// @covers AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001.3
// @covers AC-INTEGRATIONS-GITHUB-PR-POLLING-002.4
func TestSearchingWatchDeferralPreservesEligibilityAndRecovers(t *testing.T) {
	for _, reason := range []string{"busy", "pacing"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				_, svc, _, store := setupBatchedPollerTest(t)
				watches := seedSearchingWatches(t, store, "o", "r", 4)
				fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
					if r.URL.Path == "/graphql" {
						return 200, branchAliasResponse(t, 4, nil)
					}
					return 200, `{"full_name":"o/r","fork":false}`
				}}
				client, admission := newDiscoveryAdmissionClient(fixture)
				ctx := WithNonBlockingGitHubAdmission(WithGitHubWorkClass(context.Background(), WorkClassBackground))
				release, err := admission.acquire(ctx, ResourceCore)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				if reason == "pacing" {
					release()
				}
				results, err := svc.syncWatchesBatchedWithClient(ctx, client, "generation3", testWorkspaceID, 3, watches, false)
				if err != nil {
					t.Fatal(err)
				}
				health := svc.prDiscoveryHealth.snapshot(testWorkspaceID, "generation3", 3)
				if health.FailedTargetCount != 0 {
					t.Fatalf("local %s deferral became %s for %d targets", reason, health.Category, health.FailedTargetCount)
				}
				for _, result := range results {
					if result.SyncFailed || result.DiscoveryResolved {
						t.Fatal("deferral reported failure or completed discovery")
					}
				}
				for _, watch := range reloadWatches(t, store, watches) {
					if watch.LastCheckedAt != nil {
						t.Fatal("deferred target was stamped as checked")
					}
				}
				if fixture.count("/repos/o/r") != 0 {
					t.Fatal("deferred metadata request reached HTTP")
				}
				release()
				time.Sleep(2 * time.Second) // Advances virtual time past coordinator pacing.
				results, err = svc.syncWatchesBatchedWithClient(ctx, client, "generation3", testWorkspaceID, 3, reloadWatches(t, store, watches), false)
				if err != nil {
					t.Fatal(err)
				}
				for _, result := range results {
					if !result.DiscoveryResolved || result.SyncFailed {
						t.Fatal("empty branch did not complete after admission resumed")
					}
				}
				if fixture.count("/repos/o/r") != 1 {
					t.Fatal("fork metadata was not cached across empty branches")
				}
				if fixture.count("/graphql") != 2 {
					t.Fatal("unexpected repeated branch query")
				}
				if health := svc.prDiscoveryHealth.snapshot(testWorkspaceID, "generation3", 3); health.State != PRDiscoveryHealthHealthy {
					t.Fatalf("health did not recover: %+v", health)
				}
			})
		})
	}
}

// @covers AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-001.2
func TestSearchingWatchDeferralForkMakesProgressAlongsideResolvedPR(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, svc, _, store := setupBatchedPollerTest(t)
		watches := seedSearchingWatches(t, store, "o", "r", 2)
		response := branchAliasResponse(t, 2, map[int]string{1: openPRNode(9, watches[1].Branch, "owner")})
		fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
			switch r.URL.Path {
			case "/graphql":
				return 200, response
			case "/repos/o/r":
				return 200, `{"full_name":"o/r","fork":true,"parent":{"full_name":"upstream/r"}}`
			case "/repos/upstream/r/pulls":
				return 200, `[]`
			default:
				return 200, `{"total_count":0,"workflow_runs":[]}`
			}
		}}
		client, _ := newDiscoveryAdmissionClient(fixture)
		ctx := WithNonBlockingGitHubAdmission(WithGitHubWorkClass(context.Background(), WorkClassBackground))
		for cycle := 0; cycle < 2; cycle++ {
			results, err := svc.syncWatchesBatchedWithClient(ctx, client, "generation3", testWorkspaceID, 3, watches, false)
			if err != nil {
				t.Fatal(err)
			}
			if !results[1].Found || results[1].SyncFailed {
				t.Fatal("resolved sibling was lost")
			}
			if results[0].SyncFailed {
				t.Fatal("local pacing became a provider failure")
			}
			if cycle == 1 && !results[0].DiscoveryResolved {
				t.Fatal("fork discovery starved behind workflow enrichment")
			}
			time.Sleep(time.Minute) // Next background poll in virtual time.
		}
		if fixture.count("/repos/o/r") != 1 || fixture.count("/repos/upstream/r/pulls") != 1 {
			t.Fatalf("metadata=%d parent=%d, want one of each", fixture.count("/repos/o/r"), fixture.count("/repos/upstream/r/pulls"))
		}
	})
}

func TestSearchingWatchDeferralDoesNotStarveLaterForkBranches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, svc, _, store := setupBatchedPollerTest(t)
		watches := seedSearchingWatches(t, store, "o", "r", 3)
		fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
			switch r.URL.Path {
			case "/graphql":
				return 200, branchAliasResponse(t, 3, nil)
			case "/repos/o/r":
				return 200, `{"full_name":"o/r","fork":true,"parent":{"full_name":"upstream/r"}}`
			default:
				return 200, `[]`
			}
		}}
		client, _ := newDiscoveryAdmissionClient(fixture)
		ctx := WithNonBlockingGitHubAdmission(WithGitHubWorkClass(context.Background(), WorkClassBackground))
		for range 4 {
			if _, err := svc.syncWatchesBatchedWithClient(ctx, client, "generation3", testWorkspaceID, 3, watches, false); err != nil {
				t.Fatal(err)
			}
			watches = reloadWatches(t, store, watches)
			time.Sleep(time.Minute)
		}
		for _, watch := range watches {
			if watch.LastCheckedAt == nil {
				t.Fatalf("branch %s never completed despite available admission each cycle", watch.Branch)
			}
		}
		if fixture.count("/repos/o/r") != 1 {
			t.Fatal("completed fork metadata was repeated")
		}
	})
}

func TestSearchingWatchDeferralDoesNotEnablePerWatchFallback(t *testing.T) {
	if prWatchBatchFallbackAllowed(&AdmissionDeferredError{Reason: rateLimitBlockBackgroundPacing}) {
		t.Fatal("local deferral allowed per-watch fallback")
	}
}
