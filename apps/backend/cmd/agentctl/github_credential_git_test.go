package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The child executes the real helper entry point, with synthetic credentials
// and a local HTTP broker. Git's own protocol handling remains in the parent.
func TestGitCredentialPersistenceChild(t *testing.T) {
	if os.Getenv("KANDEV_TEST_CREDENTIAL_CHILD") != "1" {
		return
	}
	err := runGitHubCredentialHelper(context.Background(), []string{"get"}, os.Stdin, os.Stdout, os.Getenv, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestGitCredentialPersistenceThroughGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			_, _ = io.WriteString(w, `{"code":"persistence_unavailable","error":"private-error"}`)
			return
		}
		_, _ = io.WriteString(w, `{"username":"git","password":"synthetic-token"}`)
	}))
	defer server.Close()
	executable, err := os.Executable()
	require.NoError(t, err)
	env := githubCredentialTestEnv(server.URL + "/resolve")
	// Git helper values are shell commands; quote the test binary path literally.
	helper := "!" + "'" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestGitCredentialPersistenceChild$ --"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "credential.helper=", "-c", "credential.helper="+helper, "-c", "credential.useHttpPath=true", "credential", "fill")
	cmd.Dir = t.TempDir()
	cmd.Env = append(envMap(env), "KANDEV_TEST_CREDENTIAL_CHILD=1", "PATH="+os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\npath=acme/widgets.git\n\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "helper subprocess failed")
	require.Equal(t, int32(2), calls.Load())
	require.Contains(t, stdout.String(), "password=synthetic-token\n")
	require.Empty(t, stderr.String())
}
