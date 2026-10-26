package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kandev/kandev/internal/events"
	"github.com/kandev/kandev/internal/events/bus"
)

func TestSearchingWatchDeferralMixedBatchPublishesLinkedPR(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		poller, svc, _, store := setupBatchedPollerTest(t)
		watches := seedSearchingWatches(t, store, "o", "r", 2)
		watches[1].PRNumber = 9
		watches[1].LastCheckStatus = "pending"
		if err := store.UpdatePRWatchPRNumber(context.Background(), watches[1].ID, 9); err != nil {
			t.Fatal(err)
		}
		fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
			payload, _ := io.ReadAll(r.Body)
			if strings.Contains(string(payload), "Branches") {
				return 200, branchAliasResponse(t, 1, nil)
			}
			return 200, `{"data":{"repo0":{"pr0":{"state":"OPEN","title":"linked","url":"https://github.com/o/r/pull/9","headRefName":"feature/branch-1","headRefOid":"abc","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}}}`
		}}
		client, _ := newDiscoveryAdmissionClient(fixture)
		configureDiscoveryGeneration(svc, client, 3)
		if _, err := svc.resolveAutomationClient(context.Background(), testWorkspaceID, "", ""); err != nil {
			t.Fatal(err)
		}
		ctx := WithNonBlockingGitHubAdmission(WithGitHubWorkClass(context.Background(), WorkClassBackground))
		release, err := client.rateAdmission.acquire(ctx, ResourceCore)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		eventSeen := make(chan struct{}, 1)
		sub, err := poller.eventBus.Subscribe(events.GitHubPRFeedback, func(context.Context, *bus.Event) error { eventSeen <- struct{}{}; return nil })
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sub.Unsubscribe() }()
		first := poller.tryBatchedPRWatchCheck(ctx, watches)
		if len(fallbackEligiblePRWatches(first)) != 0 {
			t.Fatal("batch admission deferral enabled fallback")
		}
		time.Sleep(2 * time.Second)
		outcomes := poller.tryBatchedPRWatchCheck(ctx, watches)
		if len(outcomes) != 1 || outcomes[0].state != prWatchBatchDeferred {
			t.Fatal("partially deferred batch claimed complete")
		}
		if len(fallbackEligiblePRWatches(outcomes)) != 0 {
			t.Fatal("partial deferral enabled fallback")
		}
		updated, err := store.GetTaskPR(context.Background(), watches[1].TaskID)
		if err != nil || updated == nil || updated.ChecksState != "success" {
			t.Fatalf("linked PR did not persist success (err=%v)", err)
		}
		select {
		case <-eventSeen:
		case <-time.After(time.Second):
			t.Fatal("linked PR feedback was lost")
		}
		if reloadWatches(t, store, watches)[0].LastCheckedAt != nil {
			t.Fatal("deferred branch was stamped")
		}
	})
}

func TestSearchingWatchDeferralPartialMissingRepositoryKeepsSuccessfulEmpty(t *testing.T) {
	_, svc, _, store := setupBatchedPollerTest(t)
	watches := seedSearchingWatches(t, store, "o", "r", 2)
	watches[1].Repo = "missing"
	response := `{"data":{"b0":{"pullRequests":{"nodes":[]}},"b1":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository with the name 'o/missing'.","path":["b1"]}]}`
	fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
		if r.URL.Path == "/graphql" {
			return 200, response
		}
		return 200, `{"fork":false}`
	}}
	client, _ := newDiscoveryAdmissionClient(fixture)
	results, err := svc.syncWatchesBatchedWithClient(context.Background(), client, "generation3", testWorkspaceID, 3, watches, false)
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].DiscoveryResolved || results[0].SyncFailed || !results[1].SyncFailed || results[1].DiscoveryResolved {
		t.Fatal("partial missing repository contaminated successful empty result")
	}
	if health := svc.prDiscoveryHealth.snapshot(testWorkspaceID, "generation3", 3); health.FailedTargetCount != 1 {
		t.Fatalf("failed targets=%d, want only missing repository", health.FailedTargetCount)
	}
}
