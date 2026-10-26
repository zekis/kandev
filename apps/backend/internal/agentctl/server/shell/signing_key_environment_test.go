package shell

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellEnvironmentExcludesServerSigningKey(t *testing.T) {
	const key = "KANDEV_GITHUB_CREDENTIAL_BROKER_REISSUE_SIGNING_KEY"
	t.Setenv(key, "fake-parent-key")
	for _, overrides := range []map[string]string{nil, {key: "fake-override", "GITHUB_TOKEN": "explicit-token", "KANDEV_GITHUB_CREDENTIAL_REISSUE_CAPABILITY": "scoped-capability"}} {
		env := buildShellEnv(t.TempDir(), overrides)
		for _, entry := range env {
			require.False(t, strings.HasPrefix(entry, key+"="), "server signing key must be absent")
		}
		if overrides != nil {
			require.Contains(t, env, "GITHUB_TOKEN=explicit-token")
			require.Contains(t, env, "KANDEV_GITHUB_CREDENTIAL_REISSUE_CAPABILITY=scoped-capability")
		}
	}
}
