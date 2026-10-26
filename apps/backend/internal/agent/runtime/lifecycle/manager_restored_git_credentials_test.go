package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/githubauth"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

func TestRestoredWorkspaceReauthorizesGitBeforeRuntimeCreation(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "authorized", true: "revoked"}[denied], func(t *testing.T) {
			mgr := newTestManager(t)
			setter, ok := any(mgr).(interface {
				SetWorkspaceGitEnvironmentResolver(func(context.Context, string, string, string, map[string]string) (map[string]string, error))
			})
			require.True(t, ok, "workspace restoration must reauthorize managed Git credentials")
			setter.SetWorkspaceGitEnvironmentResolver(func(_ context.Context, taskID, sessionID, executorType string, env map[string]string) (map[string]string, error) {
				require.Equal(t, "task-1", taskID)
				require.Equal(t, "session-1", sessionID)
				require.Equal(t, string(models.ExecutorTypeLocal), executorType)
				if denied {
					return nil, errors.New("connection revoked")
				}
				env[githubauth.CredentialBrokerURLEnv] = "https://broker.example/resolve"
				env[githubauth.CredentialLeaseEnv] = "new-authorized-lease"
				env[githubauth.CredentialScopesEnv] = "authorized-scopes"
				return env, nil
			})
			prepared, err := mgr.prepareExecutionCreateRequest(context.Background(), "task-1", &WorkspaceInfo{
				TaskID: "task-1", SessionID: "session-1", WorkspaceID: "workspace-1", AgentID: "auggie",
				ExecutorType: string(models.ExecutorTypeLocal), WorkspacePath: t.TempDir(),
			}, "execution-1")
			if denied {
				require.Error(t, err)
				require.Nil(t, prepared)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "new-authorized-lease", prepared.request.Env[githubauth.CredentialLeaseEnv])
			require.Equal(t, "authorized-scopes", prepared.request.Env[githubauth.CredentialScopesEnv])
		})
	}
}

type unavailableRestoredProfile struct{ MockProfileResolver }

func (*unavailableRestoredProfile) ResolveProfile(context.Context, string) (*AgentProfileInfo, error) {
	return nil, errors.New("profile unavailable")
}

func TestRestoredWorkspaceDoesNotReplaceUnavailableProfileCredentials(t *testing.T) {
	mgr := newTestManager(t)
	mgr.profileResolver = &unavailableRestoredProfile{}
	called := false
	mgr.SetWorkspaceGitEnvironmentResolver(func(_ context.Context, _, _, _ string, env map[string]string) (map[string]string, error) {
		called = true
		return env, nil
	})
	prepared, err := mgr.prepareExecutionCreateRequest(context.Background(), "task-1", &WorkspaceInfo{
		TaskID: "task-1", SessionID: "session-1", AgentID: "auggie", AgentProfileID: "profile-1",
		ExecutorType: string(models.ExecutorTypeLocal), WorkspacePath: t.TempDir(),
	}, "execution-1")
	require.Error(t, err)
	require.Nil(t, prepared)
	require.False(t, called, "unknown explicit credentials must not fall back to the broker")
}

func TestFreshAndRestoredRuntimeEnvironmentExcludesServerSigningKey(t *testing.T) {
	const key = "KANDEV_GITHUB_CREDENTIAL_BROKER_REISSUE_SIGNING_KEY"
	t.Setenv(key, "fake-parent-key")
	mgr := newTestManager(t)
	for _, mode := range []string{"legacy", "strict", "finalized"} {
		env, err := mgr.buildEnvForExecution(context.Background(), "execution", &LaunchRequest{
			Env: map[string]string{key: "fake-request-key", "GITHUB_TOKEN": "explicit-token"}, EnvironmentFinalized: mode == "finalized", EnvironmentResolutionRequired: mode == "strict",
		}, nil, nil)
		require.NoError(t, err)
		require.NotContains(t, env, key)
		require.Equal(t, "explicit-token", env["GITHUB_TOKEN"])
	}
	var configured map[string]string
	var replaced bool
	client := newConfigureCaptureAgentctlClient(t, newTestLogger(), &configured, &replaced)
	execution := &AgentExecution{ID: "execution", TaskID: "task", SessionID: "session", AgentCommand: "agent", WorkspacePath: t.TempDir(), agentctl: client}
	execution.setRuntimeEnvironment(map[string]string{key: "fake-restored-key", "GITHUB_TOKEN": "explicit-token"})
	_, err := mgr.configureAndStartAgent(context.Background(), execution)
	require.NoError(t, err)
	require.NotContains(t, configured, key)
	require.Equal(t, "explicit-token", configured["GITHUB_TOKEN"])
}
