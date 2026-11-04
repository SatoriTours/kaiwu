// Package executionenv defines the fail-closed boundary for managed execution.
// Observations are supplied by trusted OS/storage backends, never by project
// configuration or an HTTP client. An observation is not itself an OS probe.
package executionenv

import "errors"

type Mode string

const (
	LegacyHost     Mode = "legacy-host"
	ManagedSandbox Mode = "managed-sandbox"
)

var (
	ErrInvalid     = errors.New("invalid managed execution request")
	ErrUnavailable = errors.New("managed execution capability unavailable")
)

type Owner struct{ ProjectID, TaskID, ConversationID, ExecutionID string }
type Budget struct{ CPUQuotaUSec, MemoryBytes, PidsMax, DiskBytes int64 }
type MountGrant struct {
	Source, Target string
	ReadOnly       bool
	Device, Inode  uint64
}
type NetworkGrant struct{ Mode, PolicyID, PolicyVersion string }
type Spec struct {
	Mode                            Mode
	Owner                           Owner
	Kind, Program, Dir, ProjectRoot string
	Args, Env                       []string
	Budget                          Budget
	Mounts                          []MountGrant
	Network                         NetworkGrant
}

// DiskObservation describes a project-wide persistent hard quota, not fsize,
// bandwidth, usage estimates, or an administrator's requested limit.
type DiskObservation struct {
	ProjectID, Root, EvidenceID string
	HardLimit                   int64
	CoversAllWrites             bool
}
type NetworkObservation struct{ PolicyID, PolicyVersion, EvidenceID string }
type Capabilities struct {
	PrivateIPC, SocketFilter                                                              bool
	Platform, EvidenceID                                                                  string
	UserManager, PrivateRoot, PrivateUsers, PrivatePID, PrivateNetwork, CPU, Memory, Pids bool
	Disk                                                                                  DiskObservation
	Network                                                                               NetworkObservation
}
