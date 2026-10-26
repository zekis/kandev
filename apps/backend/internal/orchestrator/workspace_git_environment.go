package orchestrator

import "context"

// RestoreWorkspaceGitEnvironment shares launch authorization with runtime restoration.
func (s *Service) RestoreWorkspaceGitEnvironment(ctx context.Context, taskID, sessionID, executorType string, env map[string]string) (map[string]string, error) {
	return s.executor.RestoreWorkspaceGitEnvironment(ctx, taskID, sessionID, executorType, env)
}
