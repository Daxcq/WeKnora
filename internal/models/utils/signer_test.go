package utils

import (
	"crypto/md5"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSign_StaticHeaders(t *testing.T) {
	headers := Sign("appid-1", "secret-key", "req-123", `{"q":"hello"}`)

	assert.Equal(t, "appid-1", headers["X-APPID"])
	assert.Equal(t, "secret-key", headers["X-API-Key"])
	assert.Equal(t, "req-123", headers["X-Request-ID"])

	// Timestamp is a unix seconds value close to now.
	ts, err := strconv.ParseInt(headers["X-Timestamp"], 10, 64)
	require.NoError(t, err)
	assert.InDelta(t, time.Now().Unix(), ts, 5)

	// Nonce is exactly nonceLength chars, all from the allowed alphabet.
	assert.Len(t, headers["X-Nonce"], nonceLength)
	assert.Regexp(t, regexp.MustCompile(`^[a-zA-Z0-9]{16}$`), headers["X-Nonce"])

	// Signature is a 32-char lowercase hex md5 digest.
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), headers["X-Signature"])
}

// TestSign_SignatureMatchesAlgorithm recomputes the signature from the returned
// nonce/timestamp and asserts it matches, pinning the exact signing algorithm.
func TestSign_SignatureMatchesAlgorithm(t *testing.T) {
	appID, apiKey, reqID, body := "app", "key", "rid", `{"a":1}`
	headers := Sign(appID, apiKey, reqID, body)

	params := map[string]string{
		"x-appid":      appID,
		"x-api-key":    apiKey,
		"x-request-id": reqID,
		"x-timestamp":  headers["X-Timestamp"],
		"x-nonce":      headers["X-Nonce"],
		"body":         md5Hex(body),
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, rfc3986Encode(k)+"="+rfc3986Encode(params[k]))
	}
	want := md5Hex(strings.Join(parts, "&"))

	assert.Equal(t, want, headers["X-Signature"])
}

// TestSign_EmptyBody verifies an empty body is hashed as "{}" in the signature.
func TestSign_EmptyBody(t *testing.T) {
	appID, apiKey, reqID := "app", "key", "rid"
	headers := Sign(appID, apiKey, reqID, "")

	// Recompute the signature using the "{}" body hash (the empty-body
	// substitution) and the returned nonce/timestamp.
	params := map[string]string{
		"x-appid":      appID,
		"x-api-key":    apiKey,
		"x-request-id": reqID,
		"x-timestamp":  headers["X-Timestamp"],
		"x-nonce":      headers["X-Nonce"],
		"body":         md5Hex("{}"),
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, rfc3986Encode(k)+"="+rfc3986Encode(params[k]))
	}
	want := md5Hex(strings.Join(parts, "&"))

	assert.Equal(t, want, headers["X-Signature"])
}

func TestMD5Hex(t *testing.T) {
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", md5Hex(""))
	assert.Equal(t, "9a0364b9e99bb480dd25e1f0284c8555", md5Hex("content"))

	// Cross-check against the standard library.
	want := fmt.Sprintf("%x", md5.Sum([]byte("weknora")))
	assert.Equal(t, want, md5Hex("weknora"))
}

func TestGenerateNonce(t *testing.T) {
	n := generateNonce(20)
	assert.Len(t, n, 20)
	for _, r := range n {
		assert.Contains(t, nonceChars, string(r))
	}

	assert.Empty(t, generateNonce(0))

	// Two nonces are overwhelmingly unlikely to collide.
	assert.NotEqual(t, generateNonce(16), generateNonce(16))
}

func TestRFC3986Encode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"unreserved-_.~", "unreserved-_.~"},
		{"ABCabc123", "ABCabc123"},
		{"a b", "a%20b"},
		{"a+b", "a%2Bb"},
		{"a/b?c=d&e", "a%2Fb%3Fc%3Dd%26e"},
		{"100%", "100%25"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, rfc3986Encode(tt.in))
		})
	}
}
