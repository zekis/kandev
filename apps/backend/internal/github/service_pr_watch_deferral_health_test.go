package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestSearchingWatchDeferralOnDemandPreservesEligibility(t *testing.T) {
	_, svc, _, store := setupBatchedPollerTest(t)
	watch := seedSearchingWatches(t, store, "o", "r", 1)[0]
	fixture := &discoveryHTTPFixture{respond: func(*http.Request) (int, string) { return 200, branchAliasResponse(t, 1, nil) }}
	client, _ := newDiscoveryAdmissionClient(fixture)
	configureTestWorkspaceAuth(t, svc, client, testWorkspaceID)
	if _, err := svc.resolveAutomationClient(context.Background(), testWorkspaceID, "o", "r"); err != nil {
		t.Fatal(err)
	}
	admission := client.rateAdmission
	ctx := WithNonBlockingGitHubAdmission(WithGitHubWorkClass(context.Background(), WorkClassBackground))
	release, err := admission.acquire(ctx, ResourceCore)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := svc.triggerPRDetection(ctx, watch, watch.TaskID); !errors.Is(err, ErrBackgroundAdmissionDeferred) {
		t.Fatalf("expected typed deferral, got %v", err)
	}
	if got := reloadWatches(t, store, []*PRWatch{watch})[0]; got.LastCheckedAt != nil {
		t.Fatal("on-demand deferral stamped watch as checked")
	}
}

// @covers AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001.3
func TestSearchingWatchDeferralPreservesPriorFailureUntilCompleteEmpty(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, svc, _, store := setupBatchedPollerTest(t)
		watches := seedSearchingWatches(t, store, "o", "r", 1)
		fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
			if r.URL.Path == "/graphql" {
				return 200, branchAliasResponse(t, 1, nil)
			}
			return 200, `{"fork":false}`
		}}
		client, admission := newDiscoveryAdmissionClient(fixture)
		attempt, ok := svc.beginPRDiscoveryWatch(testWorkspaceID, "generation3", 3, watches[0])
		if !ok {
			t.Fatal("initial failure not admitted")
		}
		svc.finishPRDiscoveryWatchFailureCategory(testWorkspaceID, "generation3", 3, attempt, PRDiscoveryHealthUnavailable)
		svc.completePRDiscoveryWatchAttempt(attempt, nil, false, true, errors.New("provider unavailable"), false)
		svc.forgetPRDiscoveryWatchAttempt(testWorkspaceID, "generation3", 3, attempt)
		time.Sleep(time.Minute)
		before := svc.prDiscoveryHealth.snapshot(testWorkspaceID, "generation3", 3)
		ctx := WithNonBlockingGitHubAdmission(WithGitHubWorkClass(context.Background(), WorkClassBackground))
		release, err := admission.acquire(ctx, ResourceCore)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if _, err := svc.syncWatchesBatchedWithClient(ctx, client, "generation3", testWorkspaceID, 3, watches, false); err != nil {
			t.Fatal(err)
		}
		after := svc.prDiscoveryHealth.snapshot(testWorkspaceID, "generation3", 3)
		if after.FailedTargetCount != 1 || after.Revision != before.Revision || !after.LastFailureAt.Equal(*before.LastFailureAt) {
			t.Fatal("deferral changed prior failure evidence")
		}
		release()
		time.Sleep(2 * time.Second)
		if _, err := svc.syncWatchesBatchedWithClient(ctx, client, "generation3", testWorkspaceID, 3, reloadWatches(t, store, watches), false); err != nil {
			t.Fatal(err)
		}
		if got := svc.prDiscoveryHealth.snapshot(testWorkspaceID, "generation3", 3); got.State != PRDiscoveryHealthHealthy {
			t.Fatal("complete empty lookup did not clear prior failure")
		}
	})
}

func TestSearchingWatchDeferralRealProviderErrorsRemainFailures(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			_, svc, _, store := setupBatchedPollerTest(t)
			watches := seedSearchingWatches(t, store, "o", "r", 1)
			fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
				if r.URL.Path == "/graphql" {
					return 200, branchAliasResponse(t, 1, nil)
				}
				return code, `{"message":"fixture provider refusal"}`
			}}
			client, _ := newDiscoveryAdmissionClient(fixture)
			results, err := svc.syncWatchesBatchedWithClient(context.Background(), client, "generation3", testWorkspaceID, 3, watches, false)
			if err != nil {
				t.Fatal(err)
			}
			if !results[0].SyncFailed || results[0].DiscoveryResolved {
				t.Fatal("provider failure became successful empty")
			}
			if got := svc.prDiscoveryHealth.snapshot(testWorkspaceID, "generation3", 3); got.FailedTargetCount != 1 || got.Category != PRDiscoveryHealthUnavailable {
				t.Fatalf("lost real failure: %+v", got)
			}
		})
	}
}

func TestSearchingWatchDeferralCancellationDoesNotStampOrDegrade(t *testing.T) {
	_, svc, client, store := setupBatchedPollerTest(t)
	watches := seedSearchingWatches(t, store, "o", "r", 1)
	client.branchResponses = []string{branchAliasResponse(t, 1, nil)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.SyncWorkspaceWatchesBatched(ctx, testWorkspaceID, watches)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if got := reloadWatches(t, store, watches)[0]; got.LastCheckedAt != nil {
		t.Fatal("cancellation stamped watch")
	}
	if got := svc.prDiscoveryHealth.snapshot(testWorkspaceID, testAutomationScope(t, svc, testWorkspaceID), 1); got.FailedTargetCount != 0 {
		t.Fatal("cancellation degraded discovery")
	}
}

func TestSearchingWatchDeferralAfterTerminalPRRecoversAsEmpty(t *testing.T) {
	for _, state := range []string{"MERGED", "CLOSED"} {
		t.Run(state, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				_, svc, _, store := setupBatchedPollerTest(t)
				watches := seedSearchingWatches(t, store, "o", "r", 1)
				watch := watches[0]
				watch.PRNumber = 9
				if err := store.UpdatePRWatchPRNumber(context.Background(), watch.ID, 9); err != nil {
					t.Fatal(err)
				}
				fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
					if r.URL.Path != "/graphql" {
						return 200, `{"fork":false}`
					}
					payload, _ := io.ReadAll(r.Body)
					if strings.Contains(string(payload), "Branches") {
						return 200, branchAliasResponse(t, 1, nil)
					}
					return 200, fmt.Sprintf(`{"data":{"repo0":{"pr0":{"state":%q,"title":"terminal","url":"https://github.com/o/r/pull/9","headRefName":"feature/branch-0","headRefOid":"abc"}}}}`, state)
				}}
				client, admission := newDiscoveryAdmissionClient(fixture)
				if _, err := svc.syncWatchesBatchedWithClient(context.Background(), client, "generation3", testWorkspaceID, 3, watches, false); err != nil {
					t.Fatal(err)
				}
				watches = reloadWatches(t, store, watches)
				if watches[0].PRNumber != 0 {
					t.Fatal("terminal PR did not reset watch to searching")
				}
				time.Sleep(time.Minute)
				ctx := WithNonBlockingGitHubAdmission(WithGitHubWorkClass(context.Background(), WorkClassBackground))
				release, err := admission.acquire(ctx, ResourceCore)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				lastCheck := *watches[0].LastCheckedAt
				if _, err := svc.syncWatchesBatchedWithClient(ctx, client, "generation3", testWorkspaceID, 3, watches, false); err != nil {
					t.Fatal(err)
				}
				if got := reloadWatches(t, store, watches)[0]; !got.LastCheckedAt.Equal(lastCheck) {
					t.Fatal("deferred terminal-branch search changed check timestamp")
				}
				release()
				time.Sleep(2 * time.Second)
				results, err := svc.syncWatchesBatchedWithClient(ctx, client, "generation3", testWorkspaceID, 3, reloadWatches(t, store, watches), false)
				if err != nil {
					t.Fatal(err)
				}
				if !results[0].DiscoveryResolved || results[0].Found || results[0].SyncFailed {
					t.Fatal("terminal branch was not a successful empty result")
				}
			})
		})
	}
}
