package executionenv

import (
	"errors"
	"strings"
	"testing"
)

func admissionFixture() (Spec, Capabilities) {
	s := Spec{Mode: ManagedSandbox, Owner: Owner{ProjectID: "project-a", ConversationID: "conversation-a"}, Kind: "pi", Program: "/tools/pi", Dir: "/work", Env: []string{"HOME=/home/session", "PATH=/tools"}, Budget: Budget{CPUQuotaUSec: 25000, MemoryBytes: 96 << 20, PidsMax: 32, DiskBytes: 4096}, Network: NetworkGrant{Mode: "deny"}, ProjectRoot: "/projects/a"}
	c := Capabilities{PrivateIPC: true, SocketFilter: true, Platform: "linux", UserManager: true, PrivateRoot: true, PrivateUsers: true, PrivatePID: true, PrivateNetwork: true, CPU: true, Memory: true, Pids: true, EvidenceID: "synthetic-observation", Disk: DiskObservation{ProjectID: "project-a", Root: "/projects/a", HardLimit: 4096, CoversAllWrites: true, EvidenceID: "synthetic-quota"}}
	return s, c
}

// A missing capability must never turn managed execution into a host launch.
func TestManagedNeverFallsBackToHost(t *testing.T) {
	s, _ := admissionFixture()
	if err := Check(s, Capabilities{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing observations accepted: %v", err)
	}
}
func TestLegacyKeepsOriginalPathAndSession(t *testing.T) {
	mode, err := ModeForProject("legacy")
	if err != nil || mode != LegacyHost {
		t.Fatalf("mode=%s err=%v", mode, err)
	}
	// Sandbox discovery must not become a dependency of the old host adapter.
	if err := Check(Spec{Mode: mode}, Capabilities{}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "unknown", "managed-sandbox"} {
		if _, err := ModeForProject(invalid); err == nil {
			t.Fatalf("unknown storage mode accepted: %q", invalid)
		}
	}
	mode, err = ModeForProject("managed")
	if err != nil || mode != ManagedSandbox {
		t.Fatalf("managed mode=%s err=%v", mode, err)
	}
}
func TestVerifiedOfflineManagedAdmission(t *testing.T) {
	s, c := admissionFixture()
	if err := Check(s, c); err != nil {
		t.Fatal(err)
	}
}
func TestMissingHardDiskOrNetworkBlocksBeforePrepare(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Spec, *Capabilities)
	}{
		{"soft-size-limit", func(s *Spec, c *Capabilities) { c.Disk.CoversAllWrites = false }},
		{"no-disk-proof", func(s *Spec, c *Capabilities) { c.Disk.EvidenceID = "" }},
		{"another-project-quota", func(s *Spec, c *Capabilities) { c.Disk.ProjectID = "project-b" }},
		{"another-root-quota", func(s *Spec, c *Capabilities) { c.Disk.Root = "/projects/b" }},
		{"looser-than-requested", func(s *Spec, c *Capabilities) { c.Disk.HardLimit = 8192 }},
		{"unknown-limit", func(s *Spec, c *Capabilities) { c.Disk.HardLimit = 0 }},
		{"unknown-network-mode", func(s *Spec, c *Capabilities) { s.Network.Mode = "host" }},
		{"unverified-broker", func(s *Spec, c *Capabilities) {
			s.Network = NetworkGrant{Mode: "broker", PolicyID: "provider-a", PolicyVersion: "v1"}
		}},
		{"wrong-policy", func(s *Spec, c *Capabilities) {
			s.Network = NetworkGrant{Mode: "broker", PolicyID: "provider-a", PolicyVersion: "v1"}
			c.Network = NetworkObservation{PolicyID: "provider-b", PolicyVersion: "v1", EvidenceID: "synthetic-network"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, c := admissionFixture()
			tc.mutate(&s, &c)
			if err := Check(s, c); err == nil {
				t.Fatal("unsafe admission accepted")
			}
		})
	}
}
func TestEveryIsolationCapabilityIsMandatory(t *testing.T) {
	for _, field := range []string{"ipc", "socket-filter", "platform", "manager", "root", "users", "pid", "net", "cpu", "memory", "pids", "evidence"} {
		t.Run(field, func(t *testing.T) {
			s, c := admissionFixture()
			switch field {
			case "ipc":
				c.PrivateIPC = false
			case "socket-filter":
				c.SocketFilter = false
			case "platform":
				c.Platform = "darwin"
			case "manager":
				c.UserManager = false
			case "root":
				c.PrivateRoot = false
			case "users":
				c.PrivateUsers = false
			case "pid":
				c.PrivatePID = false
			case "net":
				c.PrivateNetwork = false
			case "cpu":
				c.CPU = false
			case "memory":
				c.Memory = false
			case "pids":
				c.Pids = false
			case "evidence":
				c.EvidenceID = ""
			}
			if err := Check(s, c); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("accepted absent %s: %v", field, err)
			}
		})
	}
}
func TestBudgetAndEnvironmentCannotExpandAuthority(t *testing.T) {
	for _, entry := range []string{"OPENAI_API_KEY=do-not-reflect", "LD_PRELOAD=do-not-reflect", "NODE_OPTIONS=do-not-reflect", "BASH_ENV=do-not-reflect", "HTTP_PROXY=http://192.168.2.91:7331", "PATH=/tools\x00evil", "HOME=relative", "PATH=/tools\nEVIL=value", "HOME=/one\x00"} {
		t.Run(strings.SplitN(entry, "=", 2)[0], func(t *testing.T) {
			s, c := admissionFixture()
			s.Env = []string{entry}
			err := Check(s, c)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("unsafe env accepted: %v", err)
			}
			if strings.Contains(err.Error(), "do-not-reflect") {
				t.Fatal("error disclosed secret")
			}
		})
	}
	for _, budget := range []Budget{{}, {CPUQuotaUSec: 25000, MemoryBytes: 1, PidsMax: 1, DiskBytes: -1}, {CPUQuotaUSec: -1, MemoryBytes: 1, PidsMax: 1, DiskBytes: 1}} {
		s, c := admissionFixture()
		s.Budget = budget
		if err := Check(s, c); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid budget accepted: %+v %v", budget, err)
		}
	}
}
func TestSelectedBrokerPolicyMustMatchObservation(t *testing.T) {
	s, c := admissionFixture()
	s.Network = NetworkGrant{Mode: "broker", PolicyID: "provider-a", PolicyVersion: "v1"}
	c.Network = NetworkObservation{PolicyID: "provider-a", PolicyVersion: "v1", EvidenceID: "synthetic-network"}
	if err := Check(s, c); err != nil {
		t.Fatal(err)
	}
	c.Network.PolicyVersion = "old"
	if err := Check(s, c); err == nil {
		t.Fatal("stale policy accepted")
	}
}
