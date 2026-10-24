// Package pirpc owns a bounded pi RPC transport, not product task state.
package pirpc

import (
	"encoding/json"
	"log/slog"
	"time"
)

// LaunchSpec requires absolute Program/Dir and an explicit non-nil environment.
// No shell, PATH lookup or host-environment copy is performed.
type LaunchSpec struct {
	Program         string
	Args            []string
	Dir             string
	Env             []string
	OwnProcessGroup bool
}

// Options uses defaults for zero values, positive overrides, and rejects negatives.
type Options struct {
	MaxRecordBytes, MaxPending, MaxWriteBytes int
	MaxEvents, MaxEventBytes, StderrTailBytes int
	StartupTimeout, CallTimeout, WriteTimeout time.Duration
	ShutdownTimeout, DrainTimeout             time.Duration
	Logger                                    *slog.Logger
	// OnStarted is an optional lifecycle callback invoked once, immediately
	// after a real child process was spawned and before any handshake, so the
	// caller can durably attribute the process it actually started. An error
	// aborts the launch and stops the already spawned child. The default (nil)
	// keeps every existing call site unchanged.
	OnStarted func(pid int) error
}

// Params must not be modified concurrently with Call.
type Command struct {
	Type   string
	Params json.RawMessage
}
type Response struct {
	ID, Command string
	Success     bool
	Data        json.RawMessage
	ErrorText   string
	Raw         json.RawMessage
}
type Event struct {
	Type string
	Raw  json.RawMessage
}
type Delivery uint8

const (
	NotSent Delivery = iota
	Uncertain
	Acknowledged
)

// ExitResult describes transport cleanup, never task acceptance or success.
type ExitResult struct {
	PID                 int
	ExitCode            *int
	Forced, Reaped      bool
	Cause, CleanupError error
}
type Stats struct{ IgnoredResponses uint64 }
