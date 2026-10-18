package workbench

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func deliveryFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "state.db"), Profile{Provider: "fixture", Model: "fake"})
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
	task := command(Command{OperationID: "create-task", Type: "add_task", ProjectID: project, TopicID: project + "_other", Title: "feature", Request: "requirement"}).CreatedID
	return s, project, task
}
func enableDelivery(t *testing.T, s *Store, project string) {
	t.Helper()
	p := validDeliveryPolicy()
	p.ProjectID = project
	if _, e := s.SaveDeliveryPolicy("enable-delivery", 0, p); e != nil {
		t.Fatal(e)
	}
}
func startDelivery(t *testing.T, s *Store, project, task string) Receipt {
	t.Helper()
	r, e := s.Command(Command{OperationID: "start-delivery", Type: "start_task", ProjectID: project, TaskID: task})
	if e != nil {
		t.Fatal(e)
	}
	if r.DeliveryID == "" {
		t.Fatal("no delivery cycle")
	}
	return r
}

func TestDeliveryMigrationAtomicStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(State{
		Projects:      map[string]Project{"old-project": {ID: "old-project", Path: t.TempDir(), ConversationID: "old-conversation"}},
		Tasks:         map[string]Task{"old-task": {ID: "old-task", ProjectID: "old-project", ConversationID: "old-conversation", Status: "completed"}},
		Conversations: map[string]Conversation{"old-conversation": {ID: "old-conversation", ProjectID: "old-project", TaskID: "old-task", RunState: "idle", Messages: []Message{{ID: "old-message", Role: "user", Text: "保留历史", Delivery: "complete"}}, Rounds: []Round{{ID: "old-message", Phase: "confirmed"}}}},
		Merges:        map[string]MergeOperation{"old-merge": {ID: "old-merge", ProjectID: "old-project", TaskID: "old-task", Phase: "succeeded", TaskAfterSHA: "preserved-sha"}},
		Topics:        []Topic{},
	})
	_, e = db.Exec(`CREATE TABLE state(id INTEGER PRIMARY KEY, revision INTEGER NOT NULL, body BLOB NOT NULL); CREATE TABLE operations(id TEXT PRIMARY KEY,fingerprint TEXT,receipt BLOB); CREATE TABLE login_sessions(id_hash TEXT PRIMARY KEY,token_hash TEXT,expires INTEGER); PRAGMA user_version=1; INSERT INTO login_sessions VALUES('hash','token',9999999999);`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO state VALUES(1,0,?)", body); e != nil {
		t.Fatal(e)
	}
	_ = db.Close()
	migrated, e := Open(path, Profile{})
	if e != nil {
		t.Fatal(e)
	}
	var schema, logins int
	if e = migrated.db.QueryRow("PRAGMA user_version").Scan(&schema); e != nil {
		t.Fatal(e)
	}
	_ = migrated.db.QueryRow("SELECT count(*) FROM login_sessions").Scan(&logins)
	if schema != 2 || logins != 1 {
		t.Fatalf("migration lost schema/login: %d %d", schema, logins)
	}
	legacy := migrated.Snapshot()
	if legacy.Tasks["old-task"].Status != "completed" || legacy.Conversations["old-conversation"].Messages[0].Text != "保留历史" || legacy.Conversations["old-conversation"].Rounds[0].Phase != "confirmed" || legacy.Merges["old-merge"].TaskAfterSHA != "preserved-sha" {
		t.Fatal("migration changed legacy task/round/merge")
	}
	active, e := migrated.ActiveDeliveries()
	if e != nil || len(active) != 0 {
		t.Fatalf("migration started work: %+v %v", active, e)
	}
	_ = migrated.Close()

	s, project, task := deliveryFixture(t)
	enableDelivery(t, s, project)
	r := startDelivery(t, s, project, task)
	d, e := s.Delivery(r.DeliveryID)
	if e != nil {
		t.Fatal(e)
	}
	if d.TaskID != task || d.Policy.Version != 1 || d.Policy.MaxRepairs != 3 || d.IntentID != r.CreatedID || d.Phase != "queued" {
		t.Fatalf("bad snapshot: %+v", d)
	}
	r2, e := s.Command(Command{OperationID: "start-delivery", Type: "start_task", ProjectID: project, TaskID: task})
	if e != nil || !r2.Replay || r2.DeliveryID != r.DeliveryID {
		t.Fatalf("retry duplicated: %+v %v", r2, e)
	}
	if _, e = s.Command(Command{OperationID: "start-delivery", Type: "start_task", ProjectID: project, TaskID: task, Text: "different"}); e == nil {
		t.Fatal("conflicting retry accepted")
	}
	p, e := s.DeliveryPolicy(project)
	if e != nil {
		t.Fatal(e)
	}
	p.MaxRepairs = 4
	if _, e = s.SaveDeliveryPolicy("update-policy", p.Version, p); e != nil {
		t.Fatal(e)
	}
	d, e = s.Delivery(d.ID)
	if e != nil || d.Policy.MaxRepairs != 3 {
		t.Fatal("template changed cycle snapshot")
	}
}

func TestDeliveryTransactionRollback(t *testing.T) {
	s, project, task := deliveryFixture(t)
	enableDelivery(t, s, project)
	before := s.Snapshot()
	_, e := s.db.Exec(`CREATE TRIGGER reject_delivery BEFORE INSERT ON deliveries BEGIN SELECT RAISE(ABORT,'fixture transaction failure'); END`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Command(Command{OperationID: "failed-start", Type: "start_task", ProjectID: project, TaskID: task})
	if e == nil {
		t.Fatal("start unexpectedly persisted")
	}
	after := s.Snapshot()
	if after.Revision != before.Revision || after.Tasks[task].ExecutionID != "" || len(after.Conversations[after.Tasks[task].ConversationID].Messages) != 0 {
		t.Fatal("partial start leaked")
	}
	if _, e = s.Operation("failed-start"); e == nil {
		t.Fatal("failed operation receipt exists")
	}
	var count int
	_ = s.db.QueryRow("SELECT count(*) FROM deliveries").Scan(&count)
	if count != 0 {
		t.Fatal("partial delivery exists")
	}
}

func TestDeliveryTransactionStepReplay(t *testing.T) {
	s, project, task := deliveryFixture(t)
	enableDelivery(t, s, project)
	r := startDelivery(t, s, project, task)
	d, _ := s.Delivery(r.DeliveryID)
	step := DeliveryStep{ID: "observed-step", DeliveryID: d.ID, Kind: "check", InputFingerprint: "fixed", Phase: "observed", ExitKnown: true}
	var e error
	d, e = s.AdvanceDelivery(d.ID, d.Version, d, &step)
	if e != nil {
		t.Fatal(e)
	}
	// An identical observed step can be reconciled without adding another row.
	d, e = s.AdvanceDelivery(d.ID, d.Version, d, &step)
	if e != nil {
		t.Fatal("identical receipt rejected:", e)
	}
	page, e := s.DeliverySteps(d.ID, 0, 100)
	if e != nil || len(page.Items) != 1 {
		t.Fatalf("replay: %+v %v", page, e)
	}
	before := s.Snapshot().Revision
	if _, e = s.db.Exec(`CREATE TRIGGER reject_step_state BEFORE UPDATE ON state BEGIN SELECT RAISE(ABORT,'fixture rollback'); END`); e != nil {
		t.Fatal(e)
	}
	step.ID = "failed-step"
	if _, e = s.AdvanceDelivery(d.ID, d.Version, d, &step); e == nil {
		t.Fatal("failed transaction committed")
	}
	stored, _ := s.Delivery(d.ID)
	page, _ = s.DeliverySteps(d.ID, 0, 100)
	if stored.Version != d.Version || s.Snapshot().Revision != before || len(page.Items) != 1 {
		t.Fatal("SQL/state partial transaction")
	}
}

func TestDeliveryProjectionBoundedHistory(t *testing.T) {
	s, project, task := deliveryFixture(t)
	enableDelivery(t, s, project)
	r := startDelivery(t, s, project, task)
	d, _ := s.Delivery(r.DeliveryID)
	for n := 1; n <= 25; n++ {
		step := DeliveryStep{ID: fmt.Sprintf("step-%02d", n), DeliveryID: d.ID, Kind: "check", InputFingerprint: "fixed", Phase: "observed", Summary: "bounded"}
		next := d
		var e error
		d, e = s.AdvanceDelivery(d.ID, d.Version, next, &step)
		if e != nil {
			t.Fatal(e)
		}
	}
	summary := s.Snapshot().Deliveries[task]
	if len(summary.Steps) != 20 || summary.Steps[0].ID != "step-06" || summary.Steps[19].ID != "step-25" {
		t.Fatalf("unbounded or wrong summary: %+v", summary.Steps)
	}
	page, e := s.DeliverySteps(d.ID, 0, 10)
	if e != nil || len(page.Items) != 10 || page.Next != 10 {
		t.Fatalf("page %+v %v", page, e)
	}
	second, e := s.DeliverySteps(d.ID, page.Next, 100)
	if e != nil || len(second.Items) != 15 || second.Items[0].ID != "step-11" || second.Next != 0 {
		t.Fatalf("continuation %+v %v", second, e)
	}
	stale := d
	stale.Phase = "succeeded"
	if _, e = s.AdvanceDelivery(d.ID, d.Version-1, stale, nil); e == nil {
		t.Fatal("stale transition accepted")
	}
	step := DeliveryStep{ID: "step-01", DeliveryID: d.ID, Kind: "check", InputFingerprint: "different", Phase: "observed"}
	if _, e = s.AdvanceDelivery(d.ID, d.Version, d, &step); e == nil {
		t.Fatal("step id reused with new input")
	}
	if s.Snapshot().Tasks[task].Status == "completed" {
		t.Fatal("technical state forged human approval")
	}
}
