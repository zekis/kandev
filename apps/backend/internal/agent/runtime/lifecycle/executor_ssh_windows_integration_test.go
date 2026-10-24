package lifecycle

import (
	"context"

	"crypto/sha256"

	"encoding/hex"

	"encoding/json"

	"os"

	"strconv"

	"testing"

	"time"

	"github.com/kandev/kandev/internal/agentruntime"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/task/models"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestSSHWindowsE2E requires a private fixture JSON containing SSHConnConfig

// fields and a locally built helper. The destination must be a disposable

// Windows test account; this test creates a unique workspace below its home.

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.2

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.3

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.4

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.5

func TestSSHWindowsE2E(t *testing.T) {

	fixture := os.Getenv("KANDEV_WINDOWS_SSH_FIXTURE")

	if fixture == "" {
		t.Skip("real Windows SSH fixture not configured")
	}

	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}

	var config SSHConnConfig

	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}

	if config.PinnedFingerprint == "" {
		t.Fatal("fixture requires a pinned host fingerprint")
	}

	target, err := ResolveSSHTarget(config)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := dialSSH(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	info, err := detectRemoteInfo(ctx, client)
	if err != nil || info.Platform.GOOS != "windows" {
		t.Fatalf("Windows discovery: %+v, %v", info, err)
	}

	helper, err := os.ReadFile(os.Getenv("KANDEV_WINDOWS_SSH_HELPER"))
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(helper)

	binary, err := ensureWindowsHelper(ctx, client, hex.EncodeToString(digest[:]), helper)
	if err != nil {
		t.Fatal(err)
	}

	task, err := ensureRemoteTaskDir(ctx, client, "~/.kandev-windows-smoke", "task-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err != nil {
		t.Fatal(err)
	}

	session, err := ensureRemoteSessionDir(ctx, client, task, "session")
	if err != nil {
		t.Fatal(err)
	}

	nonce, err := generateBootstrapNonce()
	if err != nil {
		t.Fatal(err)
	}

	port, pid, err := startRemoteAgentctl(ctx, client, "powershell.exe", binary, task, session, sshAgentctlLaunchEnv(nil, nonce), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := stopRemoteAgentctl(context.Background(), client, session, pid); err != nil {
			t.Error(err)
		}
	}()

	token, err := remoteControlHandshake(ctx, client, port, nonce)
	if err != nil || token == "" {
		t.Fatalf("authenticated control handshake: %v", err)
	}

	if ours, err := verifyRemoteAgentctlIdentity(ctx, client, pid, session, task); err != nil || !ours {
		t.Fatalf("process identity: %v, %v", ours, err)
	}

	_ = client.Close()

	client, err = dialSSH(ctx, target)
	if err != nil {
		t.Fatal(err)
	}

	if ours, err := verifyRemoteAgentctlIdentity(ctx, client, pid, session, task); err != nil || !ours {
		t.Fatalf("controller did not survive reconnect: %v, %v", ours, err)
	}

	t.Log("native helper upload, private ACL, detached launch, authenticated handshake and reconnect passed")

}

func TestSSHWindowsFullCreateE2E(t *testing.T) {

	fixture := os.Getenv("KANDEV_WINDOWS_SSH_FIXTURE")
	if fixture == "" {
		t.Skip("real Windows SSH fixture not configured")
	}

	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}

	var config SSHConnConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}

	target, err := ResolveSSHTarget(config)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("KANDEV_AGENTCTL_WINDOWS_AMD64_BINARY", os.Getenv("KANDEV_WINDOWS_SSH_HELPER"))

	core, observed := observer.New(zap.DebugLevel)
	log, err := logger.NewFromZap(zap.New(core))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if t.Failed() {
			for _, entry := range observed.All() {
				t.Log(entry.Message, entry.ContextMap()["error"])
			}
		}
	}()
	exec := NewSSHExecutor(nil, nil, NewAgentctlResolver(log), log)
	defer func() { _ = exec.Close() }()

	id := strconv.FormatInt(time.Now().UnixNano(), 10)

	req := &ExecutorCreateRequest{InstanceID: "instance-" + id, TaskID: id, SessionID: "session-" + id, Protocol: "acp", OnProgress: func(step PrepareStep, _, _ int) {
		if step.Status == PrepareStepFailed {
			t.Log(step)
		}
	}, Metadata: map[string]interface{}{

		MetadataKeySSHHost: target.Host, MetadataKeySSHPort: strconv.Itoa(target.Port), MetadataKeySSHUser: target.User,

		MetadataKeySSHIdentitySource: string(SSHIdentitySourceFile), MetadataKeySSHIdentityFile: target.IdentityFile,

		MetadataKeySSHProxyJump: target.ProxyJump, MetadataKeySSHHostFingerprint: target.PinnedFingerprint, MetadataKeySSHWorkdirRoot: "~/.kandev-windows-smoke/Example User λ",
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sourceClient, err := dialSSH(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	source, err := ensureRemoteTaskDir(ctx, sourceClient, "~/.kandev-windows-smoke", "source-"+id)
	if err != nil {
		t.Fatal(err)
	}
	script := windowsGitCommand(source, "init", "-b", "main") + "\n[IO.File]::WriteAllText((Join-Path " + windowsPowerShellQuote(source) + " 'README.txt'),'fixture')\n" + windowsGitCommand(source, "add", "README.txt") + "\n" + windowsGitCommand(source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-q", "-m", "fixture")
	if _, _, err := runWindowsScript(ctx, sourceClient, script, nil); err != nil {
		t.Fatal(err)
	}
	_ = sourceClient.Close()
	req.Metadata["repository_clone_url"] = source
	req.Metadata[MetadataKeyBaseBranch] = "main"
	req.Metadata[MetadataKeyWorktreeBranch] = "feature/smoke"
	instance, err := exec.CreateInstance(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if instance.AuthToken == "" || instance.Client == nil {
		t.Fatal("native instance has no authenticated client")
	}
	if _, err := instance.Client.RequestFileContent(ctx, "README.txt", ""); err != nil {
		t.Fatal(err)
	}
	if err := instance.Client.StartShellTerminal(ctx, "smoke", 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := instance.Client.StopShellTerminal(ctx, "smoke"); err != nil {
		t.Fatal(err)
	}

	if err := exec.Close(); err != nil {
		t.Fatal(err)
	}

	recovered := NewSSHExecutor(nil, nil, NewAgentctlResolver(log), log)
	defer func() { _ = recovered.Close() }()

	pid, _ := strconv.Atoi(getMetadataString(instance.Metadata, MetadataKeySSHRemoteAgentctlPID))

	records := []*models.ExecutorRunning{{ID: req.SessionID, SessionID: req.SessionID, TaskID: req.TaskID, AgentExecutionID: req.InstanceID, Runtime: agentruntime.RuntimeSSH, PID: pid, TransientAuthToken: instance.AuthToken, Metadata: mergeSSHMetadata(req.Metadata, instance.Metadata)}}

	instances, outcomes, err := recovered.RecoverInstancesDetailed(ctx, records)

	if err != nil || len(instances) != 1 {
		t.Fatalf("native restart recovery: count=%d outcomes=%v err=%v", len(instances), outcomes, err)
	}

	if err := recovered.StopInstance(ctx, instances[0], true); err != nil {
		t.Fatal(err)
	}
	client, err := dialSSH(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if alive, err := probeRemoteAgentctlLiveness(ctx, client, pid, getMetadataString(instance.Metadata, MetadataKeySSHRemoteSessionDir)); err != nil || alive {
		t.Fatalf("controller still exists after stop: %v, %v", alive, err)
	}

	t.Log("normal native instance creation, restart recovery and owned stop passed")

}
