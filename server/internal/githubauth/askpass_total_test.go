package githubauth

import (
	"io"
	"strings"
	"testing"
)

// totalRequest drives the credential core with a request body and reports the
// emitted answer. Only the fake test token is used.
func totalRequest(body string) (string, error) {
	env := map[string]string{envHelperHost: "github.com", envHelperFD: "3"}
	token := func() (io.Reader, error) { return strings.NewReader("test-token-not-real\n"), nil }
	out, _, e := credentialOutput(env, token, []string{helperSubcommand, "get"}, strings.NewReader(body))
	return out, e
}

// TestCredentialTotalInputIsBounded covers the reported gap: the scanner limit
// only bounds a single line, so many short lines, one long array line and CRLF
// splitting must all be rejected on the total input size before any token is
// issued.
func TestCredentialTotalInputIsBounded(t *testing.T) {
	cases := map[string]string{
		"many short scalar lines": "protocol=https\nhost=github.com\n" + strings.Repeat("wwwauth[]=Basic realm=x\n", 4000),
		"long single array line":  "protocol=https\nhost=github.com\nstate[]=" + strings.Repeat("a", 68<<10) + "\n",
		"short lines under line limit but over total": "protocol=https\nhost=github.com\n" +
			strings.Repeat("capability[]=authtype\n", 4000),
		"crlf split over total": "protocol=https\r\nhost=github.com\r\n" +
			strings.Repeat("wwwauth[]=Basic\r\n", 5000),
	}
	for name, body := range cases {
		if len(body) <= 64<<10 {
			t.Fatalf("%s: test body is %d bytes, must exceed the total bound", name, len(body))
		}
		out, e := totalRequest(body)
		if e == nil {
			t.Fatalf("%s: oversized request was accepted (out=%q)", name, out)
		}
		if strings.Contains(out, "test-token-not-real") {
			t.Fatalf("%s: token leaked for oversized request: %q", name, out)
		}
	}
}

// TestCredentialTotalInputAcceptsLegalRequest keeps the legal path working,
// including a large-but-legal array payload under the total bound.
func TestCredentialTotalInputAcceptsLegalRequest(t *testing.T) {
	for _, body := range []string{
		"protocol=https\nhost=github.com\n\n",
		"protocol=https\nhost=github.com\n" + strings.Repeat("wwwauth[]=Basic realm=x\n", 60) + "\n",
	} {
		if len(body) > 64<<10 {
			t.Fatalf("legal case is %d bytes, above the total bound", len(body))
		}
		out, e := totalRequest(body)
		if e != nil {
			t.Fatalf("legal request rejected: %v", e)
		}
		if !strings.Contains(out, "password=test-token-not-real") {
			t.Fatalf("legal request did not answer: %q", out)
		}
	}
}

// padTo builds a legal request body of exactly n bytes: the required scalars plus
// a few large, legal repeated array fields, so the total-size bound is exercised
// without tripping the per-line or per-field limits.
func padTo(t *testing.T, n int) string {
	t.Helper()
	head := "protocol=https\nhost=github.com\n"
	fields := 4
	prefix := "wwwauth[]="
	suffix := "\n"
	// Reserve room for the field syntax, then split the payload evenly.
	remainder := n - len(head) - fields*(len(prefix)+len(suffix))
	if remainder < fields {
		t.Fatalf("target %d too small", n)
	}
	body := head
	for i := 0; i < fields; i++ {
		size := remainder / (fields - i)
		remainder -= size
		body += prefix + strings.Repeat("y", size) + suffix
	}
	if len(body) != n {
		t.Fatalf("padded to %d, want %d", len(body), n)
	}
	return body
}

// The bound is a real total, not a per-line rule: exactly 64 KiB is still a
// legal request, and one byte more is refused before the token is ever read.
func TestCredentialTotalBoundary(t *testing.T) {
	const limit = 64 << 10
	reads := 0
	token := func() (io.Reader, error) {
		reads++
		return strings.NewReader("test-token-not-real\n"), nil
	}
	env := map[string]string{envHelperHost: "github.com", envHelperFD: "3"}
	for _, c := range []struct {
		name  string
		size  int
		allow bool
	}{
		{"exactly the bound", limit, true},
		{"one byte over", limit + 1, false},
	} {
		reads = 0
		out, _, e := credentialOutput(env, token, []string{helperSubcommand, "get"}, strings.NewReader(padTo(t, c.size)))
		if c.allow {
			if e != nil || !strings.Contains(out, "password=test-token-not-real") || reads != 1 {
				t.Fatalf("%s: rejected legal request: %q %v reads=%d", c.name, out, e, reads)
			}
			continue
		}
		if e == nil {
			t.Fatalf("%s: oversized request accepted", c.name)
		}
		if reads != 0 {
			t.Fatalf("%s: oversized request read the token %d times", c.name, reads)
		}
		if strings.Contains(out, "test-token-not-real") {
			t.Fatalf("%s: token leaked", c.name)
		}
	}
}
