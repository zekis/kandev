package lifecycle

import "context"

// SetWorkspaceGitEnvironmentResolver supplies current task authorization when
// reconstructing a runtime for terminals and workspace Git operations.
func (m *Manager) SetWorkspaceGitEnvironmentResolver(resolver func(context.Context, string, string, string, map[string]string) (map[string]string, error)) {
	m.workspaceGitEnvironmentResolver = resolver
}
