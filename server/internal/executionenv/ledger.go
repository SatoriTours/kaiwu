package executionenv

import (
	"errors"
	"regexp"
	"strings"
)

type Phase string

const (
	Starting     Phase = "starting"
	Prepared     Phase = "prepared"
	CommitIntent Phase = "commit-intent"
	Running      Phase = "running"
	Stopping     Phase = "stopping"
	Stopped      Phase = "stopped"
	Unknown      Phase = "unknown"
)
const (
	OwnedRunning = "owned-running"
	OwnedGone    = "owned-gone"
)

var ErrConflict = errors.New("managed runtime state conflict")

type Identity struct{ RunID, BootID, Unit, InvocationID, ControlGroup string }
type Observation struct {
	State          string
	Identity       Identity
	ActualBudget   Budget
	GroupPopulated bool
}
type Record struct {
	Identity                          Identity
	Owner                             Owner
	Kind, SpecHash                    string
	Budget                            Budget
	Writer                            bool
	RendezvousDevice, RendezvousInode uint64
	Phase                             Phase
	LaunchSequence, Version           uint64
}

var bootIDPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var specHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (i Identity) Validate(full bool) error {
	if !runIDPattern.MatchString(i.RunID) || !bootIDPattern.MatchString(i.BootID) || i.Unit != "kaiwu-exec-"+i.RunID+".service" {
		return invalid("runtime identity")
	}
	if i.InvocationID == "" && i.ControlGroup == "" && !full {
		return nil
	}
	if !runIDPattern.MatchString(i.InvocationID) || !absolutePath(i.ControlGroup) || !strings.HasSuffix(i.ControlGroup, "/"+i.Unit) || len(i.ControlGroup) > 4096 {
		return invalid("runtime invocation identity")
	}
	return nil
}
func CanStop(saved Identity, observed Observation) bool {
	return saved.Validate(true) == nil && saved == observed.Identity && observed.State == OwnedRunning && observed.GroupPopulated
}
func ValidateRecord(r Record) error {
	if r.Identity.Validate(false) != nil || !identifier.MatchString(r.Owner.ProjectID) || !identifier.MatchString(r.Owner.ConversationID) || !specHashPattern.MatchString(r.SpecHash) || r.Budget.Validate() != nil || r.LaunchSequence > 1 {
		return invalid("runtime record")
	}
	if (r.Owner.TaskID == "") != (r.Owner.ExecutionID == "") {
		return invalid("runtime task owner")
	}
	if r.Owner.TaskID != "" && (!identifier.MatchString(r.Owner.TaskID) || !identifier.MatchString(r.Owner.ExecutionID)) {
		return invalid("runtime task owner")
	}
	if r.Owner.TaskID != "" && (r.Kind == "pi" || r.Kind == "terminal") && !r.Writer {
		return invalid("task writer claim required")
	}
	if r.Writer && r.Owner.TaskID == "" {
		return invalid("runtime writer")
	}
	switch r.Kind {
	case "pi":
	case "terminal", "preview":
		if r.Owner.TaskID == "" {
			return invalid("runtime task owner")
		}
	default:
		return invalid("runtime kind")
	}
	switch r.Phase {
	case Starting:
		if r.LaunchSequence != 0 {
			return invalid("starting launch sequence")
		}
	case Prepared:
		if r.LaunchSequence != 0 {
			return invalid("prepared launch sequence")
		}
		fallthrough
	case CommitIntent, Running:
		if r.Identity.Validate(true) != nil || r.RendezvousInode == 0 {
			return invalid("prepared identity required")
		}
		if r.Phase != Prepared && r.LaunchSequence != 1 {
			return invalid("committed sequence required")
		}
	case Stopping, Stopped, Unknown:
	default:
		return invalid("runtime phase")
	}
	if r.LaunchSequence == 1 && (r.Identity.Validate(true) != nil || r.RendezvousInode == 0) {
		return invalid("committed identity required")
	}
	return nil
}

// ValidateTransition never observes the OS itself. Proof is obtained from a
// trusted, fresh native inspector, never deserialized from a client command.
func ValidateTransition(old, next Record, proof *Observation) error {
	if err := ValidateRecord(old); err != nil {
		return err
	}
	if err := ValidateRecord(next); err != nil {
		return err
	}
	if old.Owner != next.Owner || old.SpecHash != next.SpecHash || old.Kind != next.Kind || old.Budget != next.Budget || old.Writer != next.Writer || old.Identity.RunID != next.Identity.RunID || old.Identity.BootID != next.Identity.BootID || old.Identity.Unit != next.Identity.Unit || old.Version != next.Version {
		return ErrConflict
	}
	if old.Identity.InvocationID != "" && old.Identity != next.Identity {
		return ErrConflict
	}
	if old.RendezvousInode != 0 && (old.RendezvousDevice != next.RendezvousDevice || old.RendezvousInode != next.RendezvousInode) {
		return ErrConflict
	}
	if next.LaunchSequence < old.LaunchSequence {
		return ErrConflict
	}
	allowed := false
	switch old.Phase {
	case Starting:
		allowed = next.Phase == Starting || next.Phase == Prepared || next.Phase == Stopping || next.Phase == Unknown || next.Phase == Stopped
	case Prepared:
		allowed = next.Phase == Prepared || next.Phase == CommitIntent || next.Phase == Stopping || next.Phase == Unknown || next.Phase == Stopped
	case CommitIntent:
		allowed = next.Phase == Running || next.Phase == Unknown || next.Phase == Stopping || next.Phase == Stopped
	case Running:
		allowed = next.Phase == Running || next.Phase == Stopping || next.Phase == Unknown || next.Phase == Stopped
	case Stopping:
		allowed = next.Phase == Stopping || next.Phase == Unknown || next.Phase == Stopped
	case Unknown:
		allowed = next.Phase == Unknown || next.Phase == Stopping || next.Phase == Stopped || (next.Phase == Prepared && old.LaunchSequence == 0) || (next.Phase == Running && old.LaunchSequence == 1)
	case Stopped:
		allowed = next.Phase == Stopped
	}
	if !allowed {
		return ErrConflict
	}
	if next.LaunchSequence > old.LaunchSequence && (old.Phase != Prepared || next.Phase != CommitIntent) {
		return ErrConflict
	}
	if next.Phase == Running && (proof == nil || !CanStop(next.Identity, *proof)) {
		return unavailable("fresh owned-running observation required")
	}
	if next.Phase == Stopped && (proof == nil || proof.State != OwnedGone || proof.GroupPopulated || proof.Identity != next.Identity) {
		return unavailable("fresh group-gone observation required")
	}
	return nil
}
