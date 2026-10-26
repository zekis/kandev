package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type credentialRoundTrip func(*http.Request) (*http.Response, error)

func (f credentialRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func credentialResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// @covers AC-PLATFORM-GIT-CREDENTIAL-LEASE-REISSUE-002.1
func TestGitHubCredentialPersistenceRecovery(t *testing.T) {
	for _, outage := range []time.Duration{0, 13 * time.Second, 30 * time.Second, 44 * time.Second} {
		t.Run(outage.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started := time.Now()
				calls := 0
				scope := githubBrokerResolveRequest{Lease: "private-lease", TaskID: "task", SessionID: "session", RepositoryID: "repository", Host: "github.com", Path: "/acme/repo.git"}
				client := &githubCredentialBrokerClient{endpoint: "https://broker/resolve", request: scope, http: &http.Client{Transport: credentialRoundTrip(func(r *http.Request) (*http.Response, error) {
					calls++
					var got githubBrokerResolveRequest
					require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
					require.Equal(t, scope, got)
					if time.Since(started) < outage {
						return credentialResponse(503, `{"code":"persistence_unavailable","error":"private-server-value"}`), nil
					}
					return credentialResponse(200, `{"username":"x-access-token","password":"private-token"}`), nil
				})}}
				credential, err := client.resolveWithReissue(context.Background())
				require.NoError(t, err)
				require.Equal(t, "private-token", credential.Password)
				require.LessOrEqual(t, time.Since(started), 60*time.Second)
				if outage == 0 {
					require.Equal(t, 1, calls)
				} else {
					require.Greater(t, calls, 1)
				}
			})
		})
	}
}

func TestGitHubCredentialPersistenceExhaustion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		start := time.Now()
		client := &githubCredentialBrokerClient{endpoint: "https://broker/resolve", http: &http.Client{Transport: credentialRoundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			return credentialResponse(503, `{"code":"persistence_unavailable","error":"private-server-value"}`), nil
		})}}
		credential, err := client.resolveWithReissue(context.Background())
		require.Error(t, err)
		require.Nil(t, credential)
		require.Greater(t, calls, 1)
		require.LessOrEqual(t, calls, 32)
		require.LessOrEqual(t, time.Since(start), 60*time.Second)
		require.NotContains(t, err.Error(), "private-server-value")
	})
}

func TestGitHubCredentialPersistenceDoesNotRetryOtherFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		transport bool
	}{
		{"unclassified", 503, `{"code":"provider_unavailable"}`, false},
		{"malformed", 503, `broken`, false},
		{"scope", 403, `{"code":"git_credential_scope_denied"}`, false},
		{"wrong_status", 500, `{"code":"persistence_unavailable"}`, false},
		{"transport", 0, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &githubCredentialBrokerClient{endpoint: "https://broker/resolve", http: &http.Client{Transport: credentialRoundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.transport {
					return nil, errors.New("ambiguous transport")
				}
				return credentialResponse(tc.status, tc.body), nil
			})}}
			credential, err := client.resolveWithReissue(context.Background())
			require.Error(t, err)
			require.Nil(t, credential)
			require.Equal(t, 1, calls)
		})
	}
}

func TestGitHubCredentialPersistenceCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		calls := 0
		start := time.Now()
		client := &githubCredentialBrokerClient{endpoint: "https://broker/resolve", http: &http.Client{Transport: credentialRoundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			return credentialResponse(503, `{"code":"persistence_unavailable"}`), nil
		})}}
		credential, err := client.resolveWithReissue(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Nil(t, credential)
		require.Equal(t, 1, calls)
		require.Equal(t, 100*time.Millisecond, time.Since(start))
	})
}

func TestGitHubCredentialPersistenceReissueBudget(t *testing.T) {
	for _, failReissue := range []bool{false, true} {
		t.Run(fmt.Sprint(failReissue), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				resolves, reissues := 0, 0
				client := &githubCredentialBrokerClient{endpoint: "https://broker/resolve", request: githubBrokerResolveRequest{Lease: "old", ReissueCapability: "capability"}, http: &http.Client{Transport: credentialRoundTrip(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/reissue" {
						reissues++
						if failReissue {
							return credentialResponse(503, `{"code":"persistence_unavailable"}`), nil
						}
						return credentialResponse(200, `{"token":"replacement"}`), nil
					}
					resolves++
					if resolves == 2 {
						return credentialResponse(401, `{"code":"git_credential_lease_expired"}`), nil
					}
					if resolves < 4 {
						return credentialResponse(503, `{"code":"persistence_unavailable"}`), nil
					}
					return credentialResponse(200, `{"username":"git","password":"token"}`), nil
				})}}
				credential, err := client.resolveWithReissue(context.Background())
				require.Equal(t, 1, reissues)
				if failReissue {
					require.Error(t, err)
					require.Nil(t, credential)
					require.Equal(t, 2, resolves)
				} else {
					require.NoError(t, err)
					require.Equal(t, "token", credential.Password)
					require.Equal(t, 4, resolves)
				}
			})
		})
	}
}

func TestGitHubCredentialPersistenceInFlightDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		calls := 0
		client := &githubCredentialBrokerClient{endpoint: "https://broker/resolve", http: &http.Client{Transport: credentialRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return credentialResponse(503, `{"code":"persistence_unavailable"}`), nil
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		})}}
		credential, err := client.resolveWithReissue(context.Background())
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Nil(t, credential)
		require.Equal(t, 2, calls)
		require.Equal(t, 60*time.Second, time.Since(start))
	})
}

func TestGitHubCredentialPersistenceShimRunsCommandOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts, commands := 0, 0
		env := githubCredentialTestEnv("https://broker/resolve")
		env["PATH"] = "/usr/bin"
		client := &http.Client{Transport: credentialRoundTrip(func(*http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return credentialResponse(503, `{"code":"persistence_unavailable"}`), nil
			}
			return credentialResponse(200, `{"username":"git","password":"token"}`), nil
		})}
		commandFailure := errors.New("ambiguous mutation failure")
		err := runGitHubCLIShim(context.Background(), []string{"pr", "create"}, strings.NewReader(""), io.Discard, io.Discard, lookupEnv(env), func() []string { return envMap(env) }, client, "/shim", func(string, string) (string, error) { return "/usr/bin/gh", nil }, func(_ context.Context, _ string, _ []string, child []string, _ io.Reader, _, _ io.Writer) error {
			commands++
			require.Equal(t, "token", envValue(child, "GH_TOKEN"))
			return commandFailure
		})
		require.ErrorIs(t, err, commandFailure)
		require.Equal(t, 3, attempts)
		require.Equal(t, 1, commands)
	})
}
