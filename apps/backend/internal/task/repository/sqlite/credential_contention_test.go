package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kandev/kandev/internal/agentctl/types/streams"
	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/persistence/requiredstores"
	"github.com/kandev/kandev/internal/startup"
	"github.com/kandev/kandev/internal/task/models"
	"github.com/stretchr/testify/require"
)

// Exercises actual FULL-durability receipts, canonical projections and cleanup
// on separate factory-created pools. Timings are evidence, not CI thresholds.
func TestCredentialContentionLoad(t *testing.T) {
	for _, concurrency := range []int{1, 4, 8} {
		for _, cleanup := range []bool{false, true} {
			t.Run(fmt.Sprintf("writers_%d/retention_%t", concurrency, cleanup), func(t *testing.T) {
				repo := newRetentionBenchmarkRepo(t, "SQLite")
				createRetentionBenchmarkSession(t, repo)
				prepareRetentionBenchmarkOwner(t, repo)
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				old := time.Now().UTC().Add(-48 * time.Hour)
				for n := int64(1); n <= 2663; n++ {
					require.NoError(t, contentionDelivery(ctx, repo, "old", n, old))
				}
				tracker, err := requiredstores.NewTracker([]requiredstores.Descriptor{{ID: "delivery", OwnerPackage: "internal/task", RequiredTables: []string{"agent_delivery_inbox", "agent_delivery_cursors", "agent_delivery_effects"}, Sweep: startup.StepStoresRepositories}})
				require.NoError(t, err)
				require.NoError(t, tracker.RecordSuccess("delivery"))
				health := requiredstores.NewHealth(tracker, db.NewPool(repo.db, repo.ro), nil)
				require.NoError(t, health.Check(ctx))
				before := repo.db.Stats()
				began := time.Now()
				done, errs := startContentionWriters(ctx, repo, concurrency, cleanup)

				probes, failed := observeContentionHealth(t, ctx, health, done)

				close(errs)
				for err := range errs {
					require.NoError(t, err)
				}
				require.NoError(t, health.Check(ctx))
				after := repo.db.Stats()
				verifyContentionIntegrity(t, ctx, repo, concurrency)
				t.Logf("writers=%d retention=%t elapsed=%s probe_count=%d failures=%d writer_waits=%d writer_wait=%s", concurrency, cleanup, time.Since(began), probes, failed, after.WaitCount-before.WaitCount, after.WaitDuration-before.WaitDuration)
			})
		}
	}
}

func contentionDelivery(ctx context.Context, repo *Repository, stream string, n int64, now time.Time) error {
	event := &models.AgentDeliveryEvent{SessionID: "retention-benchmark-session", IncarnationID: "retention-benchmark-incarnation", HarnessGeneration: 1, StreamID: stream, Sequence: n, EventType: streams.EventTypeMessageChunk, Payload: []byte(`{"type":"message_chunk","text":"synthetic load"}`), ReceivedAt: now}
	setRetentionBenchmarkEventOwner(event)
	if _, err := repo.ReceiveAgentDeliveryEvent(ctx, event, n); err != nil {
		return err
	}
	effect := &models.AgentDeliveryEffect{EffectKey: fmt.Sprintf("agent_delivery.event:%s:%d", stream, n), StreamID: stream, Sequence: n, EffectType: "agent_delivery.event", State: models.DeliveryEffectCompleted, CreatedAt: now, CompletedAt: &now}
	_, err := repo.ProjectCanonicalAgentDeliveryEvent(ctx, event, effect)
	return err
}

func startContentionWriters(ctx context.Context, repo *Repository, concurrency int, cleanup bool) (<-chan struct{}, chan error) {
	var workers sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, concurrency+1)
	for i := 0; i < concurrency; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			for n := int64(1); n <= 50; n++ {
				if err := contentionDelivery(ctx, repo, fmt.Sprintf("live-%d", i), n, time.Now().UTC()); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	if cleanup {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for i := 0; i < 20; i++ {
				if _, err := repo.RunAgentDeliveryRetentionBatch(ctx, models.AgentDeliveryRetentionBatchOptions{}); err != nil {
					// The worker treats its bounded batch deadline as retryable.
					// The outer workload deadline still fails the fixture.
					if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
						continue
					}
					errs <- err
					return
				}
			}
		}()
	}
	close(start)
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	return done, errs
}

func observeContentionHealth(t *testing.T, ctx context.Context, health *requiredstores.Health, done <-chan struct{}) (int, int) {
	t.Helper()
	probes, failed := 0, 0
probeLoop:
	for {
		select {
		case <-done:
			break probeLoop
		default:
		}
		probeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		err := health.Check(probeCtx)
		stop()
		probes++
		if err != nil {
			failed++
		}
		select {
		case <-done:
			break probeLoop
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	return probes, failed
}

func verifyContentionIntegrity(t *testing.T, ctx context.Context, repo *Repository, concurrency int) {
	t.Helper()
	for i := 0; i < concurrency; i++ {
		cursor, err := repo.GetAgentDeliveryCursor(ctx, fmt.Sprintf("live-%d", i))
		require.NoError(t, err)
		require.Equal(t, int64(50), cursor.ReceivedSequence)
		require.Equal(t, int64(50), cursor.ProjectedSequence)
		require.NoError(t, contentionDelivery(ctx, repo, fmt.Sprintf("live-%d", i), 50, time.Now().UTC()))
	}
	var integrity string
	require.NoError(t, repo.ro.Get(&integrity, `PRAGMA integrity_check`))
	require.Equal(t, "ok", integrity)
	var path string
	require.NoError(t, repo.ro.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path))
	require.NoError(t, repo.Close())
	require.NoError(t, repo.db.Close())
	require.NoError(t, repo.ro.Close())
	reopened, err := db.OpenSQLiteReader(path)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()
	var receipts, effects int
	require.NoError(t, reopened.QueryRow(`SELECT COUNT(*) FROM agent_delivery_inbox WHERE stream_id LIKE 'live-%'`).Scan(&receipts))
	require.NoError(t, reopened.QueryRow(`SELECT COUNT(*) FROM agent_delivery_effects WHERE stream_id LIKE 'live-%' AND state='completed'`).Scan(&effects))
	require.Equal(t, concurrency*50, receipts)
	require.Equal(t, receipts, effects)
}
