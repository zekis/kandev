package launchwrapper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripControlCredentialsExcludesBrokerSigningKey(t *testing.T) {
	const key = "KANDEV_GITHUB_CREDENTIAL_BROKER_REISSUE_SIGNING_KEY"
	env := []string{key + "=fake-server-key", "kandev_github_credential_broker_reissue_signing_key=fake-override", "GITHUB_TOKEN=explicit", "KANDEV_GITHUB_CREDENTIAL_REISSUE_CAPABILITY=scoped"}
	got := StripControlCredentials(env)
	require.Equal(t, env[2:], got)
}
