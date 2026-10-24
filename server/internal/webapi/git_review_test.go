package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// fakeReviewGit keeps the legacy methods and adds the reviewed extension.
type fakeReviewGit struct {
	legacy   int
	local    int
	push     int
	lastMode string
	pushErr  bool
}

type legacyOnlyGit struct{ calls int }

func (f *legacyOnlyGit) PreviewMerge(context.Context, string, string) (workbench.MergePreview, error) {
	f.calls++
	return workbench.MergePreview{ID: "legacy"}, nil
}
func (f *legacyOnlyGit) StartMerge(workbench.MergeRequest) (workbench.MergeOperation, bool, error) {
	return workbench.MergeOperation{}, false, nil
}
func (f *legacyOnlyGit) RefreshGit(context.Context, string) error { return nil }
func (f *legacyOnlyGit) UpdateProjectGit(context.Context, workbench.Command) (workbench.Receipt, error) {
	return workbench.Receipt{}, nil
}

func (f *fakeReviewGit) PreviewMerge(context.Context, string, string) (workbench.MergePreview, error) {
	f.legacy++
	return workbench.MergePreview{ID: "legacy"}, nil
}
func (f *fakeReviewGit) PreviewMergeReview(_ context.Context, r workbench.MergeReviewRequest) (workbench.MergePreview, error) {
	f.local++
	f.lastMode = r.Mode + "/" + r.RoundID
	return workbench.MergePreview{ID: "review", Mode: r.Mode, RoundID: r.RoundID}, nil
}
func (f *fakeReviewGit) PreviewPushReview(_ context.Context, r workbench.MergeReviewRequest) (workbench.MergeReview, error) {
	if f.pushErr {
		return workbench.MergeReview{}, workbench.Conflict("推送合并尚未开通")
	}
	f.push++
	f.lastMode = r.Mode + "/" + r.RoundID
	return workbench.MergeReview{RemoteURL: "https://github.com/owner/repo.git", MainRef: "refs/heads/main", CanPush: true}, nil
}
func (f *fakeReviewGit) StartMerge(workbench.MergeRequest) (workbench.MergeOperation, bool, error) {
	return workbench.MergeOperation{}, false, nil
}
func (f *fakeReviewGit) RefreshGit(context.Context, string) error { return nil }
func (f *fakeReviewGit) UpdateProjectGit(context.Context, workbench.Command) (workbench.Receipt, error) {
	return workbench.Receipt{}, nil
}

func reviewHandler(t *testing.T, git GitCommander) (*handler, *http.Cookie, func(method, body string) *httptest.ResponseRecorder) {
	t.Helper()
	root := t.TempDir()
	s, e := workbench.Open(filepath.Join(root, "db"), workbench.Profile{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	token := strings.Repeat("x", 32)
	h := New(Config{Host: "127.0.0.1:7331", Token: token, Git: git}, s, nil).(*handler)
	login := httptest.NewRequest("POST", "http://127.0.0.1:7331/api/login", strings.NewReader(`{"token":"`+token+`"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", "http://127.0.0.1:7331")
	login.Header.Set("X-Kaiwu-Client", "web")
	lw := httptest.NewRecorder()
	h.ServeHTTP(lw, login)
	if lw.Code != 200 || len(lw.Result().Cookies()) == 0 {
		t.Fatalf("login failed: %d %s", lw.Code, lw.Body.String())
	}
	cookie := lw.Result().Cookies()[0]
	call := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:7331/api/git/preview", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:7331")
		req.Header.Set("X-Kaiwu-Client", "web")
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	return h, cookie, call
}

// The unchanged legacy request must keep its exact behaviour and payload.
func TestGitPreviewLegacyRequestUnchanged(t *testing.T) {
	git := &fakeReviewGit{}
	_, _, call := reviewHandler(t, git)
	w := call("POST", `{"projectId":"p","taskId":"t"}`)
	if w.Code == 401 {
		t.Fatal("handler requires auth; test must log in")
	}
	if w.Code != 200 || git.legacy != 1 || git.local != 0 {
		t.Fatalf("legacy preview changed: %d %#v", w.Code, git)
	}
	var body workbench.MergePreview
	if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil || body.ID != "legacy" || body.Mode != "" {
		t.Fatalf("legacy payload changed: %s", w.Body.String())
	}
}

func TestGitPreviewReviewedModes(t *testing.T) {
	git := &fakeReviewGit{}
	_, _, call := reviewHandler(t, git)
	w := call("POST", `{"projectId":"p","taskId":"t","mode":"local","roundId":"r1"}`)
	if w.Code != 200 || git.local != 1 || git.lastMode != "local/r1" {
		t.Fatalf("local review not routed: %d %#v %s", w.Code, git, w.Body.String())
	}
	var local workbench.MergePreview
	if e := json.Unmarshal(w.Body.Bytes(), &local); e != nil || local.Mode != "local" || local.RoundID != "r1" {
		t.Fatalf("local payload: %s", w.Body.String())
	}
	w = call("POST", `{"projectId":"p","taskId":"t","mode":"push","roundId":"r1"}`)
	if w.Code != 200 || git.push != 1 || git.local != 1 {
		t.Fatalf("push review not routed: %d %#v", w.Code, git)
	}
	var push workbench.MergeReview
	if e := json.Unmarshal(w.Body.Bytes(), &push); e != nil || push.RemoteURL == "" || !push.CanPush {
		t.Fatalf("push payload: %s", w.Body.String())
	}
	// Unknown modes are refused and never downgraded to the legacy preview.
	w = call("POST", `{"projectId":"p","taskId":"t","mode":"remote"}`)
	if w.Code == 200 || git.legacy != 0 {
		t.Fatalf("unknown mode accepted: %d %s", w.Code, w.Body.String())
	}
}

// A Git implementation without the extension keeps working for legacy requests
// and fails reviewed ones explicitly instead of silently merging.
func TestGitPreviewRequiresReviewCapability(t *testing.T) {
	git := &legacyOnlyGit{}
	_, _, call := reviewHandler(t, git)
	if w := call("POST", `{"projectId":"p","taskId":"t"}`); w.Code != 200 || git.calls != 1 {
		t.Fatalf("legacy broken: %d", w.Code)
	}
	for _, mode := range []string{"local", "push"} {
		if w := call("POST", `{"projectId":"p","taskId":"t","mode":"`+mode+`"}`); w.Code == 200 {
			t.Fatalf("%s accepted without capability", mode)
		}
	}
}

// The socket dispatch must carry the same optional fields.
func TestSocketGitPreviewCarriesReviewMode(t *testing.T) {
	git := &fakeReviewGit{}
	h, cookie, _ := reviewHandler(t, git)
	socketReq := func() *http.Request {
		req := httptest.NewRequest("POST", "http://127.0.0.1:7331/api/socket", nil)
		req.Header.Set("Origin", "http://127.0.0.1:7331")
		req.AddCookie(cookie)
		return req
	}
	reply := h.socketCall(socketReq(), socketRequest{ID: "1", Method: "git/preview", Body: json.RawMessage(`{"projectId":"p","taskId":"t","mode":"push","roundId":"r1"}`)})
	if reply.Status != 200 || git.push != 1 || git.lastMode != "push/r1" {
		t.Fatalf("socket mode lost: %#v %#v %s", reply, git, reply.Data)
	}
	var review workbench.MergeReview
	if e := json.Unmarshal(reply.Data, &review); e != nil || review.RemoteURL == "" {
		t.Fatalf("socket payload: %s", reply.Data)
	}
	if reply := h.socketCall(socketReq(), socketRequest{ID: "2", Method: "git/preview", Body: json.RawMessage(`{"projectId":"p","taskId":"t"}`)}); reply.Status != 200 || git.legacy != 1 {
		t.Fatalf("socket legacy broken: %#v %#v", reply, git)
	}
	if reply := h.socketCall(socketReq(), socketRequest{ID: "3", Method: "git/preview", Body: json.RawMessage(`{"projectId":"p","taskId":"t","mode":"weird"}`)}); reply.Status == 200 {
		t.Fatal("socket unknown mode accepted")
	}
}
