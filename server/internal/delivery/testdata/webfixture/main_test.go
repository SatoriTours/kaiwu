package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

func TestRealServiceDeliveryFromUnpreparedTask(t *testing.T) {
	for _, tc := range []struct {
		name, request, phase string
		manual               bool
		repairs, pushes      int
	}{
		{"normal", "正常自动交付", "succeeded", false, 0, 2},
		{"acceptance", "人工效果验收", "succeeded", true, 0, 2},
		{"repair-once", "修复后交付 [fail-once]", "succeeded", false, 1, 3},
		{"repair-limit", "始终失败 [fail-always]", "blocked", false, 3, 4},
		{"main-repair", "主分支失败后修复 [main-fail]", "succeeded", false, 1, 4},
		{"main-isolation", "主分支隔离 [isolation]", "succeeded", false, 0, 4},
	} {
		t.Run(tc.name, func(t *testing.T) { runDeliveryScenario(t, tc.request, tc.phase, tc.manual, tc.repairs, tc.pushes) })
	}
}

func runDeliveryScenario(t *testing.T, request, finalPhase string, manual bool, wantRepairs, wantPushes int) {
	root := t.TempDir()
	fake := filepath.Join(root, "fakepi")
	if b, e := exec.Command("go", "build", "-o", fake, "../../../agent/testdata/fakepi").CombinedOutput(); e != nil {
		t.Fatalf("fake build: %s %v", b, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	f, e := newFixture(ctx, root, fake, ln.Addr().String())
	if e != nil {
		ln.Close()
		t.Fatal(e)
	}
	server := &http.Server{Handler: f}
	go server.Serve(ln)
	defer func() { server.Close(); f.Close() }()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	base := "http://" + ln.Addr().String()
	post := func(path string, in any, out any) {
		t.Helper()
		b, _ := json.Marshal(in)
		req, _ := http.NewRequest("POST", base+path, strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", base)
		req.Header.Set("X-Kaiwu-Client", "web")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("%s: %d %s", path, res.StatusCode, body)
		}
		if out != nil {
			if err = json.NewDecoder(res.Body).Decode(out); err != nil {
				t.Fatal(err)
			}
		}
	}
	post("/api/login", map[string]string{"token": fixtureToken}, nil)
	p := workbench.DefaultDeliveryPolicy(f.projectID)
	p.Enabled = true
	p.RequireAcceptance = manual
	p.Repository = workbench.DeliveryRepository{ID: 42, Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo.git", MainBranch: "main"}
	p.Checks = []workbench.DeliveryCheck{{ID: "check", Argv: []string{"true"}, Dir: ".", TimeoutSeconds: 300}}
	p.TaskWorkflows = []workbench.DeliveryWorkflow{{ID: 1, Jobs: []string{"test"}}}
	p.MainWorkflows = []workbench.DeliveryWorkflow{{ID: 2, Jobs: []string{"test"}}}
	p.GitCredentialRef = "server_github_https"
	p.APICredentialRef = "server_github_api"
	p.AuthorizationDigest = "fixture-explicit-scope"
	p.Actor = "local-login"
	p.AuthorizedAt = time.Now().UTC()
	post("/api/delivery/policy", map[string]any{"operationId": "enable-fixture", "expectedVersion": 0, "policy": p}, nil)
	var task workbench.Receipt
	post("/api/commands", workbench.Command{OperationID: "add-feature", Type: "add_task", ProjectID: f.projectID, Request: request, Revision: f.store.Snapshot().Revision}, &task)
	var start workbench.Receipt
	post("/api/commands", workbench.Command{OperationID: "start-feature", Type: "start_task", ProjectID: f.projectID, TaskID: task.CreatedID, Revision: f.store.Snapshot().Revision}, &start)
	isolation := strings.Contains(request, "[isolation]")
	if isolation {
		f.mu.Lock()
		f.holdMain = true
		f.mu.Unlock()
	}
	deadline := time.Now().Add(20 * time.Second)
	accepted := false
	var second string
	sawQueue := false
	for time.Now().Before(deadline) {
		s := f.store.Snapshot()
		d := s.Deliveries[task.CreatedID]
		if isolation && d.Phase == "main_ci" && second == "" {
			var added workbench.Receipt
			post("/api/commands", workbench.Command{OperationID: "second-task", Type: "add_task", ProjectID: f.projectID, Request: "第二个功能", Revision: f.store.Snapshot().Revision}, &added)
			second = added.CreatedID
			post("/api/commands", workbench.Command{OperationID: "start-second", Type: "start_task", ProjectID: f.projectID, TaskID: second, Revision: f.store.Snapshot().Revision}, nil)
		}
		if isolation && second != "" && s.Deliveries[second].Phase == "preparing_integration" && !sawQueue {
			f.mu.Lock()
			count := 0
			for _, p := range f.pushes {
				if p.Ref == "refs/heads/main" {
					count++
				}
			}
			f.mu.Unlock()
			if count != 1 || d.Phase != "main_ci" {
				t.Fatal("second task bypassed unverified main CI")
			}
			sawQueue = true
			f.mu.Lock()
			f.holdMain = false
			f.mu.Unlock()
			f.worker.Wake()
		}
		if d.Phase == "awaiting_acceptance" && manual && !accepted {
			post("/api/delivery/accept", map[string]any{"operationId": "accept-candidate", "deliveryId": d.ID, "token": d.AcceptanceToken, "expectedVersion": d.Version}, nil)
			accepted = true
		}
		if (d.Phase == "blocked" || d.Phase == "needs_check") && d.Phase != finalPhase {
			t.Fatalf("delivery %s: %s %s", d.Phase, d.BlockCode, d.BlockReason)
		}
		if d.Phase == finalPhase {
			if isolation && (second == "" || s.Deliveries[second].Phase != "succeeded") {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			if isolation && !sawQueue {
				t.Fatal("did not observe queued second task")
			}
			if d.RepairsUsed != wantRepairs {
				t.Fatalf("repairs=%d want=%d (%s)", d.RepairsUsed, wantRepairs, d.BlockCode)
			}
			f.mu.Lock()
			count := len(f.pushes)
			f.mu.Unlock()
			if count != wantPushes {
				t.Fatalf("pushes=%d want=%d", count, wantPushes)
			}
			if finalPhase == "blocked" {
				return
			}
			expectedSHA := d.CandidateSHA
			if isolation {
				expectedSHA = s.Deliveries[second].CandidateSHA
			}
			actual, err := git(ctx, f.remote, "rev-parse", "refs/heads/main")
			if err != nil || actual != expectedSHA {
				t.Fatalf("remote receipt %q != %q (%v)", actual, expectedSHA, err)
			}
			if manual && !accepted {
				t.Fatal("manual cycle bypassed candidate acceptance")
			}
			if s.Tasks[task.CreatedID].Status == "completed" {
				t.Fatal("technical delivery fabricated human acceptance")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("delivery did not finish: %+v", f.store.Snapshot().Deliveries[task.CreatedID])
}
