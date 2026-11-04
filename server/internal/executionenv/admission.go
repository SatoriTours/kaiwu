package executionenv

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ModeForProject(storageMode string) (Mode, error) {
	switch storageMode {
	case "legacy":
		return LegacyHost, nil
	case "managed":
		return ManagedSandbox, nil
	default:
		return "", invalid("project storage mode")
	}
}
func invalid(reason string) error     { return fmt.Errorf("%w: %s", ErrInvalid, reason) }
func unavailable(reason string) error { return fmt.Errorf("%w: %s", ErrUnavailable, reason) }

// Check performs no preparation, filesystem writes, process starts, or fallback.
// Observations must be refreshed by a trusted backend before actual execution;
// successful Check alone does not attest a namespace or permit a host spawn.
func Check(s Spec, c Capabilities) error {
	if s.Mode == LegacyHost {
		return nil
	}
	if err := validateSpec(s); err != nil {
		return err
	}
	if c.Platform != "linux" || c.EvidenceID == "" || !c.UserManager || !c.PrivateRoot || !c.PrivateUsers || !c.PrivatePID || !c.PrivateNetwork || !c.PrivateIPC || !c.SocketFilter || !c.CPU || !c.Memory || !c.Pids {
		return unavailable("isolation and cgroup observations required")
	}
	d := c.Disk
	if d.EvidenceID == "" || !d.CoversAllWrites || d.ProjectID != s.Owner.ProjectID || d.Root != s.ProjectRoot || d.HardLimit <= 0 || d.HardLimit > s.Budget.DiskBytes {
		return unavailable("project-wide persistent hard quota required")
	}
	switch s.Network.Mode {
	case "deny":
	case "broker":
		n := c.Network
		if n.EvidenceID == "" || n.PolicyID != s.Network.PolicyID || n.PolicyVersion != s.Network.PolicyVersion {
			return unavailable("matching verified network policy required")
		}
	default:
		return invalid("network mode")
	}
	return nil
}
func validateSpec(s Spec) error {
	if s.Mode != ManagedSandbox {
		return invalid("execution mode")
	}
	if !identifier.MatchString(s.Owner.ProjectID) || !identifier.MatchString(s.Owner.ConversationID) {
		return invalid("owner identity")
	}
	if (s.Owner.TaskID == "") != (s.Owner.ExecutionID == "") {
		return invalid("task execution identity")
	}
	if s.Owner.TaskID != "" && (!identifier.MatchString(s.Owner.TaskID) || !identifier.MatchString(s.Owner.ExecutionID)) {
		return invalid("task execution identity")
	}
	switch s.Kind {
	case "pi":
	case "terminal", "preview":
		if s.Owner.TaskID == "" {
			return invalid("task owner required")
		}
	default:
		return invalid("execution kind")
	}
	if !absolutePath(s.Program) || !absolutePath(s.Dir) || !absolutePath(s.ProjectRoot) {
		return invalid("absolute canonical paths required")
	}
	if s.Env == nil {
		return invalid("explicit environment required")
	}
	for _, arg := range s.Args {
		if strings.ContainsRune(arg, 0) {
			return invalid("argument encoding")
		}
	}
	if err := s.Budget.Validate(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range s.Env {
		key, value, ok := strings.Cut(e, "=")
		if !ok || seen[key] || !safeText(value) || !environmentValue(key, value, s.Network.Mode) {
			return invalid("environment whitelist")
		}
		seen[key] = true
	}
	targets := map[string]bool{}
	for _, m := range s.Mounts {
		if !absolutePath(m.Source) || !absolutePath(m.Target) || m.Source == "/" || m.Target == "/" || targets[m.Target] {
			return invalid("mount grant")
		}
		targets[m.Target] = true
	}
	switch s.Network.Mode {
	case "deny":
		if s.Network.PolicyID != "" || s.Network.PolicyVersion != "" {
			return invalid("deny policy carries broker authority")
		}
	case "broker":
		if !identifier.MatchString(s.Network.PolicyID) || !identifier.MatchString(s.Network.PolicyVersion) {
			return invalid("network policy identity")
		}
	default:
		return invalid("network mode")
	}
	return nil
}
func (b Budget) Validate() error {
	// Linux CFS quota has a 1ms minimum; a smaller request would be silently
	// rounded upwards and therefore would not enforce the requested ceiling.
	if b.CPUQuotaUSec < 1000 || b.CPUQuotaUSec > int64(^uint64(0)>>1)/10 || b.MemoryBytes <= 0 || b.PidsMax <= 0 || b.DiskBytes <= 0 {
		return invalid("positive enforceable resource budgets required")
	}
	return nil
}
func safeText(s string) bool     { return !strings.ContainsFunc(s, unicode.IsControl) }
func absolutePath(s string) bool { return path.IsAbs(s) && path.Clean(s) == s && safeText(s) }
func environmentValue(key, value, network string) bool {
	switch key {
	case "HOME", "PI_CODING_AGENT_DIR", "TMPDIR", "TMP", "TEMP", "GOCACHE", "GOPATH", "GOMODCACHE", "npm_config_cache", "NODE_EXTRA_CA_CERTS", "GIT_OBJECT_DIRECTORY", "GIT_INDEX_FILE":
		return absolutePath(value)
	case "PATH", "GIT_ALTERNATE_OBJECT_DIRECTORIES":
		for _, p := range strings.Split(value, ":") {
			if !absolutePath(p) {
				return false
			}
		}
		return true
	case "LANG", "LC_ALL", "TERM":
		return value != ""
	case "HISTFILE":
		return value == "/dev/null"
	case "NODE_USE_ENV_PROXY":
		return network == "broker" && value == "1"
	case "HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy":
		if network != "broker" {
			return false
		}
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return false
		}
		if u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
			return false
		}
		port, err := strconv.Atoi(u.Port())
		return err == nil && port > 0 && port <= 65535
	case "NO_PROXY", "no_proxy":
		return value == ""
	default:
		return false
	}
}
