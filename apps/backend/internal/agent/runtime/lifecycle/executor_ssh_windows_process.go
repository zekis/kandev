package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kandev/kandev/internal/common/logger"
	"golang.org/x/crypto/ssh"
)

func isWindowsRemotePath(value string) bool {
	_, err := normalizeWindowsRemotePath(value)
	return err == nil
}

func windowsPrivateDirectoryScript(directory string) string {
	return `$dir=[IO.Path]::GetFullPath(` + windowsPowerShellQuote(directory) + `)
$cursor=$dir
while ($cursor) {
 if (Test-Path -LiteralPath $cursor) {
  $item=Get-Item -LiteralPath $cursor -Force
  if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or -not $item.PSIsContainer) { throw 'Unsafe private directory' }
 }
 $parent=[IO.Path]::GetDirectoryName($cursor)
 if ($parent -eq $cursor) { break }; $cursor=$parent
}
[IO.Directory]::CreateDirectory($dir) | Out-Null
$sid=[Security.Principal.WindowsIdentity]::GetCurrent().User
$allowed=@($sid.Value,'S-1-5-18','S-1-5-32-544') | Select-Object -Unique
$acl=New-Object Security.AccessControl.DirectorySecurity
$acl.SetOwner($sid); $acl.SetAccessRuleProtection($true,$false)
foreach ($value in $allowed) {
 $identity=New-Object Security.Principal.SecurityIdentifier($value)
 $rule=New-Object Security.AccessControl.FileSystemAccessRule($identity,'FullControl','ContainerInherit,ObjectInherit','None','Allow')
 $acl.AddAccessRule($rule)
}
Set-Acl -LiteralPath $dir -AclObject $acl
$actual=Get-Acl -LiteralPath $dir
if (-not $actual.AreAccessRulesProtected) { throw 'Private ACL inheritance remains enabled' }
foreach ($rule in $actual.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier])) {
 if ($rule.AccessControlType -ne 'Allow' -or $allowed -notcontains $rule.IdentityReference.Value) { throw 'Private ACL verification failed' }
}`
}

func ensureWindowsPrivateDirectory(ctx context.Context, client *ssh.Client, directory string) error {
	if _, err := normalizeWindowsRemotePath(directory); err != nil {
		return err
	}
	_, _, err := runSSHCommand(ctx, client, windowsPowerShellCommand(windowsPrivateDirectoryScript(directory)))
	if err != nil {
		return fmt.Errorf("ssh: private Windows directory ACL: %w", err)
	}
	return nil
}

func windowsLaunchCommand(binary, workspace, session string, port int, env map[string]string) (string, string, error) {
	for _, value := range []string{binary, workspace, session} {
		if _, err := normalizeWindowsRemotePath(value); err != nil {
			return "", "", err
		}
	}
	for key := range env {
		if !posixSSHEnvIdentifier.MatchString(key) {
			return "", "", errors.New("ssh: invalid Windows environment key")
		}
	}
	payload, err := json.Marshal(map[string]interface{}{"binary": binary, "workspace": workspace, "port": port, "env": env})
	if err != nil {
		return "", "", err
	}
	child := "powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File \"" + strings.ReplaceAll(session, "/", `\`) + `\supervisor.ps1` + "\""
	command := windowsPowerShellCommand(windowsPrivateDirectoryScript(session) + `
$payload=[Console]::In.ReadToEnd()
$file=Join-Path $dir 'launch.json'
if (Test-Path -LiteralPath (Join-Path $dir 'agentctl.identity.json')) { throw 'Session already has a controller identity' }
[IO.File]::WriteAllText($file,$payload)
$result=Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine=` + windowsPowerShellQuote(child) + `;CurrentDirectory=$dir}
if ($result.ReturnValue -ne 0) { Remove-Item -LiteralPath $file -Force; throw 'Detached controller launch failed' }
$deadline=[DateTime]::UtcNow.AddSeconds(15)
while ([DateTime]::UtcNow -lt $deadline) {
 $pidFile=Join-Path $dir 'agentctl.pid'
 if (Test-Path -LiteralPath $pidFile) { [Console]::Write([IO.File]::ReadAllText($pidFile)); exit 0 }
 Start-Sleep -Milliseconds 100
}
throw 'Detached controller did not report its identity'`)
	return command, string(payload), nil
}

func windowsControllerSupervisor(session string) string {
	return `$session=` + windowsPowerShellQuote(session) + `
$file=Join-Path $session 'launch.json'
$config=[IO.File]::ReadAllText($file) | ConvertFrom-Json
Remove-Item -LiteralPath $file -Force
foreach ($property in $config.env.PSObject.Properties) { [Environment]::SetEnvironmentVariable($property.Name,[string]$property.Value,'Process') }
$env:AGENTCTL_PORT=[string]$config.port
$argument='--workdir "'+$config.workspace.Replace('/','\')+'"'
$process=Start-Process -FilePath $config.binary -ArgumentList $argument -WorkingDirectory $config.workspace -WindowStyle Hidden -RedirectStandardOutput (Join-Path $session 'agentctl.log') -RedirectStandardError (Join-Path $session 'agentctl.stderr.log') -PassThru
$native=Get-CimInstance Win32_Process -Filter ('ProcessId='+$process.Id)
if (-not $native) { throw 'Controller exited before identity capture' }
$owner=Invoke-CimMethod -InputObject $native -MethodName GetOwnerSid
if ($owner.ReturnValue -ne 0) { throw 'Cannot identify controller owner' }
$identity=@{pid=$process.Id;created=$native.CreationDate.ToUniversalTime().ToString('o');executable=$native.ExecutablePath;workspace=$config.workspace;owner=$owner.Sid;ticks=$process.StartTime.ToUniversalTime().Ticks.ToString()}
[IO.File]::WriteAllText((Join-Path $session 'agentctl.identity.json'),($identity | ConvertTo-Json -Compress))
[IO.File]::WriteAllText((Join-Path $session 'agentctl.pid'),[string]$process.Id)
$process.WaitForExit()`
}

func startWindowsRemoteAgentctl(ctx context.Context, client *ssh.Client, binary, workspace, session string, env map[string]string, log *logger.Logger) (int, int, error) {
	return retryRemoteAgentctlPort(pickRemoteAgentctlPort, func(port int) (int, error) {
		if err := ensureWindowsPrivateDirectory(ctx, client, session); err != nil {
			return 0, err
		}
		if err := sftpUploadBytes(client, session+"/supervisor.ps1", windowsScriptFile(windowsControllerSupervisor(session)), 0600, ctx); err != nil {
			return 0, err
		}
		command, payload, err := windowsLaunchCommand(binary, workspace, session, port, env)
		if err != nil {
			return 0, err
		}
		out, _, err := runSSHCommandStdin(ctx, client, command, strings.NewReader(payload))
		if err != nil {
			return 0, fmt.Errorf("ssh: detached Windows controller launch: %w", err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil || pid <= 0 {
			return 0, errors.New("ssh: invalid Windows controller PID")
		}
		return awaitRemoteAgentctlReady(ctx, client, session, port, pid, sshAgentctlReadyTimeout, sshAgentctlReadyPoll, log)
	})
}

func windowsIdentityScript(pid int, session, task string) string {
	script := `$session=` + windowsPowerShellQuote(session) + `
$identity=[IO.File]::ReadAllText((Join-Path $session 'agentctl.identity.json')) | ConvertFrom-Json
if ([int]$identity.pid -ne ` + strconv.Itoa(pid) + `) { throw 'Controller identity PID changed' }
$process=Get-CimInstance Win32_Process -Filter 'ProcessId=` + strconv.Itoa(pid) + `'
if (-not $process) { [Console]::Write('absent'); exit 0 }
$owner=Invoke-CimMethod -InputObject $process -MethodName GetOwnerSid
if ($owner.ReturnValue -ne 0 -or $owner.Sid -ne $identity.owner -or $owner.Sid -ne [Security.Principal.WindowsIdentity]::GetCurrent().User.Value) { throw 'Controller owner changed' }
if ($process.CreationDate.ToUniversalTime().ToString('o') -ne $identity.created -or $process.ExecutablePath -ne $identity.executable) { throw 'Controller process identity changed' }
if ([IO.Path]::GetFileName($process.ExecutablePath) -notlike 'agentctl*.exe') { throw 'Not a controller executable' }
if ([int]([IO.File]::ReadAllText((Join-Path $session 'agentctl.pid'))) -ne [int]$identity.pid) { throw 'Controller PID file changed' }
`
	if task != "" {
		script += "if ($identity.workspace.Replace('\\','/').TrimEnd('/') -ne " + windowsPowerShellQuote(strings.TrimRight(task, "/")) + ") { throw 'Controller workspace changed' }"
	}
	return script
}

func verifyWindowsRemoteIdentity(ctx context.Context, client *ssh.Client, pid int, session, task string) (bool, error) {
	out, _, err := runSSHCommand(ctx, client, windowsPowerShellCommand(windowsIdentityScript(pid, session, task)+"\n[Console]::Write('owned')"))
	if err != nil {
		return false, fmt.Errorf("ssh: Windows process identity is unverified: %w", err)
	}
	switch strings.TrimSpace(out) {
	case "owned":
		return true, nil
	case sshWindowsProcessAbsent:
		return false, nil
	default:
		return false, errors.New("ssh: invalid Windows process identity response")
	}
}

func windowsStopCommand(session string, pid int) string {
	if pid <= 0 {
		return windowsPowerShellCommand("throw 'Controller identity required for cleanup'")
	}
	return windowsPowerShellCommand(windowsIdentityScript(pid, session, "") + `
$handle=Get-Process -Id $identity.pid -ErrorAction Stop
if ($handle.StartTime.ToUniversalTime().Ticks.ToString() -ne $identity.ticks -or $handle.Path -ne $identity.executable) { throw 'Controller changed before stop' }
Stop-Process -InputObject $handle -Force -ErrorAction Stop
if (-not $handle.WaitForExit(5000)) { throw 'Controller did not stop' }
` + windowsRemoveSessionScript(session))
}

func windowsRemoveSessionScript(session string) string {
	return `$target=[IO.Path]::GetFullPath(` + windowsPowerShellQuote(session) + `)
if ($target -notmatch '[\\/]\.kandev[\\/]sessions[\\/][^\\/]+$') { throw 'Cleanup path is not a session directory' }
if (Test-Path -LiteralPath $target) {
 $item=Get-Item -LiteralPath $target -Force
 if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Session is a reparse point' }
` + windowsRemoveTreeScript() + `
}`
}

func windowsReadFileCommand(filename string, tail int) string {
	selection := "-Raw"
	if tail > 0 {
		selection = "-Tail " + strconv.Itoa(tail)
	}
	return windowsPowerShellCommand("Get-Content -LiteralPath " + windowsPowerShellQuote(filename) + " " + selection)
}

func probeWindowsRemoteLiveness(ctx context.Context, client *ssh.Client, pid int) (bool, error) {
	command := windowsPowerShellCommand(fmt.Sprintf("if (Get-CimInstance Win32_Process -Filter 'ProcessId=%d') { [Console]::Write('alive') } else { [Console]::Write('absent') }", pid))
	out, _, err := runSSHCommand(ctx, client, command)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(out) {
	case sshWindowsProcessAlive:
		return true, nil
	case sshWindowsProcessAbsent:
		return false, nil
	default:
		return false, errors.New("ssh: invalid Windows liveness response")
	}
}
