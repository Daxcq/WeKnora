package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A wildcard origin must never be paired with Allow-Credentials: that
// combination lets any site read authenticated responses cross-origin.
func TestCORSConfig_WildcardDisallowsCredentials(t *testing.T) {
	t.Setenv("WEKNORA_ALLOWED_ORIGINS", "")

	cfg := corsConfig()

	assert.Equal(t, []string{"*"}, cfg.AllowOrigins)
	assert.False(t, cfg.AllowCredentials)
}

func TestCORSConfig_AllowlistEnablesCredentials(t *testing.T) {
	t.Setenv("WEKNORA_ALLOWED_ORIGINS", "https://app.example.com, https://admin.example.com")

	cfg := corsConfig()

	assert.Equal(t, []string{"https://app.example.com", "https://admin.example.com"}, cfg.AllowOrigins)
	assert.True(t, cfg.AllowCredentials)
}

// "*" inside the allowlist is dropped rather than turned into a
// credentialed wildcard policy.
func TestCORSConfig_WildcardInAllowlistIgnored(t *testing.T) {
	t.Setenv("WEKNORA_ALLOWED_ORIGINS", "*")

	cfg := corsConfig()

	assert.Equal(t, []string{"*"}, cfg.AllowOrigins)
	assert.False(t, cfg.AllowCredentials)
}
