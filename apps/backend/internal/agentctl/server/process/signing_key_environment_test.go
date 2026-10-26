package process

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChildProcessEnvironmentExcludesServerSigningKey(t *testing.T) {
	const key = "KANDEV_GITHUB_CREDENTIAL_BROKER_REISSUE_SIGNING_KEY"
	t.Setenv(key, "fake-parent")
	env := mergeEnvWithStrip(map[string]string{key: "fake-override", "GITHUB_TOKEN": "explicit"}, nil)
	for _, entry := range env {
		require.False(t, strings.HasPrefix(entry, key+"="), "server signing key must be absent")
	}
	require.Contains(t, env, "GITHUB_TOKEN=explicit")
}
