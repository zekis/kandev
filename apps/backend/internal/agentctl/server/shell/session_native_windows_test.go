//go:build windows

package shell

import "testing"

func TestNativeWindowsShellSession(t *testing.T) {
	session, err := NewSession(DefaultConfig(t.TempDir()), newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Stop(); err != nil {
			t.Error(err)
		}
	}()
	if err := session.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Write([]byte("Write-Output 'native-shell'\r\n")); err != nil {
		t.Fatal(err)
	}
}
