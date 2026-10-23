package agent

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

const (
	// failureFallback is the wording used before pi's own reason was readable.
	// A missing or unusable errorMessage must keep exactly this accurate text.
	failureFallback = "模型调用失败或被中断；已有内容保留"
	// failureDetailPrefix introduces the provider's bounded reason.
	failureDetailPrefix = "模型调用失败；已有内容保留："
	// maxFailureDetailRunes bounds one stored provider message.
	maxFailureDetailRunes = 240
)

// failureMessage tells the person a round failed and, when pi reported a
// usable reason, why. It never changes the failure classification itself.
func failureMessage(detail string) string {
	if detail == "" {
		return failureFallback
	}
	return failureDetailPrefix + detail
}

// safeFailureDetail turns pi's errorMessage into the bounded single-line reason
// worth storing. pi forwards the provider envelope verbatim ("429 {...}"), which
// hides the actionable text and carries identifiers nobody needs, so only the
// provider's own message (plus a plain HTTP status) survives. Anything that is
// not a recognizable envelope is normalized as plain text, credential-shaped
// fragments are masked, and unrecognizable envelopes are dropped rather than
// stored raw.
func safeFailureDetail(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	detail := raw
	if index := strings.IndexByte(raw, '{'); index >= 0 {
		message, ok := providerFailureMessage(raw[index:])
		if !ok {
			return ""
		}
		detail = message
		if status := strings.TrimSpace(raw[:index]); httpStatus(status) {
			detail = status + " " + message
		}
	}
	detail = maskFailureCredentials(singleLine(detail))
	if runes := []rune(detail); len(runes) > maxFailureDetailRunes {
		detail = strings.TrimSpace(string(runes[:maxFailureDetailRunes])) + "…"
	}
	return detail
}

// providerFailureMessage reads the reason out of a provider error envelope.
// Only message fields are read; request ids, error types and anything else a
// provider echoes are never copied into stored state.
func providerFailureMessage(payload string) (string, bool) {
	var v struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(payload), &v) != nil {
		return "", false
	}
	if message := strings.TrimSpace(v.Error.Message); message != "" {
		return message, true
	}
	if message := strings.TrimSpace(v.Message); message != "" {
		return message, true
	}
	return "", false
}

func httpStatus(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// singleLine keeps a stored error readable: no control characters, no line
// breaks, no repeated whitespace.
func singleLine(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

var failureCredentialPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{6,}`), "bearer [已隐藏]"},
	{regexp.MustCompile(`sk-[A-Za-z0-9_-]{6,}`), "sk-[已隐藏]"},
	{regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|secret|password)(\s*[:=]\s*)\S+`), "${1}${2}[已隐藏]"},
}

// maskFailureCredentials removes credential-shaped fragments a provider may
// echo back, so a stored failure never becomes a place to read a secret.
func maskFailureCredentials(s string) string {
	for _, c := range failureCredentialPatterns {
		s = c.pattern.ReplaceAllString(s, c.replacement)
	}
	return s
}
