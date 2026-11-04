package execbridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

type gapJournal struct{ row executionenv.Record }

func (j *gapJournal) Runtime(context.Context, string) (executionenv.Record, error) { return j.row, nil }
func (j *gapJournal) AdvanceRuntime(_ context.Context, r executionenv.Record, p *executionenv.Observation) (executionenv.Record, error) {
	if err := executionenv.ValidateTransition(j.row, r, p); err != nil {
		return executionenv.Record{}, err
	}
	r.Version++
	j.row = r
	return r, nil
}

type gapEndpoint struct{ identity executionenv.Identity }

func (e gapEndpoint) Identity() executionenv.Identity      { return e.identity }
func (e gapEndpoint) Commit(context.Context, uint64) error { return nil }
func (e gapEndpoint) Status(context.Context) (LaunchStatus, error) {
	return LaunchStatus{Attempted: true, Executed: true, Gap: true}, nil
}
func (e gapEndpoint) Observe(context.Context) (executionenv.Observation, error) {
	return executionenv.Observation{State: executionenv.OwnedRunning, Identity: e.identity, GroupPopulated: true}, nil
}
func TestRecoverLaunchRejectsExecutedBridgeWithGap(t *testing.T) {
	_, _, id := authFixture(t)
	row := executionenv.Record{Identity: id, Owner: executionenv.Owner{ProjectID: "p", ConversationID: "c"}, Kind: "pi", SpecHash: strings.Repeat("a", 64), Budget: executionenv.Budget{CPUQuotaUSec: 10000, MemoryBytes: 1 << 20, PidsMax: 32, DiskBytes: 1 << 20}, RendezvousInode: 1, Phase: executionenv.CommitIntent, Version: 1, LaunchSequence: 1}
	j := &gapJournal{row}
	result, err := RecoverLaunch(context.Background(), j, id.RunID, gapEndpoint{id})
	if !errors.Is(err, executionenv.ErrUnavailable) || result.Phase != executionenv.Unknown {
		t.Fatalf("event gap became running/success: %+v %v", result, err)
	}
}
