package executor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/gitcredentials"
	"github.com/kandev/kandev/internal/githubauth"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

func TestRestoreWorkspaceGitEnvironment(t *testing.T) {
	for _, scenario := range []string{"managed", "explicit", "revoked", "missing repository", "foreign workspace", "wrong session"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newMockRepository()
			repo.tasks["task"] = &models.Task{ID: "task", WorkspaceID: "workspace"}
			repo.sessions["session"] = &models.TaskSession{ID: "session", TaskID: "task"}
			for _, id := range []string{"one", "two"} {
				repo.taskRepositories[id] = &models.TaskRepository{ID: id, TaskID: "task", RepositoryID: id}
				repo.repositories[id] = &models.Repository{ID: id, WorkspaceID: "workspace", Provider: "github", ProviderOwner: "acme", ProviderName: id}
			}
			issuer := &fakeGitCredentialLeaseReissuer{
				fakeGitHubCredentialLeaseIssuer: fakeGitHubCredentialLeaseIssuer{lease: gitcredentials.Lease{Token: "fresh-lease"}},
				capability:                      gitcredentials.ReissueCapability{Token: "fresh-capability"},
			}
			exec := newTestExecutor(t, &mockAgentManager{}, repo)
			exec.SetGitHubCredentialBroker(issuer, "https://broker.example/resolve")
			resolver, ok := any(exec).(interface {
				RestoreWorkspaceGitEnvironment(context.Context, string, string, string, map[string]string) (map[string]string, error)
			})
			require.True(t, ok, "restoration must use current task authorization")
			env := map[string]string{githubauth.CredentialLeaseEnv: "revoked-lease", githubauth.CredentialBrokerURLEnv: "https://old.example/resolve", "CUSTOM": "preserved"}
			switch scenario {
			case "explicit":
				env["GITHUB_TOKEN"] = "explicit-token"
			case "revoked":
				issuer.err = errors.New("connection revoked")
			case "missing repository":
				delete(repo.repositories, "two")
			case "foreign workspace":
				repo.repositories["two"].WorkspaceID = "foreign"
			case "wrong session":
				repo.sessions["session"].TaskID = "another-task"
			}
			got, err := resolver.RestoreWorkspaceGitEnvironment(context.Background(), "task", "session", "worktree", env)
			if scenario == "revoked" || scenario == "missing repository" || scenario == "foreign workspace" || scenario == "wrong session" {
				require.Error(t, err)
				require.Nil(t, got)
				if scenario != "revoked" {
					require.Zero(t, issuer.calls)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, "preserved", got["CUSTOM"])
			if scenario == "explicit" {
				require.Equal(t, "explicit-token", got["GITHUB_TOKEN"])
				require.Empty(t, got[githubauth.CredentialLeaseEnv])
				require.Zero(t, issuer.calls)
				return
			}
			var scopes []githubCredentialScope
			require.NoError(t, json.Unmarshal([]byte(got[githubauth.CredentialScopesEnv]), &scopes))
			require.Len(t, scopes, 2)
			for _, scope := range scopes {
				require.Equal(t, "fresh-lease", scope.Lease)
				require.Equal(t, "fresh-capability", scope.ReissueCapability)
				require.Equal(t, "task", scope.TaskID)
				require.Equal(t, "session", scope.SessionID)
				require.Contains(t, []string{"one", "two"}, scope.RepositoryID)
			}
			require.NotEmpty(t, got["GIT_CONFIG_COUNT"])
			require.Equal(t, "revoked-lease", env[githubauth.CredentialLeaseEnv], "input snapshot must not be mutated")
		})
	}
}
