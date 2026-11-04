package executionenv

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type UnitLaunch struct {
	RunID, RootDirectory string
	Budget               Budget
	Mounts               []MountGrant
}

var runIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// BuildUnitArgs serializes a trusted, already-authorized root/mount plan. It
// never runs systemd and cannot attest support or mount identity. Actual Start
// must refresh observations and complete bridge attestation before task exec.
// The user command, environment, credentials and prompt never enter these argv.
func BuildUnitArgs(s UnitLaunch) ([]string, error) {
	if !runIDPattern.MatchString(s.RunID) || !absolutePath(s.RootDirectory) || s.RootDirectory == "/" {
		return nil, invalid("unit identity or root")
	}
	if err := s.Budget.Validate(); err != nil {
		return nil, err
	}
	var rw, ro, writable []string
	seen := map[string]bool{}
	bridge, bootstrap := false, false
	for _, m := range s.Mounts {
		if !absolutePath(m.Source) || !absolutePath(m.Target) || m.Source == "/" || m.Target == "/" || strings.ContainsAny(m.Source+m.Target, ":\\") || seen[m.Target] {
			return nil, invalid("unrepresentable or duplicate mount")
		}
		seen[m.Target] = true
		word := strconv.Quote(specifierLiteral(m.Source) + ":" + specifierLiteral(m.Target))
		if m.ReadOnly {
			ro = append(ro, word)
		} else {
			rw = append(rw, word)
			writable = append(writable, strconv.Quote(specifierLiteral(m.Target)))
		}
		if m.Target == "/kaiwu/bridge" {
			bridge = m.ReadOnly
		}
		if m.Target == "/run/kaiwu" {
			bootstrap = m.ReadOnly
		}
	}
	if !bridge || !bootstrap {
		return nil, invalid("read-only bridge and bootstrap mounts required")
	}
	args := []string{"--user", "--quiet", "--no-block", "--service-type=exec", "--expand-environment=no", "--unit=kaiwu-exec-" + s.RunID + ".service"}
	// A short-lived client is not the writer. Do not use --pipe or substitute
	// its PID for the unit InvocationID/cgroup. Failed units retain inspectable
	// metadata until the owning controller saves evidence and explicitly retires.
	props := []string{
		"RootDirectory=" + specifierLiteral(s.RootDirectory),
		"PrivateUsers=yes", "PrivateMounts=yes", "PrivatePIDs=yes", "PrivateNetwork=yes", "PrivateIPC=yes", "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK", "PrivateDevices=yes", "MountAPIVFS=yes",
		"ProtectHome=tmpfs", "ProtectSystem=strict", "ProtectControlGroups=yes", "ProtectKernelTunables=yes", "ProtectKernelModules=yes", "ProtectKernelLogs=yes", "ProtectProc=invisible",
		"NoNewPrivileges=yes", "CapabilityBoundingSet=", "AmbientCapabilities=", "RestrictSUIDSGID=yes", "LockPersonality=yes", "LimitCORE=0", "UMask=0077",
		"KillMode=control-group", "Restart=no", "TimeoutStartSec=30s", "TimeoutStopSec=2s", "StandardInput=null", "StandardOutput=null", "StandardError=null",
		fmt.Sprintf("CPUQuota=%d.%03d%%", s.Budget.CPUQuotaUSec/1000, s.Budget.CPUQuotaUSec%1000), "CPUQuotaPeriodSec=100ms",
		"MemoryMax=" + strconv.FormatInt(s.Budget.MemoryBytes, 10), "MemorySwapMax=0", "TasksMax=" + strconv.FormatInt(s.Budget.PidsMax, 10),
	}
	if len(rw) > 0 {
		props = append(props, "BindPaths="+strings.Join(rw, " "), "ReadWritePaths="+strings.Join(writable, " "))
	}
	props = append(props, "BindReadOnlyPaths="+strings.Join(ro, " "))
	for _, p := range props {
		args = append(args, "--property="+p)
	}
	return append(args, "--", "/kaiwu/bridge", "internal-exec-bridge", "--bootstrap", "/run/kaiwu/public.json"), nil
}
func specifierLiteral(s string) string { return strings.ReplaceAll(s, "%", "%%") }
