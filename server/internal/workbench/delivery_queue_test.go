package workbench

import (
	"path/filepath"
	"testing"
)

// queueFixture boots one enabled delivery project with two startable tasks
// and returns the store path so a restart can reopen the same database.
func queueFixture(t *testing.T) (*Store, string, [2]string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	s, e := Open(path, Profile{Provider: "fixture", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	command := func(c Command) Receipt {
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	project := command(Command{OperationID: "create-project", Type: "add_project", Name: "fixture", Path: t.TempDir()}).CreatedID
	tasks := [2]string{
		command(Command{OperationID: "create-task-1", Type: "add_task", ProjectID: project, TopicID: project + "_other", Title: "first", Request: "requirement"}).CreatedID,
		command(Command{OperationID: "create-task-2", Type: "add_task", ProjectID: project, TopicID: project + "_other", Title: "second", Request: "requirement"}).CreatedID,
	}
	enableDelivery(t, s, project)
	return s, project, tasks, path
}

func queueTask(t *testing.T, s *Store, project, title string) string {
	t.Helper()
	c := Command{OperationID: "create-task-" + title, Type: "add_task", ProjectID: project, TopicID: project + "_other", Title: title, Request: "requirement"}
	c.Revision = s.Snapshot().Revision
	r, e := s.Command(c)
	if e != nil {
		t.Fatal(e)
	}
	return r.CreatedID
}

func startQ(t *testing.T, s *Store, project, task, op string) string {
	t.Helper()
	c := Command{OperationID: op, Type: "start_task", ProjectID: project, TaskID: task}
	c.Revision = s.Snapshot().Revision
	r, e := s.Command(c)
	if e != nil {
		t.Fatal(e)
	}
	if r.DeliveryID == "" {
		t.Fatal("no delivery cycle")
	}
	return r.DeliveryID
}

// setQueueCommon pins the cycle's canonical common dir through the real CAS
// path the delivery machine uses before entering the integration queue.
func setQueueCommon(t *testing.T, s *Store, deliveryID, dir string) Delivery {
	t.Helper()
	d := deliveryQ(t, s, deliveryID)
	next := d
	next.CommonDir = dir
	out, e := s.AdvanceDelivery(d.ID, d.Version, next, nil)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func deliveryQ(t *testing.T, s *Store, id string) Delivery {
	t.Helper()
	d, e := s.Delivery(id)
	if e != nil {
		t.Fatal(e)
	}
	return d
}

// TestDeliveryRemoteLaneAcrossClones proves the persisted FIFO queue keys:
// one waiting ticket per common workspace clone lane and one per repo main
// lane, shared across project entries and clones, manual merge kept out of an
// active cycle's way and releases reserved for verified or explicitly
// cancelled (unverified-announced) cycles.
func TestDeliveryRemoteLaneAcrossClones(t *testing.T) {
	s, project, tasks, _ := queueFixture(t)
	d1 := setQueueCommon(t, s, startQ(t, s, project, tasks[0], "start-1"), filepath.Join(filepath.Dir(t.TempDir()), "clone-a"))
	d2 := setQueueCommon(t, s, startQ(t, s, project, tasks[1], "start-2"), d1.CommonDir)

	tick1, e := s.EnqueueDelivery(d1.ID)
	if e != nil {
		t.Fatal(e)
	}
	tick2, e := s.EnqueueDelivery(d2.ID)
	if e != nil {
		t.Fatal(e)
	}
	if tick2 <= tick1 {
		t.Fatalf("FIFO tickets not ordered: %d %d", tick1, tick2)
	}
	// Enqueueing twice must never mint a second ticket.
	again, e := s.EnqueueDelivery(d1.ID)
	if e != nil || again != tick1 {
		t.Fatalf("enqueue not idempotent: %d %v", again, e)
	}

	if ok, e := s.AcquireDeliveryTurn(d1.ID); e != nil || !ok {
		t.Fatalf("first cycle refused the turn: %v %v", ok, e)
	}
	if ok, e := s.AcquireDeliveryTurn(d2.ID); e != nil || ok {
		t.Fatalf("second cycle jumped the FIFO lane: %v %v", ok, e)
	}

	// Same repo, different clone: the remote lane keeps everyone behind the
	// verified integration even when the local common dirs differ.
	d3 := setQueueCommon(t, s, startQ(t, s, project, queueTask(t, s, project, "third"), "start-3"), filepath.Join(filepath.Dir(t.TempDir()), "clone-b"))
	if _, e = s.EnqueueDelivery(d3.ID); e != nil {
		t.Fatal(e)
	}
	if ok, e := s.AcquireDeliveryTurn(d3.ID); e != nil || ok {
		t.Fatalf("a different clone overtook the busy remote lane: %v %v", ok, e)
	}

	// The old manual merge path may not race a running delivery cycle.
	proof := MergePreview{ID: "preview-lane", ProjectID: project, TaskID: tasks[0], CanMerge: true}
	if _, _, e := s.BeginMerge(MergeRequest{OperationID: "merge-lane", ProjectID: project, TaskID: tasks[0], PreviewID: "preview-lane", CommitMessage: "manual"}, proof); e == nil {
		t.Fatal("manual merge bypassed the active delivery cycle")
	}

	// A cycle that has not passed its gates keeps the quarantine.
	if e := s.ReleaseDeliveryTurn(d1.ID, false); e == nil {
		t.Fatal("unverified release accepted")
	}
	if ok, e := s.AcquireDeliveryTurn(d2.ID); e != nil || ok {
		t.Fatalf("quarantine leaked after a rejected release: %v %v", ok, e)
	}

	// Explicit cancel admits it stopped unverified and frees the lane in the
	// same transaction, with a persisted warning.
	canceled := deliveryQ(t, s, d1.ID)
	if _, e := s.ControlDelivery("cancel-1", d1.ID, "cancel", "", canceled.Version); e != nil {
		t.Fatal(e)
	}
	if ok, e := s.AcquireDeliveryTurn(d2.ID); e != nil || !ok {
		t.Fatalf("cancel did not free the lane: %v %v", ok, e)
	}
	warned := false
	for _, step := range s.Snapshot().Deliveries[tasks[0]].Steps {
		if step.Kind == "release_unverified" {
			warned = true
		}
	}
	if !warned {
		t.Fatal("unverified cancel left no warning step")
	}

	// Even the verification gate must ask the quarantine first.
	awaited := awaitingDelivery(t, s, d2, "candidate-q", "tree-q", "round-q", "token-q")
	if e := s.ReleaseDeliveryTurn(d2.ID, false); e == nil {
		t.Fatal("pre-acceptance release accepted")
	}
	if _, e := s.AcceptDelivery("accept-q", awaited.ID, "token-q", awaited.Version); e != nil {
		t.Fatal(e)
	}
	// Acceptance admits the candidate to integration but is not yet verified:
	// the lane stays until the main gate passes.
	if e := s.ReleaseDeliveryTurn(d2.ID, false); e == nil {
		t.Fatal("integration-in-flight release accepted")
	}
	// Only the verified final state releases without a warning.
	final := deliveryQ(t, s, d2.ID)
	finalNext := final
	finalNext.Phase = "succeeded"
	if _, e := s.AdvanceDelivery(d2.ID, final.Version, finalNext, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.ReleaseDeliveryTurn(d2.ID, false); e != nil {
		t.Fatalf("verified release refused: %v", e)
	}
	if ok, e := s.AcquireDeliveryTurn(d3.ID); e != nil || !ok {
		t.Fatalf("lane not handed on after verification: %v %v", ok, e)
	}
}

// TestDeliveryQueuePersistsRestart proves ticket FIFO survives a process
// restart: waiting cycles keep their place and the lanes still refuse
// overtaking after reopen.
func TestDeliveryQueuePersistsRestart(t *testing.T) {
	s, project, tasks, path := queueFixture(t)
	d1 := setQueueCommon(t, s, startQ(t, s, project, tasks[0], "start-1"), filepath.Join(filepath.Dir(t.TempDir()), "clone-a"))
	d2 := setQueueCommon(t, s, startQ(t, s, project, tasks[1], "start-2"), d1.CommonDir)
	if _, e := s.EnqueueDelivery(d1.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.EnqueueDelivery(d2.ID); e != nil {
		t.Fatal(e)
	}
	if ok, e := s.AcquireDeliveryTurn(d1.ID); e != nil || !ok {
		t.Fatalf("first turn refused: %v %v", ok, e)
	}
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(path, Profile{Provider: "fixture", Model: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if ok, e := reopened.AcquireDeliveryTurn(d2.ID); e != nil || ok {
		t.Fatalf("restart let the queue be overtaken: %v %v", ok, e)
	}
	w := deliveryQ(t, reopened, d1.ID)
	if _, e := reopened.ControlDelivery("cancel-restart", d1.ID, "cancel", "", w.Version); e != nil {
		t.Fatal(e)
	}
	if ok, e := reopened.AcquireDeliveryTurn(d2.ID); e != nil || !ok {
		t.Fatalf("restart lost the FIFO hand-on: %v %v", ok, e)
	}
}

func TestDeliveryQueueValidation(t *testing.T) {
	s, project, tasks, _ := queueFixture(t)
	r := startQ(t, s, project, tasks[0], "start-1")
	d := deliveryQ(t, s, r)

	// The lane keys cannot be derived from an unknown or workspace-less cycle.
	if _, e := s.EnqueueDelivery("unknown-delivery"); e == nil {
		t.Fatal("unknown delivery queued")
	}
	if _, e := s.EnqueueDelivery(d.ID); e == nil {
		t.Fatal("workspace-less cycle queued")
	}
	set := setQueueCommon(t, s, d.ID, filepath.Join(filepath.Dir(t.TempDir()), "clone-a"))
	if _, e := s.EnqueueDelivery(set.ID); e != nil {
		t.Fatal(e)
	}
	if ok, e := s.AcquireDeliveryTurn(set.ID); e != nil || !ok {
		t.Fatalf("valid turn refused: %v %v", ok, e)
	}
	// Releasing a cycle that never queued is a defensive no-op.
	if e := s.ReleaseDeliveryTurn(d.ID, true); e != nil {
		t.Fatalf("release without a ticket failed: %v", e)
	}
}
