package executor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kandev/kandev/internal/gitcredentials"
	"github.com/kandev/kandev/internal/githubauth"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

type restorationBrokerAuthority struct {
	allowed bool
	binding string
}

func (a *restorationBrokerAuthority) Supports(provider string) bool { return provider == "github" }
func (a *restorationBrokerAuthority) Binding(context.Context, gitcredentials.Scope) (string, error) {
	return a.binding, nil
}
func (a *restorationBrokerAuthority) Resolve(context.Context, gitcredentials.Scope) (gitcredentials.Credential, error) {
	return gitcredentials.Credential{Username: "synthetic", Password: "synthetic"}, nil
}
func (a *restorationBrokerAuthority) AuthorizeGitCredential(_ context.Context, s gitcredentials.Scope) error {
	if !a.allowed || s.TaskID != "task" || s.SessionID != "session" || s.RepositoryID != "repository" || s.WorkspaceID != "workspace" {
		return gitcredentials.ErrScopeDenied
	}
	return nil
}

func TestRestoredWorkspaceUsesNewBrokerAndReissuesOnlyAuthorizedScope(t *testing.T) {
	ctx := context.Background()
	repo := newMockRepository()
	repo.tasks["task"] = &models.Task{ID: "task", WorkspaceID: "workspace"}
	repo.sessions["session"] = &models.TaskSession{ID: "session", TaskID: "task"}
	repo.taskRepositories["binding"] = &models.TaskRepository{ID: "binding", TaskID: "task", RepositoryID: "repository"}
	repository := &models.Repository{ID: "repository", WorkspaceID: "workspace", Provider: "github", ProviderOwner: "acme", ProviderName: "widgets"}
	repo.repositories[repository.ID] = repository
	authority := &restorationBrokerAuthority{allowed: true, binding: "generation-1"}
	signer, err := gitcredentials.NewReissueCapabilitySigner("synthetic-test-key-only")
	require.NoError(t, err)
	newBroker := func() *gitcredentials.Broker {
		broker := gitcredentials.NewBroker(authority, authority)
		broker.SetReissueCapabilitySigner(signer)
		return broker
	}
	exec := newTestExecutor(t, &mockAgentManager{}, repo)
	exec.SetGitHubCredentialBroker(newBroker(), "https://broker.example/resolve")
	fresh := &LaunchAgentRequest{TaskID: "task", SessionID: "session", WorkspaceID: "workspace", ExecutorType: "worktree"}
	require.NoError(t, exec.configureGitCredentialBrokerForRepositories(ctx, fresh, []*repoInfo{{RepositoryID: repository.ID, Repository: repository}}))
	oldLease := fresh.Env[githubauth.CredentialLeaseEnv]
	broker := newBroker()
	exec.SetGitHubCredentialBroker(broker, "https://broker.example/resolve")
	restored, err := exec.RestoreWorkspaceGitEnvironment(ctx, "task", "session", "worktree", nil)
	require.NoError(t, err)
	require.NotEqual(t, oldLease, restored[githubauth.CredentialLeaseEnv])
	var scopes []githubCredentialScope
	require.NoError(t, json.Unmarshal([]byte(restored[githubauth.CredentialScopesEnv]), &scopes))
	require.Len(t, scopes, 1)
	scope := scopes[0]
	redemption := gitcredentials.Redemption{Lease: scope.Lease, TaskID: scope.TaskID, SessionID: scope.SessionID, RepositoryID: scope.RepositoryID, Host: scope.Host, Path: scope.Path}
	_, err = broker.Redeem(ctx, redemption)
	require.NoError(t, err)
	redemption.Path = "/acme/unrelated.git"
	_, err = broker.Redeem(ctx, redemption)
	require.ErrorIs(t, err, gitcredentials.ErrScopeDenied)
	redemption.Path = scope.Path
	redemption.Lease = oldLease
	_, err = broker.Redeem(ctx, redemption)
	require.ErrorIs(t, err, gitcredentials.ErrLeaseInvalid)
	restarted := newBroker()
	reissue := gitcredentials.ReissueRequest{Capability: scope.ReissueCapability, TaskID: scope.TaskID, SessionID: scope.SessionID, RepositoryID: scope.RepositoryID, Host: scope.Host, Path: scope.Path}
	_, err = restarted.Reissue(ctx, reissue)
	require.NoError(t, err)
	reissue.RepositoryID = "unrelated"
	_, err = restarted.Reissue(ctx, reissue)
	require.Error(t, err)
	authority.allowed = false
	_, err = exec.RestoreWorkspaceGitEnvironment(ctx, "task", "session", "worktree", nil)
	require.ErrorIs(t, err, gitcredentials.ErrScopeDenied)
	authority.allowed = true
	authority.binding = ""
	_, err = exec.RestoreWorkspaceGitEnvironment(ctx, "task", "session", "worktree", nil)
	require.ErrorIs(t, err, gitcredentials.ErrLeaseRevoked)
}
