package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func sshSafeWindowsSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\:\x00\r\n\"<>|?*")
}

func sameSSHWorkspacePath(left, right string) bool {
	if !isWindowsRemotePath(left) || !isWindowsRemotePath(right) {
		return left == right
	}
	a, errA := normalizeWindowsRemotePath(left)
	b, errB := normalizeWindowsRemotePath(right)
	return errA == nil && errB == nil && strings.EqualFold(a, b)
}

func chmodSFTPFile(client *sftp.Client, filename string, mode os.FileMode) error {
	if isWindowsRemotePath(filename) {
		return nil
	}
	return client.Chmod(filename, mode)
}

func remoteReadFileCommand(filename string, tail int) string {
	if isWindowsRemotePath(filename) {
		return windowsReadFileCommand(filename, tail)
	}
	if tail > 0 {
		return fmt.Sprintf("tail -n %d %s 2>/dev/null", tail, shellQuote(filename))
	}
	return "cat " + shellQuote(filename) + " 2>/dev/null"
}

func windowsDirectoryExists(ctx context.Context, client *ssh.Client, directory string) (bool, error) {
	command := windowsPowerShellCommand("if (Test-Path -LiteralPath " + windowsPowerShellQuote(directory) + " -PathType Container) { [Console]::Write('exists') } else { [Console]::Write('absent') }")
	out, _, err := runSSHCommand(ctx, client, command)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(out) {
	case "exists":
		return true, nil
	case sshWindowsProcessAbsent:
		return false, nil
	default:
		return false, errors.New("ssh: invalid Windows directory probe")
	}
}

func ensureWindowsDirectoryExists(ctx context.Context, client *ssh.Client, directory string) error {
	exists, err := windowsDirectoryExists(ctx, client, directory)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("ssh: existing Windows task directory is unavailable")
	}
	return nil
}

func windowsHelperPath(ctx context.Context, client *ssh.Client, digest string) (string, error) {
	root, err := expandRemoteHome(ctx, client, "~/.kandev/bin")
	if err != nil {
		return "", err
	}
	return root + "/agentctl-" + digest + ".exe", nil
}

func windowsHelperCached(ctx context.Context, client *ssh.Client, filename, digest string) (bool, error) {
	script := "if (Test-Path -LiteralPath " + windowsPowerShellQuote(filename) + " -PathType Leaf) { (Get-FileHash -LiteralPath " + windowsPowerShellQuote(filename) + " -Algorithm SHA256).Hash }"
	out, _, err := runSSHCommand(ctx, client, windowsPowerShellCommand(script))
	return strings.EqualFold(strings.TrimSpace(out), digest), err
}

func ensureWindowsHelper(ctx context.Context, client *ssh.Client, digest string, data []byte) (string, error) {
	filename, err := windowsHelperPath(ctx, client, digest)
	if err != nil {
		return "", err
	}
	cached, err := windowsHelperCached(ctx, client, filename, digest)
	if err != nil {
		return "", err
	}
	if cached {
		return filename, nil
	}
	if err := ensureWindowsPrivateDirectory(ctx, client, parentDir(filename)); err != nil {
		return "", err
	}
	if err := sftpUploadBytes(client, filename, data, 0755, ctx); err != nil {
		return "", err
	}
	cached, err = windowsHelperCached(ctx, client, filename, digest)
	if err != nil {
		return "", err
	}
	if !cached {
		return "", errors.New("ssh: Windows helper checksum verification failed")
	}
	return filename, nil
}

func windowsSFTPUpload(ctx context.Context, client *ssh.Client, filename string, data []byte) error {
	native, err := normalizeWindowsRemotePath(filename)
	if err != nil {
		return err
	}
	c, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	parent, err := c.RealPath("/" + parentDir(native))
	if err != nil {
		return err
	}
	canonical, err := normalizeWindowsRemotePath(parent)
	if err != nil || !strings.EqualFold(canonical, parentDir(native)) {
		return errors.New("ssh: Windows SFTP path identity changed")
	}
	tmp := native + ".tmp." + fmt.Sprint(time.Now().UnixNano())
	file, err := c.Create("/" + tmp)
	if err != nil {
		return err
	}
	defer func() { _ = c.Remove("/" + tmp) }()
	written, err := file.Write(data)
	if err != nil {
		_ = file.Close()
		return err
	}
	if written != len(data) {
		_ = file.Close()
		return errors.New("ssh: incomplete Windows SFTP write")
	}
	if err := file.Close(); err != nil {
		return err
	}
	_, _, err = runSSHCommand(ctx, client, windowsPowerShellCommand("Move-Item -LiteralPath "+windowsPowerShellQuote(tmp)+" -Destination "+windowsPowerShellQuote(native)+" -Force"))
	return err
}
