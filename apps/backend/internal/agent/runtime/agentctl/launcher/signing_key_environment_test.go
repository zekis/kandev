package launcher

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForwardedRuntimeEnvironmentExcludesServerSigningKey(t *testing.T) {
	const key = "KANDEV_GITHUB_CREDENTIAL_BROKER_REISSUE_SIGNING_KEY"
	got := environmentWithOverrides([]string{key + "=fake-parent", "GITHUB_TOKEN=explicit"}, key+"=fake-override", "AGENTCTL_BOOTSTRAP_NONCE=runtime-control")
	require.Equal(t, []string{"GITHUB_TOKEN=explicit", "AGENTCTL_BOOTSTRAP_NONCE=runtime-control"}, got)
}
