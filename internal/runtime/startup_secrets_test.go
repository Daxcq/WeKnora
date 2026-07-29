package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInsecureSecretNames(t *testing.T) {
	t.Run("placeholders from the env templates are flagged", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "weknora-jwt-secret")
		t.Setenv("SYSTEM_AES_KEY", "weknora-system-aes-key-32bytes!!")

		assert.Equal(t, []string{"JWT_SECRET", "SYSTEM_AES_KEY"}, insecureSecretNames())
	})

	t.Run("generated secrets are accepted", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "N0tAPlaceholderSecretValue")
		t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")

		assert.Empty(t, insecureSecretNames())
	})

	t.Run("unset secrets are not flagged", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "")
		t.Setenv("SYSTEM_AES_KEY", "")

		assert.Empty(t, insecureSecretNames())
	})
}
