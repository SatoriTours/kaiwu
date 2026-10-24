package workbench

import (
	"path/filepath"
	"testing"
)

// ackFixture is a task whose newest round finished and whose execution is
// awaiting review, but which the user never confirmed.
func ackFixture(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e := s.Update(func(st *State) error {
		st.Projects["p"] = Project{ID: "p"}
		st.Tasks["t"] = Task{ID: "t", ProjectID: "p", ConversationID: "c", Status: "needs_confirmation", ExecutionID: "x"}
		st.Executions["x"] = Execution{ID: "x", TaskID: "t", ConversationID: "c", Phase: "awaiting_review", FinishedAt: "2026-10-24T10:00:00Z"}
		st.Conversations["c"] = Conversation{ID: "c", ProjectID: "p", TaskID: "t", RunState: "idle", Rounds: []Round{{
			ID: "r1", Phase: "finished", StartedAt: "2026-10-24T09:00:00Z", FinishedAt: "2026-10-24T10:00:00Z",
			Report: &Report{Summary: "done", Checks: []ReportCheck{{Title: "test", Claim: "passed", ToolID: "tool-1"}}},
			Tools:  []ToolRecord{{ID: "tool-1", Name: "bash", Result: "ok"}},
		}}}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return s
}

func ackRequest(op, mode, round string) MergeRequest {
	return MergeRequest{OperationID: op, ProjectID: "p", TaskID: "t", PreviewID: "preview", CommitMessage: "feature", Mode: mode, RoundID: round}
}

func ackProof(mode, round string) MergePreview {
	return MergePreview{ID: "preview", ProjectID: "p", TaskID: "t", CanMerge: true, Mode: mode, RoundID: round}
}

// A local review binds the newest finished round and atomically records the
// acknowledgement together with the exclusive merge operation.
func TestMergeReviewAcknowledgesLocalRoundAtomically(t *testing.T) {
	s := ackFixture(t)
	req := ackRequest("merge-ack", "local", "r1")
	op, replay, e := s.BeginMerge(req, ackProof("local", "r1"))
	if e != nil || replay {
		t.Fatal(op, replay, e)
	}
	st := s.Snapshot()
	round := st.Conversations["c"].Rounds[len(st.Conversations["c"].Rounds)-1]
	if round.Phase != "confirmed" || st.Tasks["t"].Status != "completed" || st.Executions["x"].Phase != "confirmed" {
		t.Fatalf("not acknowledged: %#v %#v", round.Phase, st.Tasks["t"])
	}
	if op.Mode != "local" || op.RoundID != "r1" || op.Phase != "queued" {
		t.Fatalf("operation lost review binding: %#v", op)
	}
	// The acknowledgement must not rewrite AI report checks or tool evidence.
	if len(round.Report.Checks) != 1 || round.Report.Checks[0].ToolID != "tool-1" || len(round.Tools) != 1 || round.Tools[0].ID != "tool-1" {
		t.Fatalf("evidence rewritten: %#v", round)
	}
	// One receipt covers both the acknowledgement and the queued merge.
	if _, replay, e := s.BeginMerge(req, ackProof("local", "r1")); e != nil || !replay {
		t.Fatalf("replay lost: %v %v", replay, e)
	}
}

// A rejected or failed admission must never leave a half-recorded
// acknowledgement behind.
func TestMergeReviewDoesNotHalfAcknowledge(t *testing.T) {
	for _, c := range []struct {
		name  string
		req   MergeRequest
		proof MergePreview
	}{
		{"round mismatch", ackRequest("merge-bad-round", "local", "r2"), ackProof("local", "r2")},
		{"mode mismatch", ackRequest("merge-bad-mode", "local", "r1"), ackProof("push", "r1")},
		{"unsupported mode", ackRequest("merge-bad-mode2", "push", "r1"), ackProof("push", "r1")},
		{"missing round id", ackRequest("merge-bad-empty", "local", ""), ackProof("local", "")},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := ackFixture(t)
			if _, _, e := s.BeginMerge(c.req, c.proof); e == nil {
				t.Fatal("invalid review accepted")
			}
			st := s.Snapshot()
			if st.Conversations["c"].Rounds[0].Phase != "finished" || st.Tasks["t"].Status != "needs_confirmation" {
				t.Fatal("half acknowledged")
			}
			if len(st.Merges) != 0 {
				t.Fatal("merge queued after failed review", st.Merges)
			}
		})
	}
}

// Refresh/re-template failures in persist must not acknowledge either.
func TestMergeReviewPersistFailureLeavesNoAcknowledgement(t *testing.T) {
	s := ackFixture(t)
	before := s.Snapshot()
	// Make the persisted revision diverge from the in-memory one so the
	// compare-and-swap inside persist fails after the acknowledgement has been
	// staged on the clone, exactly like a competing writer.
	if _, e := s.db.Exec("UPDATE state SET revision = revision + 5 WHERE id=1"); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.BeginMerge(ackRequest("merge-persist-fail", "local", "r1"), ackProof("local", "r1")); e == nil {
		t.Fatal("persist failure accepted")
	}
	after := s.Snapshot()
	if after.Conversations["c"].Rounds[0].Phase != "finished" || after.Tasks["t"].Status != "needs_confirmation" || len(after.Merges) != 0 {
		t.Fatal("half acknowledged after persist failure")
	}
	if after.Revision != before.Revision {
		t.Fatalf("revision advanced: %d -> %d", before.Revision, after.Revision)
	}
}

// An already confirmed round is reused instead of being confirmed twice.
func TestMergeReviewReusesConfirmedRound(t *testing.T) {
	s := ackFixture(t)
	if e := s.Update(func(st *State) error {
		c := st.Conversations["c"]
		c.Rounds[0].Phase = "confirmed"
		st.Conversations["c"] = c
		task := st.Tasks["t"]
		task.Status = "completed"
		st.Tasks["t"] = task
		x := st.Executions["x"]
		x.Phase = "confirmed"
		st.Executions["x"] = x
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	rev := s.Snapshot().Revision
	if _, _, e := s.BeginMerge(ackRequest("merge-confirmed", "local", "r1"), ackProof("local", "r1")); e != nil {
		t.Fatal(e)
	}
	st := s.Snapshot()
	if st.Executions["x"].Phase != "confirmed" || st.Conversations["c"].Rounds[0].Phase != "confirmed" {
		t.Fatal("confirmed round modified")
	}
	// Exactly one persist for the queued merge: reusing an existing
	// acknowledgement must not add a second write.
	if st.Revision != rev+1 {
		t.Fatalf("unexpected extra write: %d -> %d", rev, st.Revision)
	}
}

// The same operation id must not be reused for a different mode or round.
func TestMergeReviewOperationIDIsBoundToModeAndRound(t *testing.T) {
	s := ackFixture(t)
	if _, _, e := s.BeginMerge(ackRequest("merge-bound", "local", "r1"), ackProof("local", "r1")); e != nil {
		t.Fatal(e)
	}
	for _, req := range []MergeRequest{ackRequest("merge-bound", "local", "r2"), ackRequest("merge-bound", "push", "r1")} {
		if _, _, e := s.BeginMerge(req, ackProof(req.Mode, req.RoundID)); e == nil {
			t.Fatal("operation id reused across review variants", req)
		}
	}
}

// Unfinished, failed or interaction-blocked work can never be acknowledged by
// starting a merge.
func TestMergeReviewRejectsUnconfirmableWork(t *testing.T) {
	cases := map[string]func(*State){
		"running round": func(st *State) {
			c := st.Conversations["c"]
			c.Rounds[0].Phase = "running"
			st.Conversations["c"] = c
		},
		"failed execution": func(st *State) {
			x := st.Executions["x"]
			x.Phase = "failed"
			st.Executions["x"] = x
		},
		"awaiting user interaction": func(st *State) {
			c := st.Conversations["c"]
			c.Interactions = []InteractionRequest{{ID: "i", ProjectID: "p", ConversationID: "c", TaskID: "t", Status: InteractionPending, Blocking: true, Source: "structured"}}
			st.Conversations["c"] = c
		},
		"no finished round": func(st *State) {
			c := st.Conversations["c"]
			c.Rounds = nil
			st.Conversations["c"] = c
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := ackFixture(t)
			if e := s.Update(func(st *State) error { mutate(st); return nil }); e != nil {
				t.Fatal(e)
			}
			if _, _, e := s.BeginMerge(ackRequest("merge-reject", "local", "r1"), ackProof("local", "r1")); e == nil {
				t.Fatal("unconfirmable work acknowledged")
			}
			st := s.Snapshot()
			if len(st.Merges) != 0 || st.Tasks["t"].Status == "completed" {
				t.Fatal("state changed by rejected review")
			}
		})
	}
}

// The legacy request keeps its exact fingerprint and never acknowledges a round.
func TestMergeLegacyRequestKeepsFingerprintAndDoesNotAcknowledge(t *testing.T) {
	s := ackFixture(t)
	legacy := MergeRequest{OperationID: "merge-test", ProjectID: "p", TaskID: "t", PreviewID: "preview", CommitMessage: "feature"}
	if got := mergeFingerprint(legacy); got != "7d3ea07e2b37b8ff58e614f262a20c6736efd66bc38c3f49c94f7063f7cfa497" {
		t.Fatalf("legacy fingerprint changed: %s", got)
	}
	if _, _, e := s.BeginMerge(legacy, MergePreview{ID: "preview", ProjectID: "p", TaskID: "t", CanMerge: true}); e != nil {
		t.Fatal(e)
	}
	st := s.Snapshot()
	if st.Conversations["c"].Rounds[0].Phase != "finished" || st.Tasks["t"].Status != "needs_confirmation" {
		t.Fatal("legacy request acknowledged silently")
	}
	if op := st.Merges["merge-test"]; op.Mode != "" || op.RoundID != "" {
		t.Fatalf("legacy operation gained review fields: %#v", op)
	}
}

// Push is a named but not-yet-executable mode in this slice.
func TestMergeReviewRejectsPushBeforeAnyRemoteWork(t *testing.T) {
	s := ackFixture(t)
	if _, _, e := s.BeginMerge(ackRequest("merge-push", "push", "r1"), ackProof("push", "r1")); e == nil {
		t.Fatal("push accepted")
	}
	if st := s.Snapshot(); len(st.Merges) != 0 || st.Tasks["t"].Status == "completed" {
		t.Fatal("push changed state")
	}
}

// legacyAckFixture has no Round at all but carries persisted completion
// evidence, which is exactly the legacy shape ConfirmResult still accepts.
func legacyAckFixture(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "db"), Profile{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e := s.Update(func(st *State) error {
		st.Projects["p"] = Project{ID: "p"}
		st.Tasks["t"] = Task{ID: "t", ProjectID: "p", ConversationID: "c", Status: "needs_confirmation", ExecutionID: "x"}
		st.Executions["x"] = Execution{ID: "x", TaskID: "t", ConversationID: "c", Phase: "awaiting_review", StartedAt: "2026-10-24T09:00:00Z", FinishedAt: "2026-10-24T10:00:00Z"}
		st.Conversations["c"] = Conversation{ID: "c", ProjectID: "p", TaskID: "t", RunState: "idle"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return s
}

const legacyToken = "legacy:x:2026-10-24T10:00:00Z"

// A legacy task (no Round slice) must reach local review with the same exact
// token ConfirmResult accepts, and be confirmed by the same atomic write.
func TestMergeReviewAcknowledgesLegacyToken(t *testing.T) {
	s := legacyAckFixture(t)
	rev := s.Snapshot().Revision
	op, replay, e := s.BeginMerge(ackRequest("merge-legacy", "local", legacyToken), ackProof("local", legacyToken))
	if e != nil || replay {
		t.Fatal(op, replay, e)
	}
	st := s.Snapshot()
	if st.Revision != rev+1 {
		t.Fatalf("legacy acknowledgement was not part of the merge write: %d -> %d", rev, st.Revision)
	}
	c := st.Conversations["c"]
	if len(c.Rounds) != 1 || c.Rounds[0].ID != legacyToken || c.Rounds[0].Phase != "confirmed" {
		t.Fatalf("legacy round not synthesized as confirmed: %#v", c.Rounds)
	}
	if st.Tasks["t"].Status != "completed" || st.Executions["x"].Phase != "confirmed" {
		t.Fatalf("legacy task not confirmed: %#v %#v", st.Tasks["t"], st.Executions["x"])
	}
	if op.RoundID != legacyToken || op.Mode != "local" || op.Phase != "queued" {
		t.Fatalf("operation lost legacy binding: %#v", op)
	}
}

// Only the exact legacy token may be acknowledged.
func TestMergeReviewRejectsWrongLegacyEvidence(t *testing.T) {
	for _, c := range []struct {
		name   string
		round  string
		mutate func(*State)
	}{
		{"wrong token", "legacy:x:2026-10-24T11:00:00Z", nil},
		{"missing finishedAt", legacyToken, func(st *State) {
			x := st.Executions["x"]
			x.FinishedAt = ""
			st.Executions["x"] = x
		}},
		{"interrupted execution", legacyToken, func(st *State) {
			x := st.Executions["x"]
			x.Phase = "interrupted"
			st.Executions["x"] = x
		}},
		{"execution identity mismatch", legacyToken, func(st *State) {
			x := st.Executions["x"]
			x.ConversationID = "other"
			st.Executions["x"] = x
		}},
		{"task not awaiting confirmation", legacyToken, func(st *State) {
			v := st.Tasks["t"]
			v.Status = "completed"
			st.Tasks["t"] = v
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := legacyAckFixture(t)
			if c.mutate != nil {
				if e := s.Update(func(st *State) error { c.mutate(st); return nil }); e != nil {
					t.Fatal(e)
				}
			}
			before := s.Snapshot()
			if _, _, e := s.BeginMerge(ackRequest("merge-legacy-bad", "local", c.round), ackProof("local", c.round)); e == nil {
				t.Fatal("invalid legacy review accepted")
			}
			st := s.Snapshot()
			if st.Revision != before.Revision || len(st.Merges) != 0 || st.Tasks["t"].Status != before.Tasks["t"].Status ||
				st.Executions["x"].Phase != before.Executions["x"].Phase || len(st.Conversations["c"].Rounds) != len(before.Conversations["c"].Rounds) {
				t.Fatal("invalid legacy review changed state")
			}
		})
	}
}

// A confirmed round is only reusable when the execution really agrees with it.
func TestMergeReviewConfirmedRoundRequiresConsistentExecution(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*State)
	}{
		{"execution not confirmed", func(st *State) {
			x := st.Executions["x"]
			x.Phase = "awaiting_review"
			st.Executions["x"] = x
		}},
		{"waiting user", func(st *State) {
			conv := st.Conversations["c"]
			conv.RunState = "waiting_user"
			st.Conversations["c"] = conv
		}},
		{"task not completed", func(st *State) {
			task := st.Tasks["t"]
			task.Status = "needs_confirmation"
			st.Tasks["t"] = task
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := ackFixture(t)
			if e := s.Update(func(st *State) error {
				conv := st.Conversations["c"]
				conv.Rounds[0].Phase = "confirmed"
				st.Conversations["c"] = conv
				task := st.Tasks["t"]
				task.Status = "completed"
				st.Tasks["t"] = task
				x := st.Executions["x"]
				x.Phase = "confirmed"
				st.Executions["x"] = x
				c.mutate(st)
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			if _, _, e := s.BeginMerge(ackRequest("merge-confirmed-bad", "local", "r1"), ackProof("local", "r1")); e == nil {
				t.Fatal("inconsistent confirmed round accepted")
			}
			if st := s.Snapshot(); len(st.Merges) != 0 {
				t.Fatal("merge queued")
			}
		})
	}
}

// A CLI that failed or was interrupted after the result was acknowledged must
// not lose the completed result: only the live/waiting states block review.
func TestMergeReviewKeepsAcknowledgedResultAcrossCLIFailure(t *testing.T) {
	for _, run := range []string{"failed", "interrupted"} {
		t.Run(run, func(t *testing.T) {
			s := ackFixture(t)
			if e := s.Update(func(st *State) error {
				c := st.Conversations["c"]
				c.Rounds[0].Phase = "confirmed"
				c.RunState = run
				st.Conversations["c"] = c
				v := st.Tasks["t"]
				v.Status = "completed"
				st.Tasks["t"] = v
				x := st.Executions["x"]
				x.Phase = "confirmed"
				st.Executions["x"] = x
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			if _, e := ReviewedTask(s.Snapshot(), ackRequest("merge-cli", "local", "r1")); e != nil {
				t.Fatal("acknowledged result lost after CLI failure:", e)
			}
			// The execution outcome itself is still authoritative.
			if e := s.Update(func(st *State) error {
				x := st.Executions["x"]
				x.Phase = "failed"
				st.Executions["x"] = x
				return nil
			}); e != nil {
				t.Fatal(e)
			}
			if _, e := ReviewedTask(s.Snapshot(), ackRequest("merge-cli", "local", "r1")); e == nil {
				t.Fatal("failed execution accepted")
			}
		})
	}
	if s := ackFixture(t); s != nil {
		if e := s.Update(func(st *State) error {
			c := st.Conversations["c"]
			c.Rounds[0].Phase = "confirmed"
			c.RunState = "waiting_user"
			st.Conversations["c"] = c
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		if _, e := ReviewedTask(s.Snapshot(), ackRequest("merge-cli", "local", "r1")); e == nil {
			t.Fatal("waiting_user accepted")
		}
	}
}
