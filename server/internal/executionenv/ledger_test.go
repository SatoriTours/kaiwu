package executionenv

import (
	"errors"
	"strings"
	"testing"
)

func runtimeFixture() Record {
	return Record{Identity: Identity{RunID: "0123456789abcdef0123456789abcdef", BootID: "01234567-89ab-cdef-0123-456789abcdef", Unit: "kaiwu-exec-0123456789abcdef0123456789abcdef.service"}, Owner: Owner{ProjectID: "project-a", ConversationID: "conversation-a"}, Kind: "pi", SpecHash: strings.Repeat("a", 64), Budget: Budget{CPUQuotaUSec: 25000, MemoryBytes: 96 << 20, PidsMax: 32, DiskBytes: 4096}, Phase: Starting}
}
func fullIdentity(r Record) Identity {
	i := r.Identity
	i.InvocationID = strings.Repeat("b", 32)
	i.ControlGroup = "/user.slice/user-1000.slice/user@1000.service/app.slice/" + i.Unit
	return i
}
func TestPIDReuseNeverAuthorizesForeignIdentity(t *testing.T) {
	r := runtimeFixture()
	saved := fullIdentity(r)
	for _, field := range []string{"run", "boot", "unit", "invocation", "cgroup"} {
		t.Run(field, func(t *testing.T) {
			other := saved
			switch field {
			case "run":
				other.RunID = strings.Repeat("c", 32)
			case "boot":
				other.BootID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
			case "unit":
				other.Unit = "foreign.service"
			case "invocation":
				other.InvocationID = strings.Repeat("d", 32)
			case "cgroup":
				other.ControlGroup = "/foreign.service"
			}
			o := Observation{Identity: other, State: OwnedRunning, GroupPopulated: true}
			if CanStop(saved, o) {
				t.Fatal("foreign identity authorized")
			}
		})
	}
	if !CanStop(saved, Observation{Identity: saved, State: OwnedRunning, GroupPopulated: true}) {
		t.Fatal("exact ownership rejected")
	}
	if CanStop(r.Identity, Observation{Identity: r.Identity, State: OwnedRunning, GroupPopulated: true}) {
		t.Fatal("partial identity authorized")
	}
}
func TestRuntimeCannotReleaseOnDisconnectOrAgentEnd(t *testing.T) {
	old := runtimeFixture()
	old.Identity = fullIdentity(old)
	old.Phase = Running
	old.LaunchSequence = 1
	old.RendezvousInode = 12
	old.Version = 4
	next := old
	next.Phase = Stopped
	for _, proof := range []*Observation{nil, {Identity: old.Identity, State: string(Unknown)}, {Identity: old.Identity, State: OwnedRunning, GroupPopulated: true}, {Identity: old.Identity, State: OwnedGone, GroupPopulated: true}} {
		if err := ValidateTransition(old, next, proof); err == nil {
			t.Fatal("released without group-gone proof")
		}
	}
	proof := Observation{Identity: old.Identity, State: OwnedGone}
	if err := ValidateTransition(old, next, &proof); err != nil {
		t.Fatal(err)
	}
}
func TestCommitIntentCannotBeResetOrIdentityRebound(t *testing.T) {
	old := runtimeFixture()
	old.Identity = fullIdentity(old)
	old.RendezvousInode = 12
	old.Phase = CommitIntent
	old.LaunchSequence = 1
	old.Version = 3
	for _, change := range []func(*Record){func(r *Record) { r.LaunchSequence = 0 }, func(r *Record) { r.SpecHash = strings.Repeat("c", 64) }, func(r *Record) { r.Owner.ProjectID = "project-b" }, func(r *Record) { r.Identity.InvocationID = strings.Repeat("d", 32) }, func(r *Record) { r.RendezvousInode = 13 }, func(r *Record) { r.Phase = Prepared }} {
		next := old
		next.Phase = Unknown
		change(&next)
		if err := ValidateTransition(old, next, nil); err == nil {
			t.Fatal("immutable launch claim changed")
		}
	}
	next := old
	next.Phase = Unknown
	if err := ValidateTransition(old, next, nil); err != nil {
		t.Fatal(err)
	}
}
func TestRuntimePhasesRequirePreparedIdentityAndCommit(t *testing.T) {
	old := runtimeFixture()
	next := old
	next.Phase = Prepared
	if err := ValidateTransition(old, next, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("partial preparation accepted: %v", err)
	}
	next.Identity = fullIdentity(old)
	next.RendezvousInode = 12
	if err := ValidateTransition(old, next, nil); err != nil {
		t.Fatal(err)
	}
	old = next
	next.Phase = CommitIntent
	next.LaunchSequence = 1
	if err := ValidateTransition(old, next, nil); err != nil {
		t.Fatal(err)
	}
	old = next
	next.Phase = Running
	proof := Observation{Identity: old.Identity, State: OwnedRunning, GroupPopulated: true}
	if err := ValidateTransition(old, next, &proof); err != nil {
		t.Fatal(err)
	}
}

func TestTaskPIAndTerminalCannotOptOutOfWriterClaim(t *testing.T) {
	for _, kind := range []string{"pi", "terminal"} {
		t.Run(kind, func(t *testing.T) {
			r := runtimeFixture()
			r.Owner.TaskID = "task-a"
			r.Owner.ExecutionID = "execution-a"
			r.Kind = kind
			if err := ValidateRecord(r); err == nil {
				t.Fatal("task execution opted out of single-writer reservation")
			}
			r.Writer = true
			if err := ValidateRecord(r); err != nil {
				t.Fatal(err)
			}
		})
	}
}
