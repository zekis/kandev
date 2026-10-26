package subproc

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestMissingGitCredentialsDoNotExecuteAskpassCommandText(t *testing.T) {
	cmd := NewGitCommand(context.Background(), "-c", "credential.helper=", "credential", "fill")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1"}
	cmd.Stdin = strings.NewReader("protocol=https\nhost=example.invalid\n\n")
	PrepareGitCommand(cmd)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("credential fill must fail without credentials")
	}
	if strings.Contains(string(output), "cannot run exit 1") {
		t.Fatalf("invalid askpass executable obscures missing credentials: %s", output)
	}
	if !strings.Contains(string(output), "terminal prompts disabled") {
		t.Fatalf("expected noninteractive failure: %s", output)
	}
}
