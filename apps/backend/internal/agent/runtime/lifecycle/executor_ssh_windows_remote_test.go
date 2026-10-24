package lifecycle

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.1
func TestSSHWindowsProbe(t *testing.T) {
	server := newFakeSSHServer(t, func(command, stdin string) sshExecResult {
		if strings.HasPrefix(command, "powershell.exe ") {
			return sshOut(`{"os":"Windows","arch":"AMD64","home":"C:\\Users\\Example User","powershell":"5.1","git":"git version 2.50.0.windows.1"}`)
		}
		return sshExecResult{ExitCode: 127, Stderr: "command not found"}
	})
	info, err := SSHProbeRemote(context.Background(), server.dial(t))
	if err != nil {
		t.Fatalf("native Windows discovery failed: %v", err)
	}
	if info.Platform.GOOS != "windows" || info.Platform.GOARCH != "amd64" {
		t.Fatalf("platform = %s", info.Platform)
	}
	if info.GitVer != "git version 2.50.0.windows.1" {
		t.Fatalf("Git prerequisite not reported: %q", info.GitVer)
	}
}

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.2
func TestResolveRemoteWindowsHelper(t *testing.T) {
	platform := SSHRemotePlatform{GOOS: "windows", GOARCH: "amd64"}
	if got := agentctlBinaryName(platform); got != "agentctl-windows-amd64.exe" {
		t.Fatalf("helper = %q", got)
	}
}

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.7
func TestSSHWindowsUnsupportedArchitecture(t *testing.T) {
	if err := SSHRequireSupportedRemotePlatform(SSHRemotePlatform{GOOS: "windows", GOARCH: "arm64"}); err == nil {
		t.Fatal("Windows arm64 must remain unsupported")
	}
}

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.2
func TestSSHWindowsPaths(t *testing.T) {
	script := `Write-Output 'C:\Users\Example User\O''Brien λ'`
	command := WrapLoginShell("powershell.exe", script)
	const prefix = "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand "
	if !strings.HasPrefix(command, prefix) {
		t.Fatalf("not an explicit encoded PowerShell invocation: %q", command)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, prefix))
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	if decoded := string(utf16.Decode(units)); !strings.Contains(decoded, script) {
		t.Fatalf("path/script changed: %q", decoded)
	}
}

// @covers AC-EXECUTORS-SSH-EXECUTOR-002.2
func TestRemoteHelperWindowsManifest(t *testing.T) {
	manifest := testRemoteHelperManifest(RemoteHelperVariantFull)
	manifest.Helpers = append(manifest.Helpers, RemoteHelperRecord{Platform: "windows/amd64", Asset: "agentctl-windows-amd64.exe.gz", SHA256: strings.Repeat("e", 64), SizeBytes: 10})
	if err := validateRemoteHelperManifest(manifest, manifest.Version, manifest.Commit); err != nil {
		t.Fatalf("Windows helper manifest rejected: %v", err)
	}
}

func TestSSHWindowsBinaryDiscovery(t *testing.T) {
	server := newFakeSSHServer(t, func(command, stdin string) sshExecResult {
		data, err := base64.StdEncoding.DecodeString(command[strings.LastIndex(command, " ")+1:])
		if err != nil {
			return sshExecResult{ExitCode: 1}
		}
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		script := string(utf16.Decode(units))
		if strings.Contains(script, "Get-Command") && !strings.Contains(script, "command -v") {
			return sshOut(`C:\Tools\npx.cmd`)
		}
		return sshExecResult{ExitCode: 1, Stderr: "POSIX command is not available"}
	})
	got, err := ProbeRemoteBinary(context.Background(), server.dial(t), "powershell.exe", "npx")
	if err != nil || got != `C:\Tools\npx.cmd` {
		t.Fatalf("Windows CLI not found: %q, %v", got, err)
	}
}

func TestSSHWindowsHomePaths(t *testing.T) {
	for _, tc := range []struct {
		home  string
		want  string
		valid bool
	}{
		{`C:\Users\Example User`, "C:/Users/Example User/.kandev", true},
		{`C:\Users\O'Brien λ`, "C:/Users/O'Brien λ/.kandev", true},
		{`C:\Users\..\Other`, "", false},
		{`\\server\share`, "", false},
	} {
		t.Run(tc.home, func(t *testing.T) {
			server := newFakeSSHServer(t, func(command, stdin string) sshExecResult {
				if strings.HasPrefix(command, "powershell.exe ") {
					return sshOut(tc.home)
				}
				return sshExecResult{ExitCode: 127}
			})
			got, err := expandRemoteHome(context.Background(), server.dial(t), "~/.kandev")
			if tc.valid && (err != nil || got != tc.want) {
				t.Fatalf("remote home = %q, %v; want %q", got, err, tc.want)
			}
			if !tc.valid && err == nil {
				t.Fatalf("unsafe home accepted: %q", got)
			}
		})
	}
}
