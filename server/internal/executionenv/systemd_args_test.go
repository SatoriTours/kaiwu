package executionenv

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func unitFixture() UnitLaunch {
	s, _ := admissionFixture()
	return UnitLaunch{RunID: "0123456789abcdef0123456789abcdef", RootDirectory: "/tmp/隔离 root%n", Budget: s.Budget, Mounts: []MountGrant{{Source: "/projects/中文 空格%n", Target: "/work"}, {Source: "/tools/pi", Target: "/kaiwu/bridge", ReadOnly: true}, {Source: "/runtime/public", Target: "/run/kaiwu", ReadOnly: true}}}
}
func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
func TestUnitArgsAreNotShell(t *testing.T) {
	args, err := BuildUnitArgs(unitFixture())
	if err != nil {
		t.Fatal(err)
	}
	tail := []string{"--", "/kaiwu/bridge", "internal-exec-bridge", "--bootstrap", "/run/kaiwu/public.json"}
	if len(args) < len(tail) || !reflect.DeepEqual(args[len(args)-len(tail):], tail) {
		t.Fatalf("unexpected command: %q", args)
	}
	for _, want := range []string{"--user", "--no-block", "--service-type=exec", "--expand-environment=no", "--unit=kaiwu-exec-0123456789abcdef0123456789abcdef.service", "--property=KillMode=control-group", "--property=Restart=no", "--property=ProtectHome=tmpfs", "--property=PrivatePIDs=yes", "--property=PrivateNetwork=yes", "--property=NoNewPrivileges=yes", "--property=CPUQuota=25.000%", "--property=CPUQuotaPeriodSec=100ms", "--property=MemoryMax=100663296", "--property=MemorySwapMax=0", "--property=TasksMax=32"} {
		if !hasArg(args, want) {
			t.Fatalf("missing %s in %q", want, args)
		}
	}
	for _, arg := range args {
		if arg == "-c" || strings.Contains(arg, "Environment=") || strings.Contains(arg, "RuntimeMaxSec=20") || strings.Contains(arg, "ProtectHome=yes") {
			t.Fatalf("unsafe or probe-only argument: %q", arg)
		}
	}
}
func TestPercentSpaceUnicodePaths(t *testing.T) {
	args, err := BuildUnitArgs(unitFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--property=RootDirectory=/tmp/隔离 root%%n", "--property=BindPaths=\"/projects/中文 空格%%n:/work\"", "--property=ReadWritePaths=\"/work\"", "--property=BindReadOnlyPaths=\"/tools/pi:/kaiwu/bridge\" \"/runtime/public:/run/kaiwu\""} {
		if !hasArg(args, want) {
			t.Fatalf("missing literal %q in %q", want, args)
		}
	}
}
func TestUnitArgsRejectAmbiguousPathsAndIdentity(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*UnitLaunch)
	}{
		{"unit-injection", func(s *UnitLaunch) { s.RunID = "x.service --property=RootDirectory=/" }},
		{"relative-root", func(s *UnitLaunch) { s.RootDirectory = "relative" }},
		{"root-property-injection", func(s *UnitLaunch) { s.RootDirectory = "/tmp/x\nExecStart=/bin/sh" }},
		{"colon-bind", func(s *UnitLaunch) { s.Mounts[0].Source = "/project/a:b" }},
		{"backslash-bind", func(s *UnitLaunch) { s.Mounts[0].Source = "/project/a\\b" }},
		{"duplicate-target", func(s *UnitLaunch) { s.Mounts = append(s.Mounts, s.Mounts[0]) }},
		{"host-root", func(s *UnitLaunch) { s.Mounts[0].Source = "/" }},
		{"missing-bridge", func(s *UnitLaunch) { s.Mounts = s.Mounts[:1] }},
		{"writable-bridge", func(s *UnitLaunch) { s.Mounts[1].ReadOnly = false }},
		{"zero-budget", func(s *UnitLaunch) { s.Budget = Budget{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := unitFixture()
			tc.mutate(&s)
			if args, err := BuildUnitArgs(s); !errors.Is(err, ErrInvalid) || len(args) > 0 {
				t.Fatalf("invalid unit accepted: %q %v", args, err)
			}
		})
	}
}
func TestUnitBuilderDoesNotMutateMounts(t *testing.T) {
	s := unitFixture()
	before := append([]MountGrant(nil), s.Mounts...)
	if _, err := BuildUnitArgs(s); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Mounts, before) {
		t.Fatal("caller grants mutated")
	}
}

func TestUnitClosesSharedIPCAndNonInternetSockets(t *testing.T) {
	args, err := BuildUnitArgs(unitFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--property=PrivateIPC=yes", "--property=RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK"} {
		if !hasArg(args, want) {
			t.Fatalf("namespace bypass left open: missing %s", want)
		}
	}
}
