package launchwrapper

import "strings"

const brokerSigningKeyEnv = "KANDEV_GITHUB_CREDENTIAL_BROKER_REISSUE_SIGNING_KEY"

// StripControlCredentials removes control-plane credentials from child environments.
// These values belong to the host process and cannot be restored by profile overrides.
func StripControlCredentials(env []string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "AGENTCTL_BOOTSTRAP_NONCE", "AGENTCTL_AUTH_TOKEN", "KANDEV_DESKTOP_HEALTH_TOKEN", brokerSigningKeyEnv:
			continue
		}
		result = append(result, entry)
	}
	return result
}

// StripServerCredentials keeps backend-only signing authority out of runtime
// children while retaining their own control-plane authentication contract.
func StripServerCredentials(env []string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, brokerSigningKeyEnv) {
			result = append(result, entry)
		}
	}
	return result
}

// RemoveServerCredentials removes server signing authority after all task
// environment sources have been composed, including explicit overrides.
func RemoveServerCredentials(env map[string]string) {
	for key := range env {
		if strings.EqualFold(key, brokerSigningKeyEnv) {
			delete(env, key)
		}
	}
}
