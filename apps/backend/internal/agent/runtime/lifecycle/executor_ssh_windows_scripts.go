package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

func runWindowsScript(ctx context.Context, client *ssh.Client, script string, env map[string]string) (string, string, error) {
	for key := range env {
		if !posixSSHEnvIdentifier.MatchString(key) {
			return "", "", errors.New("ssh: invalid Windows environment key")
		}
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return "", "", err
	}
	wrapped := `$environment=[Console]::In.ReadToEnd() | ConvertFrom-Json
if ($environment) { foreach ($property in $environment.PSObject.Properties) { [Environment]::SetEnvironmentVariable($property.Name,[string]$property.Value,'Process') } }
` + script
	return runSSHCommandStdin(ctx, client, windowsPowerShellCommand(wrapped), strings.NewReader(string(payload)))
}

func windowsGitCommand(directory string, args ...string) string {
	command := "& git.exe -C " + windowsPowerShellQuote(directory)
	for _, arg := range args {
		command += " " + windowsPowerShellQuote(arg)
	}
	return command + "\nif ($LASTEXITCODE -ne 0) { throw 'Git operation failed' }"
}

func windowsPrepareScript(req *ExecutorCreateRequest, task string) (string, error) {
	if len(req.RemoteContributions) > 0 || len(req.ContributionDestinations) > 0 {
		return "", errors.New("ssh: Windows contribution checkouts require a native PowerShell prepare script")
	}
	stored := strings.TrimSpace(strings.ReplaceAll(getMetadataString(req.Metadata, MetadataKeySetupScript), "\r\n", "\n"))
	managed := strings.TrimSpace(strings.ReplaceAll(DefaultPrepareScript(executorTypeSSH), "\r\n", "\n"))
	if stored != "" && stored != managed {
		if strings.Contains(stored, "{{") {
			return "", errors.New("ssh: native Windows prepare scripts must use PowerShell and literal paths")
		}
		return "Set-Location -LiteralPath " + windowsPowerShellQuote(task) + "\n" + stored, nil
	}
	url := getMetadataString(req.Metadata, "repository_clone_url")
	if url == "" {
		return "", nil
	}
	branch := getMetadataString(req.Metadata, MetadataKeyBaseBranch)
	if branch == "" {
		branch = getMetadataString(req.Metadata, "repository_branch")
	}
	branch = strings.TrimPrefix(branch, "origin/")
	if branch == "" {
		return "", errors.New("ssh: repository base branch is required")
	}
	script := `$workspace=` + windowsPowerShellQuote(task) + `
$url=` + windowsPowerShellQuote(url) + `
if (-not (Test-Path -LiteralPath (Join-Path $workspace '.git'))) {
 & git.exe init -- $workspace
 if ($LASTEXITCODE -ne 0) { throw 'Git init failed' }
 & git.exe -C $workspace remote add origin $url
 if ($LASTEXITCODE -ne 0) { throw 'Git origin setup failed' }
}
$origin=& git.exe -C $workspace config --get remote.origin.url
if ($LASTEXITCODE -ne 0 -or $origin -ne $url) { throw 'Repository origin identity conflict' }
$exclude=Join-Path $workspace '.git/info/exclude'
if (-not (Test-Path -LiteralPath $exclude)) { [IO.File]::WriteAllText($exclude,'') }
if (-not ([IO.File]::ReadAllLines($exclude) -contains '/.kandev/')) { [IO.File]::AppendAllText($exclude,"` + "`n/.kandev/`n" + `") }
` + windowsGitCommand(task, "fetch", "--no-tags", "origin", "+refs/heads/"+branch+":refs/remotes/origin/"+branch) + `
& git.exe -C $workspace rev-parse --verify --quiet HEAD | Out-Null
if ($LASTEXITCODE -ne 0) {
` + windowsGitCommand(task, "checkout", "-b", branch, "refs/remotes/origin/"+branch) + `
}
$LASTEXITCODE=0
`
	for _, key := range []string{"git_user_name", "git_user_email"} {
		if value := getMetadataString(req.Metadata, key); value != "" {
			config := "user.name"
			if key == "git_user_email" {
				config = "user.email"
			}
			script += "\n" + windowsGitCommand(task, "config", config, value)
		}
	}
	if feature := getMetadataString(req.Metadata, MetadataKeyWorktreeBranch); feature != "" && feature != branch {
		script += "\n& git.exe -C $workspace show-ref --verify --quiet " + windowsPowerShellQuote("refs/heads/"+feature) + "\nif ($LASTEXITCODE -eq 0) {\n" + windowsGitCommand(task, "checkout", feature) + "\n} else {\n" + windowsGitCommand(task, "checkout", "-b", feature) + "\n}\n"
	}
	script += "\nSet-Location -LiteralPath $workspace\n" + getMetadataString(req.Metadata, "repository_setup_script")
	return script, nil
}

func (r *SSHExecutor) runWindowsPrepareScript(ctx context.Context, client *ssh.Client, task string, req *ExecutorCreateRequest, binary string) error {
	script, err := windowsPrepareScript(req, task)
	if err != nil {
		return err
	}
	if script == "" {
		return nil
	}
	env := sshRemoteContributionEnv(req, binary)
	out, stderr, err := runWindowsScript(ctx, client, script, env)
	if err != nil {
		r.report(req.OnProgress, "Running prepare script", PrepareStepFailed, redactSSHScriptOutput(out+"\n"+stderr, env))
		return errors.New("ssh: Windows prepare script failed")
	}
	r.report(req.OnProgress, "Running prepare script", PrepareStepCompleted, redactSSHScriptOutput(out+"\n"+stderr, env))
	return nil
}

func verifyWindowsPrimaryCheckout(ctx context.Context, client *ssh.Client, task string, req *ExecutorCreateRequest) error {
	url := getMetadataString(req.Metadata, "repository_clone_url")
	if url == "" {
		return nil
	}
	script := `$root=& git.exe -C ` + windowsPowerShellQuote(task) + ` rev-parse --show-toplevel
if ($LASTEXITCODE -ne 0 -or [IO.Path]::GetFullPath($root) -ne [IO.Path]::GetFullPath(` + windowsPowerShellQuote(task) + `)) { throw 'Repository root does not match workspace' }
$origin=& git.exe -C ` + windowsPowerShellQuote(task) + ` config --get remote.origin.url
if ($LASTEXITCODE -ne 0 -or $origin -ne ` + windowsPowerShellQuote(url) + `) { throw 'Repository origin does not match' }
` + windowsGitCommand(task, "rev-parse", "--verify", "HEAD")
	_, _, err := runWindowsScript(ctx, client, script, nil)
	if err != nil {
		return fmt.Errorf("ssh: Windows primary checkout verification failed: %w", err)
	}
	return nil
}

func windowsPlatformForPath(directory string) SSHRemotePlatform {
	if isWindowsRemotePath(directory) {
		return SSHRemotePlatform{GOOS: sshRemoteGOOSWindows, GOARCH: sshRemoteGOARCHAMD64}
	}
	return SSHRemotePlatform{}
}
