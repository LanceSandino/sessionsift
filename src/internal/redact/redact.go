// Package redact removes secrets and normalizes private filesystem locations.
// @module Redact is the single privacy filter applied before any record is persisted or shared.
package redact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
	"sync"
)

var namedCredential = regexp.MustCompile(`(?i)((?:cf[-_]access[-_]client[-_](?:id|secret)|api[-_]?(?:key|token)|access[-_]?(?:key|token)|refresh[-_]?token|auth[-_]?token|session[-_]?token|id[-_]?token|secret[-_]?(?:key|access[-_]?key)|private[-_]?key|client[-_]?secret|secret|password|passwd|passphrase|token|credentials?|authorization|cookie|set-cookie)\b["']?[ \t]*[:=][ \t]*)("[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)

// redactNamed keeps the original quoting so redacted JSON stays well-formed.
func redactNamed(match string) string {
	parts := namedCredential.FindStringSubmatch(match)
	if len(parts) != 3 {
		return "[REDACTED]"
	}
	switch {
	case strings.HasPrefix(parts[2], `"`):
		return parts[1] + `"[REDACTED]"`
	case strings.HasPrefix(parts[2], `'`):
		return parts[1] + `'[REDACTED]'`
	default:
		return parts[1] + "[REDACTED]"
	}
}

// tokenRule is one provider credential shape and the lowercase literals that
// must appear before the (comparatively slow) expression is worth running.
type tokenRule struct {
	hints   []string
	pattern *regexp.Regexp
}

var tokenRules = []tokenRule{
	{[]string{"sk-", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"}, regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]{16,}|gh[pousr]_[a-z0-9_]{16,}|github_pat_[a-z0-9_]{22,})\b`)},
	{[]string{"_live_", "_test_"}, regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}\b`)},
	{[]string{"xox"}, regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}\b`)},
	{[]string{"aiza"}, regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}`)},
	{[]string{"glpat-"}, regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`)},
	{[]string{"npm_"}, regexp.MustCompile(`\bnpm_[A-Za-z0-9]{30,}\b`)},
	{[]string{"hf_"}, regexp.MustCompile(`\bhf_[A-Za-z0-9]{30,}\b`)},
	{[]string{"cfast_"}, regexp.MustCompile(`(?i)\bcfast_[a-z0-9_-]{20,}\b`)},
	{[]string{".access"}, regexp.MustCompile(`(?i)\b[a-f0-9]{32}\.access\b`)},
	{[]string{"akia", "asia"}, regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{[]string{"bearer"}, regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/-]+=*`)},
	{[]string{"://"}, regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|amqp)://[^\s"']+`)},
	// Private keys, including a block cut off before its END marker.
	{[]string{"private key"}, regexp.MustCompile(`(?s)-----BEGIN(?: [A-Z]+)? PRIVATE KEY-----(?:.*?-----END(?: [A-Z]+)? PRIVATE KEY-----|.*$)`)},
	// JWTs, including one cut off after its payload segment.
	{[]string{"eyj"}, regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)?\b`)},
}

// accountPath matches the user segment of a home directory in POSIX, Windows,
// forward-slash and JSON-escaped spellings, including other local accounts.
var accountPath = regexp.MustCompile(`(?i)((?:^|[\s"'=(:/\x60])(?:/Users/|/home/|/mnt/[a-z]/Users/|[a-z]:(?:\\{1,2}|/)Users(?:\\{1,2}|/)))([^\\/\s"'\x60<>]+)`)

// scanMargin bounds redaction work on large inputs. Text redacts at most
// maxBytes+scanMargin bytes, so a credential that begins inside the kept output
// is still seen whole or matched by the cut-off-safe rules above.
const scanMargin = 4 << 10

// namedHints and pathHints let Text skip a pass when its trigger words are absent.
var (
	namedHints = []string{"key", "token", "secret", "passw", "passphrase", "auth", "cookie", "credential", "cf-access", "cf_access"}
	pathHints  = []string{"users", "home"}
)

var (
	homeOnce     sync.Once
	homePatterns []*regexp.Regexp
	homeHint     string
	keyMu        sync.RWMutex
	pathKey      []byte
)

// Text returns a bounded, redacted representation suitable for persistence.
func Text(value string, maxBytes int) string {
	if maxBytes > 0 && len(value) > maxBytes+scanMargin {
		value = truncate(value, maxBytes+scanMargin)
	}
	redacted := value
	lower := strings.ToLower(value)
	for _, rule := range tokenRules {
		if containsAny(lower, rule.hints) {
			redacted = rule.pattern.ReplaceAllString(redacted, "[REDACTED]")
		}
	}
	lower = strings.ToLower(redacted)
	if containsAny(lower, namedHints) {
		redacted = namedCredential.ReplaceAllStringFunc(redacted, redactNamed)
	}
	redacted = paths(redacted, lower)
	if maxBytes > 0 && len(redacted) > maxBytes {
		redacted = truncate(redacted, maxBytes) + "…"
	}
	return redacted
}

// Paths replaces the current home directory with ~ and any account name in a
// home-directory path with "redacted-user", in every encoding transcripts use.
// The placeholder starts with a letter so Markdown never consumes a preceding
// backslash as an escape.
func Paths(value string) string {
	return paths(value, strings.ToLower(value))
}

func paths(value, lower string) string {
	homeOnce.Do(compileHomePatterns)
	if homeHint != "" && strings.Contains(lower, homeHint) {
		for _, pattern := range homePatterns {
			value = pattern.ReplaceAllString(value, "~")
		}
	}
	if !containsAny(lower, pathHints) {
		return value
	}
	return accountPath.ReplaceAllString(value, "${1}redacted-user")
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func compileHomePatterns() {
	home, err := os.UserHomeDir()
	home = strings.TrimRight(home, `\/`)
	if err != nil || len(home) < 3 {
		return
	}
	homeHint = strings.ToLower(home[strings.LastIndexAny(home, `\/`)+1:])
	seen := map[string]bool{}
	for _, variant := range []string{home, strings.ReplaceAll(home, `\`, `/`), strings.ReplaceAll(home, `\`, `\\`), strings.ReplaceAll(home, `/`, `\`)} {
		if seen[variant] {
			continue
		}
		seen[variant] = true
		homePatterns = append(homePatterns, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(variant)))
	}
}

// Title returns a single-line, redacted, bounded label safe for session lists.
func Title(value string, limit int) string {
	value = strings.Join(strings.Fields(Text(value, limit*4)), " ")
	if ellipsis := len("…"); limit > ellipsis && len(value) > limit {
		return truncate(value, limit-ellipsis) + "…"
	}
	return value
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && limit < len(value) && !isRuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

func isRuneStart(value byte) bool { return value&0xC0 != 0x80 }

// Hash produces a stable non-reversible identity for local-only bookkeeping.
func Hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// SetPathKey installs the per-installation secret used by PathHash so shared
// path identities cannot be confirmed by hashing guessed paths.
func SetPathKey(key []byte) {
	keyMu.Lock()
	defer keyMu.Unlock()
	pathKey = append([]byte(nil), key...)
}

// PathHash returns a keyed identity for a filesystem path that may be shared.
func PathHash(path string) string {
	keyMu.RLock()
	key := pathKey
	keyMu.RUnlock()
	if len(key) == 0 {
		return Hash("sessionsift.path.v1\x00" + path)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("sessionsift.path.v1\x00" + path))
	return hex.EncodeToString(mac.Sum(nil))
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

// Workspace returns a replacer that rewrites an absolute working directory to
// its short display name in already-redacted text: native, forward-slash, and
// JSON-escaped spellings, case-insensitively. Roots and home markers are left
// alone because they carry no private layout.
func Workspace(cwd, name string) func(string) string {
	cwd = strings.TrimRight(Paths(cwd), `\/`)
	if name == "" {
		name = cwd[strings.LastIndexAny(cwd, `\/`)+1:]
	}
	if len(cwd) < 4 || cwd == "~" || !strings.ContainsAny(cwd[1:], `\/`) || name == "" {
		return func(value string) string { return value }
	}
	variants := []string{}
	seen := map[string]bool{}
	for _, variant := range []string{cwd, strings.ReplaceAll(cwd, `\`, `/`), strings.ReplaceAll(cwd, `\`, `\\`), strings.ReplaceAll(cwd, `/`, `\`), strings.ReplaceAll(cwd, `/`, `\\`)} {
		if !seen[variant] {
			seen[variant] = true
			variants = append(variants, regexp.QuoteMeta(variant))
		}
	}
	pattern := regexp.MustCompile(`(?i)(?:` + strings.Join(variants, "|") + `)`)
	hint := strings.ToLower(cwd[strings.LastIndexAny(cwd, `\/`)+1:])
	return func(value string) string {
		if !strings.Contains(strings.ToLower(value), hint) {
			return value
		}
		return pattern.ReplaceAllString(value, name)
	}
}
