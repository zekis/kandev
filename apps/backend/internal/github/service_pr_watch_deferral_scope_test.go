package github

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/synctest"
)

func configureDiscoveryGeneration(svc *Service, client Client, generation int64) {
	connection := &WorkspaceConnection{WorkspaceID: testWorkspaceID, Source: ConnectionSourcePAT, GitHubHost: defaultGitHubHost, Login: "owner", Status: ConnectionStatusActive, CredentialGeneration: generation}
	svc.resolver = NewCredentialResolver(testConnectionReader{workspaces: map[string]*WorkspaceConnection{testWorkspaceID: connection}}, nil)
	svc.resolver.SetAutomationProvider(testAutomationCredentialProvider{client: client})
}

// @covers AC-INTEGRATIONS-GITHUB-PR-DISCOVERY-HEALTH-001.5
func TestSearchingWatchDeferralCredentialReplacementFencesOldFollowup(t *testing.T) {
	_, svc, _, store := setupBatchedPollerTest(t)
	watches := seedSearchingWatches(t, store, "o", "r", 1)
	started, finish := make(chan struct{}), make(chan struct{})
	oldFixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
		if r.URL.Path == "/graphql" {
			return 200, branchAliasResponse(t, 1, nil)
		}
		close(started)
		<-finish
		return 403, `{"message":"old credential denied"}`
	}}
	oldClient, _ := newDiscoveryAdmissionClient(oldFixture)
	configureDiscoveryGeneration(svc, oldClient, 2)
	oldDone := make(chan error, 1)
	go func() {
		_, err := svc.SyncWorkspaceWatchesBatched(context.Background(), testWorkspaceID, watches)
		oldDone <- err
	}()
	<-started
	svc.invalidateWorkspaceCredential(testWorkspaceID)
	newFixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
		if r.URL.Path == "/graphql" {
			return 200, branchAliasResponse(t, 1, nil)
		}
		return 200, `{"fork":false}`
	}}
	newClient, _ := newDiscoveryAdmissionClient(newFixture)
	configureDiscoveryGeneration(svc, newClient, 3)
	results, err := svc.SyncWorkspaceWatchesBatched(context.Background(), testWorkspaceID, watches)
	close(finish)
	oldErr := <-oldDone
	if err != nil || oldErr != nil {
		t.Fatalf("new/old batch errors: %v / %v", err, oldErr)
	}
	if !results[0].DiscoveryResolved || results[0].SyncFailed {
		t.Fatal("replacement credential did not complete discovery")
	}
	scope := testAutomationScope(t, svc, testWorkspaceID)
	if health := svc.prDiscoveryHealth.snapshot(testWorkspaceID, scope, 3); health.State != PRDiscoveryHealthHealthy || health.CredentialGeneration != 3 {
		t.Fatalf("old follow-up affected new health: %+v", health)
	}
	if newFixture.count("/repos/o/r") != 1 {
		t.Fatal("replacement reused old metadata evidence")
	}
}

func TestSearchingWatchDeferralSharedAttemptPreservesTypedOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, svc, _, store := setupBatchedPollerTest(t)
		watches := seedSearchingWatches(t, store, "o", "r", 1)
		started, finish := make(chan struct{}), make(chan struct{})
		fixture := &discoveryHTTPFixture{respond: func(r *http.Request) (int, string) {
			close(started)
			<-finish
			return 200, branchAliasResponse(t, 1, nil)
		}}
		client, _ := newDiscoveryAdmissionClient(fixture)
		configureDiscoveryGeneration(svc, client, 3)
		if _, err := svc.resolveAutomationClient(context.Background(), testWorkspaceID, "", ""); err != nil {
			t.Fatal(err)
		}
		admission := client.rateAdmission
		ctx := WithNonBlockingGitHubAdmission(WithGitHubWorkClass(context.Background(), WorkClassBackground))
		release, err := admission.acquire(ctx, ResourceCore)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		type outcome struct {
			results []PRWatchSyncResult
			err     error
		}
		done := make(chan outcome, 2)
		run := func() {
			results, err := svc.SyncWorkspaceWatchesBatched(ctx, testWorkspaceID, watches)
			done <- outcome{results, err}
		}
		go run()
		<-started
		go run()
		synctest.Wait()
		close(finish)
		for range 2 {
			got := <-done
			if got.err != nil {
				t.Fatal(got.err)
			}
			if len(got.results) != 1 || got.results[0].DiscoveryResolved || got.results[0].SyncFailed {
				t.Fatal("shared deferral became completed success/failure")
			}
			if !errors.Is(got.results[0].discoveryErr, ErrBackgroundAdmissionDeferred) {
				t.Fatal("shared attempt lost typed deferral")
			}
		}
		if fixture.count("/graphql") != 1 {
			t.Fatal("joined attempt repeated provider query")
		}
		if reloadWatches(t, store, watches)[0].LastCheckedAt != nil {
			t.Fatal("shared deferral stamped target")
		}
	})
}
