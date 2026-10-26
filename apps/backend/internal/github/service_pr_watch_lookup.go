package github

import (
	"context"
	"errors"
	"sort"
)

// searchingWatchLookup retains a follow-up outcome for every consumer of the
// same batch, including local deferrals that have not reached the provider.
type searchingWatchLookup struct {
	pr        *PR
	attempted bool
	err       error
}

func isPRDiscoveryDeferred(err error) bool {
	return errors.Is(err, ErrBackgroundAdmissionDeferred) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func searchingWatchErrorResult(watch *PRWatch, err error) PRWatchSyncResult {
	return PRWatchSyncResult{Watch: watch, discoveryErr: err, SyncFailed: !isPRDiscoveryDeferred(err)}
}

// Discovery gets the Core admission opportunity before optional Actions
// enrichment. Cached fork metadata allows an exact-head parent read to advance
// on the next poll when normal pacing defers it after the metadata request.
func (s *Service) fetchSearchingWatchLookups(
	ctx context.Context, client Client, cacheScope string, watches []*PRWatch, statuses *batchedWatchStatuses,
) {
	statuses.branchLookups = make(map[string]searchingWatchLookup, len(watches))
	// Oldest unfinished targets get the next admission opportunity.
	ordered := append([]*PRWatch(nil), watches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i].LastCheckedAt, ordered[j].LastCheckedAt
		if right == nil {
			return false
		}
		return left == nil || left.Before(*right)
	})
	for _, watch := range ordered {
		key := graphqlBranchKey(watch.Owner, watch.Repo, watch.Branch)
		if status := statuses.byKey[key]; status != nil && status.PR != nil {
			continue
		}
		if _, exists := statuses.branchLookups[key]; exists {
			continue
		}
		if s.isRepoCachedAsMissingForScope(cacheScope, watch.Owner, watch.Repo) {
			continue
		}
		pr, attempted, err := s.lookupSearchingWatchPR(ctx, client, cacheScope, watch, key, statuses, watch.LastCheckedAt)
		statuses.branchLookups[key] = searchingWatchLookup{pr: pr, attempted: attempted, err: err}
	}
}
