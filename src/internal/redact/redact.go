// Package redact removes secrets and normalizes private filesystem locations.
package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
)

var namedCredential = regexp.MustCompile(`(?i)(\b(?:cf[-_]access[-_]client[-_](?:id|secret)|(?:cloudflare|openai|github|aws|azure|google)[-_](?:api[-_]?(?:key|token)|access[-_]?token|refresh[-_]?token|client[-_]?secret)|api[-_]?(?:key|token)|access[-_]?token|refresh[-_]?token|client[-_]?secret|password|authorization|cookie|set-cookie)\b[ \t]*[:=][ \t]*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(sk-[a-z0-9_-]{16,}|gh[pousr]_[a-z0-9_]{16,})\b`),
	regexp.MustCompile(`(?i)\bcfast_[a-z0-9_-]{20,}\b`),
	regexp.MustCompile(`(?i)\b[a-f0-9]{32}\.access\b`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`(?i)\b(bearer\s+)[a-z0-9._~+/-]+=*`),
	regexp.MustCompile(`(?i)\b(postgres(?:ql)?|mysql|mongodb(?:\+srv)?):\/\/[^\s"']+`),
	regexp.MustCompile(`(?s)-----BEGIN(?: [A-Z]+)? PRIVATE KEY-----.*?-----END(?: [A-Z]+)? PRIVATE KEY-----`),
	regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\b`),
}

// Text returns a bounded, redacted representation suitable for persistence.
func Text(value string, maxBytes int) string {
	redacted := value
	for _, pattern := range patterns {
		redacted = pattern.ReplaceAllString(redacted, "[REDACTED]")
	}
	redacted = namedCredential.ReplaceAllString(redacted, "$1[REDACTED]")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		redacted = strings.ReplaceAll(redacted, home, "~")
	}
	if maxBytes > 0 && len(redacted) > maxBytes {
		redacted = redacted[:maxBytes] + "…"
	}
	return redacted
}

// Hash produces a stable non-reversible identity for a local path or payload.
func Hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// DisplayPath keeps only the final two path elements for UI evidence.
func DisplayPath(path string) string {
	clean := strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	parts := strings.Split(clean, "/")
	if len(parts) <= 2 {
		return clean
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
