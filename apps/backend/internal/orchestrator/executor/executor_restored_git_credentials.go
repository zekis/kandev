package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/kandev/kandev/internal/task/models"
)

// RestoreWorkspaceGitEnvironment rebuilds opaque capabilities from current
// bindings. Runtime snapshots and persisted metadata are never lease authority.
func (e *Executor) RestoreWorkspaceGitEnvironment(ctx context.Context, taskID, sessionID, executorType string, env map[string]string) (map[string]string, error) {
	task, err := e.repo.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load credential task: %w", err)
	}
	if task == nil {
		return nil, errors.New("credential task is missing")
	}
	session, err := e.repo.GetTaskSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load credential session: %w", err)
	}
	if session == nil || session.TaskID != taskID {
		return nil, errors.New("credential session does not belong to task")
	}
	repositories, err := e.restoredCredentialRepositories(ctx, taskID, task.WorkspaceID)
	if err != nil {
		return nil, err
	}
	req := &LaunchAgentRequest{TaskID: taskID, SessionID: sessionID, WorkspaceID: task.WorkspaceID, ExecutorType: executorType, Env: cloneStringMap(env)}
	if err := removeManagedGitHubCredentials(req); err != nil {
		return nil, err
	}
	// env already contains resolved agent, executor, and repository overrides.
	// Normal policy selection still determines whether a local host bridge applies.
	if err := e.configureGitCredentialBrokerForRepositoriesWithProfileEnvAndBridge(ctx, req, repositories, nil, true); err != nil {
		return nil, err
	}
	return req.Env, nil
}

func (e *Executor) restoredCredentialRepositories(ctx context.Context, taskID, workspaceID string) ([]*repoInfo, error) {
	bindings, err := e.repo.ListTaskRepositories(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list credential repositories: %w", err)
	}
	infos := make([]*repoInfo, 0, len(bindings))
	for _, binding := range bindings {
		if binding == nil || binding.RepositoryID == "" || binding.TaskID != taskID {
			return nil, errors.New("invalid credential repository binding")
		}
		repository, err := e.repo.GetRepository(ctx, binding.RepositoryID)
		if err != nil {
			return nil, fmt.Errorf("load credential repository: %w", err)
		}
		if repository == nil || repository.WorkspaceID != workspaceID {
			return nil, errors.New("credential repository does not belong to workspace")
		}
		info, err := restoredCredentialRepositoryInfo(binding, repository)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func restoredCredentialRepositoryInfo(binding *models.TaskRepository, repository *models.Repository) (*repoInfo, error) {
	info := &repoInfo{RepositoryID: binding.RepositoryID, TaskRepositoryID: binding.ID, Repository: repository}
	contribution, found, err := models.LoadRemoteContribution(binding.Metadata)
	if err != nil {
		return nil, err
	}
	if found {
		info.RemoteContribution = &contribution
	}
	destination, found, err := models.LoadContributionDestination(binding.Metadata)
	if err != nil {
		return nil, err
	}
	if found {
		info.ContributionDestination = &destination
	}
	return info, nil
}
