// Browser fixture only. Synthetic records, temporary SQLite, no Agent runtime.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/webapi"
	"github.com/SatoriTours/kaiwu/server/internal/webui"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	root := flag.String("root", "", "temporary data directory")
	flag.Parse()
	if *root == "" {
		panic("root required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := workbench.Open(filepath.Join(*root, "state.db"), workbench.Profile{Provider: "fixture", Model: "synthetic"})
	must(err)
	defer store.Close()
	command := func(c workbench.Command) string {
		c.Revision = store.Snapshot().Revision
		r, e := store.Command(c)
		must(e)
		return r.CreatedID
	}
	p := command(workbench.Command{OperationID: "project", Type: "add_project", Name: "子 Agent 观察测试", Path: filepath.Join(*root, "project")})
	for i, title := range []string{"主要任务", "隔离任务"} {
		task := command(workbench.Command{OperationID: fmt.Sprint("task", i), Type: "add_task", ProjectID: p, Title: title, Request: "仅合成事件，无真实派发"})
		c := store.Snapshot().Tasks[task].ConversationID
		round := command(workbench.Command{OperationID: fmt.Sprint("round", i), Type: "send_message", ProjectID: p, TaskID: task, ConversationID: c, Text: "测试记录"})
		statuses := []string{workbench.SubagentStatusRunning, workbench.SubagentStatusUnknown, workbench.SubagentStatusCompleted}
		if i == 1 {
			statuses = []string{workbench.SubagentStatusCompleted}
		}
		for j, status := range statuses {
			id := fmt.Sprintf("run-%d-%d", i, j)
			name := []string{"检查实现 <script>alert(1)</script>", "归属待核查", "历史审查"}[j]
			if i == 1 {
				name = "另一任务专属记录"
			}
			_, err = store.CreateSubagentRun(workbench.SubagentSummary{ID: id, ProjectID: p, ConversationID: c, TaskID: task, RoundID: round, ParentSessionID: store.Snapshot().Conversations[c].SessionID, Title: name, TaskSummary: "读取实际工具输出与任务要求", Status: workbench.SubagentStatusStarting, Profile: workbench.Profile{Provider: "fixture", Model: "synthetic"}}, "完整要求：检查当前实现，不修改文件。<img src=x onerror=alert(1)>")
			must(err)
			appendEvent := func(e workbench.SubagentEvent) { _, err = store.AppendSubagentEvent(c, id, e); must(err) }
			appendEvent(workbench.SubagentEvent{ID: "start", Kind: workbench.SubagentEventStatus, Status: workbench.SubagentStatusRunning})
			appendEvent(workbench.SubagentEvent{ID: "read", Kind: workbench.SubagentEventToolStart, ToolID: "tool1", ToolName: "read", Input: "{\"path\":\"README.md\"}"})
			appendEvent(workbench.SubagentEvent{ID: "result", Kind: workbench.SubagentEventToolEnd, ToolID: "tool1", ToolName: "read", Output: "工具输出：合成内容 <script>alert(2)</script>"})
			for n := 0; n < 105; n++ {
				appendEvent(workbench.SubagentEvent{ID: fmt.Sprint("text", n), Kind: workbench.SubagentEventText, Text: fmt.Sprintf("记录 %03d", n)})
			}
			if status != workbench.SubagentStatusRunning {
				appendEvent(workbench.SubagentEvent{ID: "end", Kind: workbench.SubagentEventStatus, Status: status})
			}
		}
	}
	handler := webapi.New(webapi.Config{Context: ctx, Host: "127.0.0.1:17333", Token: "kaiwu-subagent-fixture-token-000000", Assets: webui.Files(), Health: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })}, store, nil)
	mux := http.NewServeMux()
	// Test-only mutation for exercising the real Store -> socket patch -> UI path.
	mux.HandleFunc("POST /fixture/append", func(w http.ResponseWriter, r *http.Request) {
		run := store.Snapshot().Subagents["run-0-0"]
		_, e := store.AppendSubagentEvent(run.ConversationID, run.ID, workbench.SubagentEvent{ID: fmt.Sprint("live", run.Version), Kind: workbench.SubagentEventText, Text: fmt.Sprint("实时增量 ", run.Version)})
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})
	mux.Handle("/", handler)
	srv := &http.Server{Addr: "127.0.0.1:17333", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = srv.Shutdown(shutdown)
	}()
	if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
