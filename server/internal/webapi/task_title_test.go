package webapi

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

type titleFixture struct {
	calls   atomic.Int32
	release chan struct{}
}

func (f *titleFixture) SummarizeTitle(ctx context.Context, request, provider, model, thinking, credentialSource string) (string, error) {
	f.calls.Add(1)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-f.release:
		return "完整需求的语义标题", nil
	}
}
func TestTitleJobIsDurableOnceAndDoesNotStartDevelopment(t *testing.T) {
	s, err := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{Provider: "fixture", Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := &titleFixture{release: make(chan struct{})}
	h := New(Config{Titles: f}, s, nil).(*handler)
	cmd := func(c workbench.Command) string {
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r.CreatedID
	}
	p := cmd(workbench.Command{OperationID: "project", Revision: s.Snapshot().Revision, Type: "add_project", Name: "test", Path: t.TempDir()})
	id := cmd(workbench.Command{OperationID: "task", Type: "add_task", ProjectID: p, Request: "前言并不是任务。真正任务在最后。", AutoTitle: true})
	defer h.Close()
	h.scheduleTitle(id)
	h.scheduleTitle(id)
	close(f.release)
	h.titleWG.Wait()
	task := s.Snapshot().Tasks[id]
	if task.Title != "完整需求的语义标题" || task.TitleState != "ready" || f.calls.Load() != 1 {
		t.Fatalf("%+v calls %d", task, f.calls.Load())
	}
	if task.Status != "idle" || task.ExecutionID != "" || len(s.Snapshot().Conversations[task.ConversationID].Messages) != 0 {
		t.Fatal("title changed development")
	}
	h.scheduleTitle(id)
	if f.calls.Load() != 1 {
		t.Fatal("replayed summary")
	}
}
func TestTitleShutdownAndRestartNeverReplay(t *testing.T) {
	s, err := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{Provider: "fixture", Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := &titleFixture{release: make(chan struct{})}
	h := New(Config{Titles: f}, s, nil).(*handler)
	_, err = s.Command(workbench.Command{OperationID: "project", Revision: s.Snapshot().Revision, Type: "add_project", Name: "test", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var p string
	for id := range s.Snapshot().Projects {
		p = id
	}
	r, err := s.Command(workbench.Command{OperationID: "task", Revision: s.Snapshot().Revision, Type: "add_task", ProjectID: p, Request: "test", AutoTitle: true})
	if err != nil {
		t.Fatal(err)
	}
	h.scheduleTitle(r.CreatedID)
	done := make(chan struct{})
	go func() { h.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown stuck")
	}
	if s.Snapshot().Tasks[r.CreatedID].TitleState != "failed" {
		t.Fatal("failure not saved")
	}
}

func TestTitleNeedsModelThenStartsOnceAndCrashDoesNotRetry(t *testing.T) {
	s, err := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := &titleFixture{release: make(chan struct{})}
	h := New(Config{Titles: f}, s, nil).(*handler)
	command := func(c workbench.Command) workbench.Receipt {
		c.Revision = s.Snapshot().Revision
		r, e := s.Command(c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	p := command(workbench.Command{OperationID: "project", Type: "add_project", Name: "p", Path: t.TempDir()}).CreatedID
	id := command(workbench.Command{OperationID: "task", Type: "add_task", ProjectID: p, Request: "完整需求", AutoTitle: true}).CreatedID
	h.scheduleTitle(id)
	if s.Snapshot().Tasks[id].TitleState != "needs_model" || f.calls.Load() != 0 {
		t.Fatal("missing model launched")
	}
	c := s.Snapshot().Tasks[id].ConversationID
	command(workbench.Command{OperationID: "model", Type: "set_conversation_profile", ProjectID: p, ConversationID: c, Provider: "fixture", Model: "test"})
	h.scheduleTitle(id)
	close(f.release)
	h.titleWG.Wait()
	h.Close()
	if s.Snapshot().Tasks[id].TitleState != "ready" || f.calls.Load() != 1 {
		t.Fatal("model selection did not summarize once")
	}
	if err = s.Update(func(st *workbench.State) error {
		t := st.Tasks[id]
		t.TitleState = "running"
		st.Tasks[id] = t
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h2 := New(Config{Titles: f}, s, nil).(*handler)
	defer h2.Close()
	h2.scheduleTitle(id)
	if s.Snapshot().Tasks[id].TitleState != "failed" || f.calls.Load() != 1 {
		t.Fatal("restart replayed model request")
	}
}

func TestTitlePendingCrashWindowBecomesExplicitFailure(t *testing.T) {
	s, e := workbench.Open(filepath.Join(t.TempDir(), "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Update(func(st *workbench.State) error {
		st.Tasks["pending"] = workbench.Task{ID: "pending", Title: "需求摘要", Request: "完整需求", TitleState: "pending"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	f := &titleFixture{release: make(chan struct{})}
	h := New(Config{Titles: f}, s, nil).(*handler)
	defer h.Close()
	if task := s.Snapshot().Tasks["pending"]; task.TitleState != "failed" || task.Title != "需求摘要" || task.TitleError == "" {
		t.Fatalf("stuck pending: %+v", task)
	}
	if f.calls.Load() != 0 {
		t.Fatal("replayed pending model call")
	}
}
