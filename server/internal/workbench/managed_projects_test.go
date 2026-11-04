package workbench

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/projectstorage"
)

func TestManagedCreationPersistsIntentAcrossDatabaseFailureAndRestart(t *testing.T) {
	db := filepath.Join(t.TempDir(), "state.db")
	root := t.TempDir()
	s, err := Open(db, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	c := Command{OperationID: "managed-create", Type: "create_project", Name: "中文项目", Revision: s.Snapshot().Revision}
	var prepared []Project
	prepare := func(p Project) error {
		prepared = append(prepared, p)
		_, err := projectstorage.Create(context.Background(), root, p.ID, p.DirectoryName)
		return err
	}
	s.SetPersistBarrier(func(State) error { return errors.New("injected database failure") })
	if _, err = s.CreateManagedProject(c, root, prepare); err == nil {
		t.Fatal("failure not surfaced")
	}
	if len(s.Snapshot().Projects) != 0 || len(prepared) != 1 {
		t.Fatal("partial project visible")
	}
	pending, err := s.PendingProjectCreations()
	if err != nil || len(pending) != 1 || pending[0].OperationID != c.OperationID || pending[0].ProjectID != prepared[0].ID {
		t.Fatalf("missing recovery record: %+v %v", pending, err)
	}
	s.Close()
	s, err = Open(db, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.CreateManagedProject(c, root, prepare)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Snapshot().Projects[r.CreatedID]
	if len(prepared) != 2 || prepared[0].ID != p.ID || prepared[0].DirectoryName != p.DirectoryName || prepared[0].ConversationID != p.ConversationID {
		t.Fatal("retry generated new identities")
	}
	if p.StorageMode != "managed" || p.StorageRoot != filepath.Join(root, p.DirectoryName) || p.Path != filepath.Join(p.StorageRoot, "repo") || p.LayoutVersion != 1 {
		t.Fatalf("wrong layout: %+v", p)
	}
	replay, err := s.CreateManagedProject(c, root, prepare)
	if err != nil || !replay.Replay || replay.CreatedID != r.CreatedID || len(prepared) != 2 {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	pending, err = s.PendingProjectCreations()
	if err != nil || len(pending) != 0 {
		t.Fatalf("completed project remains pending: %+v %v", pending, err)
	}
	c.Name = "changed"
	if _, err := s.CreateManagedProject(c, root, prepare); err == nil {
		t.Fatal("operation id reused for different payload")
	}
}

func TestManagedCreationSameNameConcurrentAndSameOperationDeduplication(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"), Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root := t.TempDir()
	revision := s.Snapshot().Revision
	var wg sync.WaitGroup
	results := make(chan Receipt, 8)
	errs := make(chan error, 8)
	prepare := func(p Project) error {
		_, err := projectstorage.Create(context.Background(), root, p.ID, p.DirectoryName)
		return err
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.CreateManagedProject(Command{Type: "create_project", OperationID: "same-request", Name: "同名", Revision: revision}, root, prepare)
			results <- r
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id string
	for r := range results {
		if id != "" && id != r.CreatedID {
			t.Fatal("duplicate project")
		}
		id = r.CreatedID
	}
	c := Command{Type: "create_project", OperationID: "different-request", Name: "同名", Revision: s.Snapshot().Revision}
	r, err := s.CreateManagedProject(c, root, prepare)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Snapshot()
	if len(st.Projects) != 2 || st.Projects[id].Path == st.Projects[r.CreatedID].Path {
		t.Fatal("same names overwritten")
	}
}

func TestSchemaThreeUpgradeKeepsLegacySessionMessagesAndLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := map[string]any{
		"projects":      map[string]any{"old": map[string]any{"id": "old", "name": "旧目录", "path": "/original/repo", "conversationId": "conversation"}},
		"conversations": map[string]any{"conversation": map[string]any{"id": "conversation", "projectId": "old", "sessionId": "original-session", "sessionFile": "/original/session.jsonl", "runState": "interrupted", "messages": []map[string]any{{"id": "message", "role": "assistant", "text": "保留原结果", "delivery": "sent"}}}},
		"tasks":         map[string]any{}, "topics": []any{}, "revision": 7,
	}
	body, _ := json.Marshal(old)
	_, err = db.Exec("CREATE TABLE state(id INTEGER PRIMARY KEY,revision INTEGER NOT NULL,body BLOB);CREATE TABLE operations(id TEXT PRIMARY KEY,fingerprint TEXT,receipt BLOB);CREATE TABLE login_sessions(id_hash TEXT PRIMARY KEY,token_hash TEXT,expires INTEGER);PRAGMA user_version=3;INSERT INTO login_sessions VALUES('synthetic-hash','synthetic-token-hash',9999999999);")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO state VALUES(1,7,?)", body); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path, Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st := s.Snapshot()
	p := st.Projects["old"]
	c := st.Conversations["conversation"]
	if p.StorageMode != "legacy" || p.Path != "/original/repo" || p.StorageRoot != "" || p.ConversationID != "conversation" ||
		c.SessionID != "original-session" || c.SessionFile != "/original/session.jsonl" || len(c.Messages) != 1 || c.Messages[0].Text != "保留原结果" {
		t.Fatal("upgrade moved or lost legacy data")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM login_sessions WHERE id_hash='synthetic-hash'").Scan(&count); err != nil || count != 1 {
		t.Fatal("login lost", err)
	}
}

func TestRenameOnlyChangesDisplayNameIncludingRunningLegacyProject(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"), Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Command(Command{OperationID: "legacy-project", Type: "add_project", Name: "old", Path: t.TempDir(), Revision: s.Snapshot().Revision})
	if err != nil {
		t.Fatal(err)
	}
	p := s.Snapshot().Projects[r.CreatedID]
	if p.StorageMode != "legacy" {
		t.Fatal("legacy project not classified")
	}
	if err := s.Update(func(st *State) error {
		c := st.Conversations[p.ConversationID]
		c.RunState = "running"
		c.SessionFile = "/original/session.jsonl"
		st.Conversations[c.ID] = c
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	_, err = s.Command(Command{OperationID: "rename-project", Type: "rename_project", ProjectID: p.ID, Name: "新显示名称", Revision: before.Revision})
	if err != nil {
		t.Fatal(err)
	}
	after := s.Snapshot()
	want := before.Projects[p.ID]
	want.Name = "新显示名称"
	if after.Projects[p.ID] != want || !reflect.DeepEqual(after.Conversations, before.Conversations) || !reflect.DeepEqual(after.Tasks, before.Tasks) {
		t.Fatal("rename changed project identity, path or conversations")
	}
}

func TestPendingCreationCannotBeStolenByDeliveryOrMerge(t *testing.T) {
	for _, kind := range []string{"delivery", "merge", "raw-receipt"} {
		t.Run(kind, func(t *testing.T) {
			s, project, task := deliveryFixture(t)
			root := t.TempDir()
			cmd := Command{OperationID: "pending-shared-id", Type: "create_project", Name: "pending", Revision: s.Snapshot().Revision}
			if _, err := s.CreateManagedProject(cmd, root, func(Project) error { return errors.New("disk failure") }); err == nil {
				t.Fatal("failure missing")
			}
			switch kind {
			case "delivery":
				_, err := s.SaveDeliveryPolicy(cmd.OperationID, 0, DefaultDeliveryPolicy(project))
				if err == nil {
					t.Fatal("delivery stole pending creation id")
				}
			case "merge":
				req := MergeRequest{OperationID: cmd.OperationID, ProjectID: project, TaskID: task, PreviewID: "proof", CommitMessage: "feature"}
				proof := MergePreview{ID: "proof", ProjectID: project, TaskID: task, CanMerge: true}
				if _, _, err := s.BeginMerge(req, proof); err == nil {
					t.Fatal("merge stole pending creation id")
				}
			case "raw-receipt":
				if _, err := s.db.Exec("INSERT INTO operations VALUES(?,?,?)", cmd.OperationID, "different", []byte("{}")); err == nil {
					t.Fatal("database accepted foreign receipt for reserved id")
				}
			}
			_, err := s.CreateManagedProject(cmd, root, func(p Project) error {
				_, err := projectstorage.Create(context.Background(), root, p.ID, p.DirectoryName)
				return err
			})
			if err != nil {
				t.Fatalf("original creation cannot resume: %v", err)
			}
		})
	}
}

func TestManagedCreationRejectsClientPathsAndReusedPendingOperation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"), Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root := t.TempDir()
	calls := 0
	prep := func(Project) error { calls++; return errors.New("failed") }
	c := Command{Type: "create_project", OperationID: "pending-request", Name: "safe", Path: "/client-controlled", Revision: s.Snapshot().Revision}
	if _, err := s.CreateManagedProject(c, root, prep); err == nil || calls != 0 {
		t.Fatal("client path accepted")
	}
	c.Path = ""
	if _, err := s.CreateManagedProject(c, root, prep); err == nil || calls != 1 {
		t.Fatal("failed preparation not surfaced")
	}
	c.Type = "add_project"
	c.Path = t.TempDir()
	if _, err := s.Command(c); err == nil {
		t.Fatal("pending operation id reused through legacy command")
	}
	c.Type = "create_project"
	c.Path = ""
	c.Name = "changed"
	if _, err := s.CreateManagedProject(c, root, prep); err == nil || calls != 1 {
		t.Fatal("pending payload changed")
	}
}
