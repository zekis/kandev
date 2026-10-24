package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
)

const windowsSSHCommandLimit = 7500
const windowsSSHEncodedPrefix = "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand "

func windowsLongCommandScript(command string) (string, bool, error) {
	if len(command) <= windowsSSHCommandLimit || !strings.HasPrefix(command, windowsSSHEncodedPrefix) {
		return "", false, nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, windowsSSHEncodedPrefix))
	if err != nil || len(data)%2 != 0 {
		return "", true, errors.New("ssh: malformed encoded Windows command")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return string(utf16.Decode(units)), true, nil
}

// Windows OpenSSH may run exec through cmd.exe, whose command line limit is
// smaller than a useful encoded PowerShell script. Only code is staged here;
// the caller's data and credential environment continue to travel on stdin.
func runLongWindowsSSHCommand(ctx context.Context, client *ssh.Client, script string, stdin io.Reader) (string, string, error) {
	home, err := expandRemoteHome(ctx, client, "~")
	if err != nil {
		return "", "", err
	}
	directory := home + "/.kandev/ssh-commands/" + rand.Text()
	if _, err := normalizeWindowsRemotePath(directory); err != nil {
		return "", "", err
	}
	bootstrap := windowsPowerShellCommand(windowsPrivateDirectoryScript(directory))
	if len(bootstrap) > windowsSSHCommandLimit {
		return "", "", errors.New("ssh: Windows home is too long for private command staging")
	}
	if _, _, err := runRawSSHCommandStdin(ctx, client, bootstrap, nil); err != nil {
		return "", "", err
	}
	filename := directory + "/command.ps1"
	defer removeWindowsCommandFile(client, filename, directory)
	if err := windowsSFTPUpload(ctx, client, filename, []byte("\xef\xbb\xbf"+script)); err != nil {
		return "", "", err
	}
	command := `powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "` + filename + `"`
	return runRawSSHCommandStdin(ctx, client, command, stdin)
}

func removeWindowsCommandFile(client *ssh.Client, filename, directory string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sftp, err := newSFTPClientContext(ctx, client)
	if err != nil {
		return
	}
	defer func() { _ = sftp.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = sftp.Close() })
	defer stop()
	_ = sftp.Remove("/" + filename)
	_ = sftp.RemoveDirectory("/" + directory)
}
