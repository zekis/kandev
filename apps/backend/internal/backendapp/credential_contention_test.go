package backendapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"
	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/db"
	"github.com/kandev/kandev/internal/gitcredentials"
	"github.com/kandev/kandev/internal/github"
	"github.com/kandev/kandev/internal/persistence/requiredstores"
	"github.com/kandev/kandev/internal/startup"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type contentionCredentialAuthority struct{ reader *sqlx.DB }

func (a contentionCredentialAuthority) AuthorizeGitCredential(ctx context.Context, s gitcredentials.Scope) error {
	var allowed bool
	err := a.reader.GetContext(ctx, &allowed, `SELECT allowed FROM credential_scope WHERE task_id=? AND session_id=? AND repository_id=?`, s.TaskID, s.SessionID, s.RepositoryID)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("scope revoked")
	}
	return nil
}

type contentionCredentialResolver struct{}

func (contentionCredentialResolver) Supports(string) bool { return true }
func (contentionCredentialResolver) Binding(context.Context, gitcredentials.Scope) (string, error) {
	return "generation-1", nil
}
func (contentionCredentialResolver) Resolve(context.Context, gitcredentials.Scope) (gitcredentials.Credential, error) {
	return gitcredentials.Credential{Username: "git", Password: "synthetic-token"}, nil
}

func newContentionCredentialFixture(t *testing.T) (*sqlx.DB, *requiredstores.Health, func() *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "broker.db")
	w, err := db.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })
	r, err := db.OpenSQLiteReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	writer, reader := sqlx.NewDb(w, "sqlite3"), sqlx.NewDb(r, "sqlite3")
	_, err = writer.Exec(`CREATE TABLE credential_scope(task_id TEXT,session_id TEXT,repository_id TEXT,allowed BOOLEAN)`)
	require.NoError(t, err)
	_, err = writer.Exec(`INSERT INTO credential_scope VALUES ('task','session','repository',1)`)
	require.NoError(t, err)
	tracker, err := requiredstores.NewTracker([]requiredstores.Descriptor{{ID: "scope", OwnerPackage: "test", RequiredTables: []string{"credential_scope"}, Sweep: startup.StepStoresRepositories}})
	require.NoError(t, err)
	require.NoError(t, tracker.RecordSuccess("scope"))
	health := requiredstores.NewHealth(tracker, db.NewPool(writer, reader), nil)
	require.NoError(t, health.Check(context.Background()))
	broker := gitcredentials.NewBroker(contentionCredentialResolver{}, contentionCredentialAuthority{reader: reader})
	scope := gitcredentials.Scope{ProviderID: "github", WorkspaceID: "workspace", TaskID: "task", SessionID: "session", RepositoryID: "repository", Host: "github.com", Path: "/acme/repo.git"}
	lease, err := broker.Issue(context.Background(), scope)
	require.NoError(t, err)
	log, err := logger.NewFromZap(zap.NewNop())
	require.NoError(t, err)
	service := github.NewService(nil, "", nil, nil, nil, log)
	service.SetCredentialBroker(github.NewCredentialBrokerFromBroker(broker))
	router := gin.New()
	router.Use(requiredPersistenceMiddleware(health))
	github.NewController(service, log).RegisterHTTPRoutes(router)
	request := map[string]string{"lease": lease.Token, "task_id": "task", "session_id": "session", "repository_id": "repository", "host": "github.com", "owner": "acme", "repo": "repo"}
	send := func() *httptest.ResponseRecorder {
		body, err := json.Marshal(request)
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/github/credentials/resolve", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		return rec
	}
	return writer, health, send
}

func TestCredentialContentionLoad(t *testing.T) {
	writer, health, send := newContentionCredentialFixture(t)
	require.Equal(t, 200, send().Code)
	tx, err := writer.Beginx()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, health.Check(ctx), context.DeadlineExceeded)
	denied := send()
	require.Equal(t, 503, denied.Code)
	require.Contains(t, denied.Body.String(), "persistence_unavailable")
	require.NotContains(t, denied.Body.String(), "synthetic-token")
	require.NoError(t, tx.Rollback())
	require.NoError(t, health.Check(context.Background()))
	require.Equal(t, 200, send().Code)
	_, err = writer.Exec(`UPDATE credential_scope SET allowed=0`)
	require.NoError(t, err)
	denied = send()
	require.NotEqual(t, 200, denied.Code)
	require.NotContains(t, denied.Body.String(), "synthetic-token")
	_, err = writer.Exec(`DROP TABLE credential_scope`)
	require.NoError(t, err)
	require.Error(t, health.Check(context.Background()))
	require.Equal(t, 503, send().Code)
}
