package lifecycle

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func decodeWindowsSSHCommand(t *testing.T, command string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(command[strings.LastIndex(command, " ")+1:])
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return string(utf16.Decode(units))
}

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.4
func TestSSHWindowsOwnedStop(t *testing.T) {
	command := remoteAgentctlStopCommand("C:/Users/Example/.kandev/tasks/task/.kandev/sessions/session", 42)
	script := decodeWindowsSSHCommand(t, command)
	for _, required := range []string{"CreationDate", "ExecutablePath", "agentctl.identity.json", "Stop-Process", "Get-CimInstance"} {
		if !strings.Contains(script, required) {
			t.Errorf("stop does not verify %s", required)
		}
	}
	if strings.Contains(script, "taskkill") || strings.Contains(script, "kill -") {
		t.Fatal("non-owned termination")
	}
}

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.5
func TestSSHWindowsRecovery(t *testing.T) {
	server := newFakeSSHServer(t, func(command, stdin string) sshExecResult {
		script := decodeWindowsSSHCommand(t, command)
		if !strings.Contains(script, "CreationDate") || !strings.Contains(script, "ExecutablePath") {
			t.Error("identity probe omits durable process identity")
		}
		return sshExecResult{ExitCode: 1, Stderr: "process identity changed"}
	})
	ours, err := verifyRemoteAgentctlIdentity(context.Background(), server.dial(t), 42,
		"C:/Users/Example/.kandev/tasks/task/.kandev/sessions/session", "C:/Users/Example/.kandev/tasks/task")
	if ours || err == nil {
		t.Fatalf("ambiguous identity accepted: %v, %v", ours, err)
	}
}

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.6
func TestSSHWindowsPrivateCredentials(t *testing.T) {
	script := windowsPrivateDirectoryScript("C:/Users/O'Brien λ/.kandev")
	for _, required := range []string{"SetAccessRuleProtection", "SecurityIdentifier", "Get-Acl", "ReparsePoint", "Set-Acl"} {
		if !strings.Contains(script, required) {
			t.Errorf("private directory omits %s", required)
		}
	}
	if !strings.Contains(script, "O''Brien λ") {
		t.Fatal("path quoting changed")
	}
}

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.3
func TestSSHWindowsLaunchSecrets(t *testing.T) {
	command, payload, err := windowsLaunchCommand("C:/private/agentctl.exe", "C:/private/task", "C:/private/task/.kandev/sessions/s", 40000,
		map[string]string{"AGENTCTL_BOOTSTRAP_NONCE": "secret-sentinel"})
	if err != nil {
		t.Fatal(err)
	}
	script := decodeWindowsSSHCommand(t, command)
	if strings.Contains(script, "secret-sentinel") || !strings.Contains(payload, "secret-sentinel") {
		t.Fatal("launch secret is not confined to stdin")
	}
	if !strings.Contains(script, "Win32_Process") || !strings.Contains(script, "Invoke-CimMethod") {
		t.Fatal("launch does not detach from OpenSSH")
	}
}

func TestSSHWindowsSessionDirectory(t *testing.T) {
	server := newFakeSSHServer(t, func(command, stdin string) sshExecResult {
		if !strings.HasPrefix(command, "powershell.exe ") {
			return sshExecResult{ExitCode: 127}
		}
		script := decodeWindowsSSHCommand(t, command)
		if strings.Contains(script, "PathType Container") {
			return sshOut("exists")
		}
		if !strings.Contains(script, "SetAccessRuleProtection") {
			return sshExecResult{ExitCode: 1, Stderr: "private directory required"}
		}
		return sshOut("")
	})
	_, err := ensureRemoteSessionDir(context.Background(), server.dial(t), "C:/Users/Example/.kandev/tasks/task", "session")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSSHWindowsCredentialACLFailure(t *testing.T) {
	server := newFakeSSHServer(t, func(command, stdin string) sshExecResult { return sshExecResult{ExitCode: 1, Stderr: "ACL denied"} })
	uploader := sshFileUploader{client: server.dial(t)}
	err := uploader.WriteFile(context.Background(), "C:/Users/Example/.codex/auth.json", []byte("credential"), 0600)
	if err == nil || !strings.Contains(err.Error(), "ACL") {
		t.Fatalf("credential write did not fail at private ACL boundary: %v", err)
	}
}

func TestSSHWindowsShellCannotSelectUnix(t *testing.T) {
	got := sshShellForRemote(map[string]interface{}{MetadataKeySSHShell: "bash"}, SSHRemotePlatform{GOOS: "windows", GOARCH: "amd64"})
	if got != "powershell.exe" {
		t.Fatalf("native Windows selected %q", got)
	}
}

func TestSSHWindowsPrepare(t *testing.T) {
	req := &ExecutorCreateRequest{Metadata: map[string]interface{}{"repository_clone_url": "https://example.test/team/repo.git", MetadataKeyBaseBranch: "main", MetadataKeyWorktreeBranch: "feature/work"}}
	script, err := windowsPrepareScript(req, "C:/Users/Example User/.kandev/tasks/task")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"git.exe", "origin", "feature/work", "Set-Location"} {
		if !strings.Contains(script, required) {
			t.Errorf("prepare omits %s", required)
		}
	}
	if strings.Contains(script, "checkout -B") || strings.Contains(script, "set -e") {
		t.Fatal("destructive or POSIX checkout")
	}
}

func TestSSHWindowsReclaimPaths(t *testing.T) {
	if err := validateSSHReclaimPath("C:/Users/Example/.kandev", "C:/Users/Example/.kandev/tasks/task-one"); err != nil {
		t.Fatal(err)
	}
	if err := validateSSHReclaimPath("C:/Users/Example/.kandev", "C:/Users/Example/.kandev/tasks/../other"); err == nil {
		t.Fatal("Windows traversal accepted")
	}
	script := decodeWindowsSSHCommand(t, sshReclaimStatusCmd("C:/Users/Example/.kandev/tasks/task-one"))
	if !strings.Contains(script, "git.exe") {
		t.Fatal("Windows reclamation uses Unix commands")
	}
}

func TestSSHWindowsOrphanInventory(t *testing.T) {
	script := decodeWindowsSSHCommand(t, sshOrphanInventoryCommand("C:/Users/Example User/.kandev"))
	if !strings.Contains(script, "Get-CimInstance") || !strings.Contains(script, "CreationDate") {
		t.Fatal("Windows inventory omits process identity")
	}
	parsed := parseSSHOrphanInventory("PROC\t42\t1\tagentctl.exe --workdir \"C:/Users/Example User/.kandev/tasks/task-abc\"\nPIDFILE\ttask-abc\tsession\t42\n", "C:/Users/Example User/.kandev")
	if len(parsed.Processes) != 1 {
		t.Fatal("Windows workspace with spaces is missing from inventory")
	}
}

func TestSSHWindowsRecoveryWorkspaceIdentity(t *testing.T) {
	if !sameSSHWorkspacePath(`C:\Users\Example User\.kandev\tasks\task-a`, "C:/Users/Example User/.kandev/tasks/task-a") {
		t.Fatal("native path separators prevent recovery")
	}
	if sameSSHWorkspacePath("C:/work/task-a", "C:/work/task-ab") {
		t.Fatal("another workspace accepted")
	}
	if sameSSHWorkspacePath("/work/Task", "/work/task") {
		t.Fatal("Unix path comparison became case insensitive")
	}
}

func TestSSHWindowsSupervisorEncoding(t *testing.T) {
	data := windowsScriptFile("Write-Output 'λ'")
	if len(data) < 3 || string(data[:3]) != "\xef\xbb\xbf" {
		t.Fatal("PowerShell 5.1 needs a UTF-8 BOM for script files")
	}
	if !strings.Contains(string(data), "InputEncoding") || !strings.Contains(string(data), "λ") {
		t.Fatal("Unicode transport changed")
	}
}

func TestSSHWindowsCleanupDoesNotTraverseJunctions(t *testing.T) {
	script := windowsRemoveTreeScript()
	if strings.Contains(script, "-Recurse") || !strings.Contains(script, "ReparsePoint") {
		t.Fatal("recursive deletion may follow a junction")
	}
	if !strings.Contains(script, "GetFullPath") || !strings.Contains(script, "Stack") {
		t.Fatal("cleanup must verify each directory before deletion")
	}
}
