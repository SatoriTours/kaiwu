package execbridge

import (
	"context"
	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

type LaunchStatus struct{ Attempted, Executed, Gap bool }
type Journal interface {
	Runtime(context.Context, string) (executionenv.Record, error)
	AdvanceRuntime(context.Context, executionenv.Record, *executionenv.Observation) (executionenv.Record, error)
}
type Endpoint interface {
	Identity() executionenv.Identity
	Commit(context.Context, uint64) error
	Status(context.Context) (LaunchStatus, error)
	Observe(context.Context) (executionenv.Observation, error)
}

func uncertain(ctx context.Context, j Journal, r executionenv.Record) (executionenv.Record, error) {
	next := r
	next.Phase = executionenv.Unknown
	saved, err := j.AdvanceRuntime(ctx, next, nil)
	if err != nil {
		return r, executionenv.ErrUnavailable
	}
	return saved, executionenv.ErrUnavailable
}

// CommitPrepared persists the irreversible intent before the one wire send.
// Neither an ambiguous response nor a repeated call sends EXEC_COMMIT again.
func CommitPrepared(ctx context.Context, j Journal, key string, e Endpoint) (executionenv.Record, error) {
	r, err := j.Runtime(ctx, key)
	if err != nil {
		return r, err
	}
	if e == nil || r.Phase != executionenv.Prepared || r.LaunchSequence != 0 || e.Identity() != r.Identity {
		return r, executionenv.ErrConflict
	}
	proof, err := e.Observe(ctx)
	if err != nil || !executionenv.CanStop(r.Identity, proof) {
		return uncertain(ctx, j, r)
	}
	r.Phase = executionenv.CommitIntent
	r.LaunchSequence = 1
	r, err = j.AdvanceRuntime(ctx, r, nil)
	if err != nil {
		return r, err
	}
	if err = e.Commit(ctx, 1); err != nil {
		return uncertain(ctx, j, r)
	}
	return RecoverLaunch(ctx, j, key, e)
}

// RecoverLaunch is query-only at the endpoint. Even a prepared bridge observed
// after a durable intent is ambiguous: recovery never re-sends execution or a
// business prompt. New user intent must first resolve/stop that exact old run.
func RecoverLaunch(ctx context.Context, j Journal, key string, e Endpoint) (executionenv.Record, error) {
	r, err := j.Runtime(ctx, key)
	if err != nil {
		return r, err
	}
	if r.Phase == executionenv.Stopped {
		return r, executionenv.ErrConflict
	}
	if e == nil || e.Identity() != r.Identity {
		return uncertain(ctx, j, r)
	}
	status, err := e.Status(ctx)
	if err != nil || status.Gap || !status.Attempted || !status.Executed || r.LaunchSequence != 1 {
		return uncertain(ctx, j, r)
	}
	proof, err := e.Observe(ctx)
	if err != nil || !executionenv.CanStop(r.Identity, proof) {
		return uncertain(ctx, j, r)
	}
	next := r
	next.Phase = executionenv.Running
	return j.AdvanceRuntime(ctx, next, &proof)
}
