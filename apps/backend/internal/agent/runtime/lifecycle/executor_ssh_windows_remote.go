package lifecycle

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
)

const (
	sshWindowsPowerShell    = "powershell.exe"
	sshWindowsProcessAbsent = "absent"
	sshWindowsProcessAlive  = "alive"
)

func isWindowsRemoteShell(shell string) bool {
	return strings.EqualFold(shell, sshWindowsPowerShell) || strings.EqualFold(shell, "powershell")
}

func windowsPowerShellCommand(script string) string {
	script = windowsPowerShellCommandBody(script)
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(data[i*2:], u)
	}
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(data)
}

func isSSHCommandMissing(err error) bool {
	var exit *ssh.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	switch exit.ExitStatus() {
	case 1, 127, 9009:
		return true
	default:
		return false
	}
}

func detectWindowsRemoteInfo(ctx context.Context, client *ssh.Client) (*SSHRemoteInfo, error) {
	const script = `$arch = $env:PROCESSOR_ARCHITEW6432
if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
$git = Get-Command git.exe -ErrorAction SilentlyContinue
$gitVersion = ''
if ($git) { $gitVersion = & $git.Source --version; if ($LASTEXITCODE -ne 0) { throw 'Git prerequisite probe failed' } }
@{ os='Windows'; arch=$arch; home=[Environment]::GetFolderPath('UserProfile'); powershell=$PSVersionTable.PSVersion.ToString(); git=$gitVersion } | ConvertTo-Json -Compress`
	out, _, err := runSSHCommand(ctx, client, windowsPowerShellCommand(script))
	if err != nil {
		return nil, fmt.Errorf("ssh: native Windows prerequisite probe: %w", err)
	}
	var result struct {
		OS         string
		Arch       string
		Home       string
		PowerShell string
		Git        string
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		return nil, fmt.Errorf("ssh: invalid Windows prerequisite response: %w", err)
	}
	if result.OS != "Windows" || result.Home == "" || !windowsPowerShellVersionSupported(result.PowerShell) {
		return nil, errors.New("ssh: Windows requires a user profile and Windows PowerShell 5.1 or newer")
	}
	platform, _ := normalizeSSHRemotePlatform(result.OS, result.Arch)
	return &SSHRemoteInfo{OS: result.OS, Arch: result.Arch, Platform: platform, UnameAll: "Windows " + result.Arch, GitVer: result.Git}, nil
}

func windowsPowerShellVersionSupported(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	return e1 == nil && e2 == nil && (major > 5 || major == 5 && minor >= 1)
}

func windowsPowerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func normalizeWindowsRemotePath(value string) (string, error) {
	value = strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(value, "/") && len(value) > 3 && value[2] == ':' {
		value = value[1:]
	}
	if len(value) < 3 {
		return "", errors.New("ssh: Windows path must be an absolute local drive path")
	}
	driveLetter := (value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')
	if !driveLetter || value[1] != ':' || value[2] != '/' {
		return "", errors.New("ssh: Windows path must be an absolute local drive path")
	}
	if strings.ContainsAny(value[2:], ":\x00\r\n\"<>|?*") {
		return "", errors.New("ssh: invalid Windows path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return "", errors.New("ssh: Windows path traversal refused")
		}
	}
	return path.Clean(value), nil
}

func windowsPowerShellCommandBody(script string) string {
	return "$env:PATH=[Environment]::GetEnvironmentVariable('PATH','Machine')+';'+[Environment]::GetEnvironmentVariable('PATH','User')+';'+$env:PATH; $ProgressPreference='SilentlyContinue'; [Console]::InputEncoding=New-Object Text.UTF8Encoding($false); [Console]::OutputEncoding=New-Object Text.UTF8Encoding($false); $LASTEXITCODE=0; $ErrorActionPreference = 'Stop'; try {\n" + script + "\nif ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) { exit $LASTEXITCODE }\n} catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }"
}

func windowsScriptFile(script string) []byte {
	return []byte("\xef\xbb\xbf" + windowsPowerShellCommandBody(script))
}
