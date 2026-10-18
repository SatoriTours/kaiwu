package delivery

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// TestDeliveryCheckHelper provides the check payloads. All inputs arrive via
// argv after the standard "--" separator, never via the environment.
func TestDeliveryCheckHelper(t *testing.T) {
	args := os.Args
	if len(args) < 3 || args[len(args)-3] != "--" {
		t.Skip("helper")
	}
	mode, payload := args[len(args)-2], args[len(args)-1]
	switch mode {
	case "dump-env":
		var b bytes.Buffer
		for _, line := range os.Environ() {
			b.WriteString(line + "\n")
		}
		if e := os.WriteFile(payload, b.Bytes(), 0600); e != nil {
			os.Exit(3)
		}
	case "emit-overflow":
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 10<<20))
		os.Exit(0)
	case "emit-failure":
		_, _ = os.Stdout.WriteString(strings.Repeat("y", 300<<10))
		os.Exit(3)
	case "linger":
		exe, _ := os.Executable()
		child := exec.Command(exe, "-test.run=^TestDeliveryCheckChild$", "--", payload)
		_ = child.Start()
		time.Sleep(30 * time.Second)
		_ = child.Process.Kill()
		os.Exit(0)
	case "abandon":
		exe, _ := os.Executable()
		child := exec.Command(exe, "-test.run=^TestDeliveryCheckChild$", "--", payload)
		_ = child.Start()
		os.Exit(0)
	default:
		t.Skip("helper")
	}
}

func TestDeliveryCheckChild(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		t.Skip("child")
	}
	time.Sleep(60 * time.Second)
}

type stubLease struct {
	mu           sync.Mutex
	attempts     int
	started      []int
	released     bool
	releasedSafe bool
	accept       bool
}

func (l *stubLease) File() *os.File { return nil }
func (l *stubLease) Attempt() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts++
	if !l.accept {
		return fmt.Errorf("租约拒绝")
	}
	return nil
}
func (l *stubLease) Started(pid int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.started = append(l.started, pid)
	return nil
}
func (l *stubLease) Release(safe bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released, l.releasedSafe = true, safe
	return nil
}
func (l *stubLease) journal() (int, []int, bool, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.attempts, append([]int{}, l.started...), l.released, l.releasedSafe
}

func checkCommand(t *testing.T, mode, payload string, timeoutSeconds int64) workbench.DeliveryCheck {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	return workbench.DeliveryCheck{
		ID:             mode,
		Argv:           []string{exe, "-test.run=^TestDeliveryCheckHelper$", "--", mode, payload},
		Dir:            ".",
		TimeoutSeconds: timeoutSeconds,
	}
}

func revParse(t *testing.T, ws string, args ...string) string {
	t.Helper()
	gitArgs := append([]string{"-C", ws}, args...)
	c := exec.Command("git", gitArgs...)
	b, e := c.Output()
	if e != nil {
		t.Fatal(e)
	}
	return strings.TrimSpace(string(b))
}

func checksFixture(t *testing.T) workbench.Execution {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "tree")
	if e := os.Mkdir(ws, 0700); e != nil {
		t.Fatal(e)
	}
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", ws}, args...)...)
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %s", args, b)
		}
	}
	git("init", "--initial-branch=main", "--template=")
	os.WriteFile(filepath.Join(ws, "base.txt"), []byte("base\n"), 0600)
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "base")
	return workbench.Execution{ID: "x", TaskID: "task", Workspace: ws, Branch: "kaiwu/task-task", BaseSHA: revParse(t, ws, "rev-parse", "HEAD")}
}

func headOf(t *testing.T, ws string) string {
	t.Helper()
	return revParse(t, ws, "rev-parse", "HEAD")
}

// lingering reports whether any live process still carries the token argv.
func lingering(t *testing.T, token string) bool {
	t.Helper()
	entries, e := os.ReadDir("/proc")
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if _, e := fmt.Sscanf(entry.Name(), "%d", new(int)); e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if e != nil {
			continue
		}
		if bytes.Contains(b, []byte(token)) {
			return true
		}
	}
	return false
}

func TestDeliveryChecksProcessBounds(t *testing.T) {
	ctx := context.Background()

	t.Run("lease refusal aborts before any process", func(t *testing.T) {
		x := checksFixture(t)
		lease := &stubLease{}
		_, e := RunChecks(ctx, x, []workbench.DeliveryCheck{checkCommand(t, "dump-env", filepath.Join(t.TempDir(), "env.txt"), 300)}, lease)
		if e == nil {
			t.Fatal("refused lease accepted")
		}
		if !leaseRefused(e) {
			t.Fatalf("wrong refusal error: %v", e)
		}
		if _, started, _, _ := lease.journal(); len(started) != 0 {
			t.Fatal("check ran despite lease refusal")
		}
	})

	t.Run("argv executed directly without shell concatenation", func(t *testing.T) {
		x := checksFixture(t)
		target := "flag1; touch flag2"
		lease := &stubLease{accept: true}
		_, e := RunChecks(ctx, x, []workbench.DeliveryCheck{{
			ID:             "probe",
			Argv:           []string{"/bin/touch", target},
			Dir:            ".",
			TimeoutSeconds: 300,
		}}, lease)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := os.Stat(filepath.Join(x.Workspace, target)); e != nil {
			t.Fatal("argv result missing:", e)
		}
		if _, e := os.Stat(filepath.Join(x.Workspace, "flag2")); e == nil {
			t.Fatal("shell concatenation leaked")
		}
		attempts, started, _, _ := lease.journal()
		if attempts != 1 || len(started) != 1 || started[0] <= 0 {
			t.Fatalf("lease journal wrong: attempts=%d started=%v", attempts, started)
		}
	})

	t.Run("failed exit is not passed and summary is bounded", func(t *testing.T) {
		x := checksFixture(t)
		lease := &stubLease{accept: true}
		ev, e := RunChecks(ctx, x, []workbench.DeliveryCheck{checkCommand(t, "emit-failure", "", 300)}, lease)
		if e != nil {
			t.Fatal(e)
		}
		if ev.Passed {
			t.Fatal("failing check marked passed")
		}
		if ev.Truncated {
			t.Fatal("failure marked truncated")
		}
		if len(ev.Summary) == 0 || len(ev.Summary) > 256<<10 {
			t.Fatalf("summary not bounded: %d", len(ev.Summary))
		}
		if ev.Fingerprint == "" || ev.Tree == "" {
			t.Fatal("evidence lacks fingerprint or tree")
		}
	})

	t.Run("output cap truncates and invalidates", func(t *testing.T) {
		x := checksFixture(t)
		lease := &stubLease{accept: true}
		ev, e := RunChecks(ctx, x, []workbench.DeliveryCheck{checkCommand(t, "emit-overflow", "", 300)}, lease)
		if e != nil {
			t.Fatal(e)
		}
		if !ev.Truncated {
			t.Fatal("output cap not flagged")
		}
		if ev.Passed {
			t.Fatal("truncated evidence claimed passed")
		}
		if len(ev.Summary) > 256<<10 {
			t.Fatalf("summary overflow: %d", len(ev.Summary))
		}
	})

	t.Run("per-check timeout reaps the whole group", func(t *testing.T) {
		x := checksFixture(t)
		token := fmt.Sprintf("dlv-%d", time.Now().UnixNano())
		lease := &stubLease{accept: true}
		start := time.Now()
		ev, e := RunChecks(ctx, x, []workbench.DeliveryCheck{checkCommand(t, "linger", token, 2)}, lease)
		elapsed := time.Since(start)
		if e != nil {
			t.Fatal(e)
		}
		if elapsed >= 6*time.Second {
			t.Fatalf("check did not honor timeout: %s", elapsed)
		}
		if ev.Passed {
			t.Fatal("timed-out check marked passed")
		}
		end := time.Now().Add(2 * time.Second)
		for lingering(t, token) {
			if time.Now().After(end) {
				t.Fatal("timeout left process group alive")
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	t.Run("three-hundred-second runtime cap", func(t *testing.T) {
		x := checksFixture(t)
		previous := checkCommandCap
		checkCommandCap = 600 * time.Millisecond
		defer func() { checkCommandCap = previous }()
		lease := &stubLease{accept: true}
		start := time.Now()
		_, e := RunChecks(ctx, x, []workbench.DeliveryCheck{checkCommand(t, "linger", "dlvcap-token", 604800)}, lease)
		if e != nil {
			t.Fatal(e)
		}
		if elapsed := time.Since(start); elapsed >= 5*time.Second {
			t.Fatalf("runtime cap not applied: %s", elapsed)
		}
		end := time.Now().Add(2 * time.Second)
		for lingering(t, "dlvcap-token") {
			if time.Now().After(end) {
				t.Fatal("cap left process group alive")
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	t.Run("check directory may not leave the workspace", func(t *testing.T) {
		x := checksFixture(t)
		outside := filepath.Join(filepath.Dir(x.Workspace), "dlv-escape.txt")
		os.Remove(outside)
		lease := &stubLease{accept: true}
		_, e := RunChecks(ctx, x, []workbench.DeliveryCheck{{
			ID:             "escape",
			Argv:           []string{"/bin/touch", "dlv-escape.txt"},
			Dir:            "../",
			TimeoutSeconds: 300,
		}}, lease)
		if e == nil {
			t.Fatal("escaping check directory accepted")
		}
		if _, started, _, _ := lease.journal(); len(started) != 0 {
			t.Fatal("escaping check executed")
		}
		if _, e := os.Stat(outside); e == nil {
			t.Fatal("check wrote outside the workspace")
		}
	})

	t.Run("symlinked check directory stays in bounds", func(t *testing.T) {
		x := checksFixture(t)
		outside := t.TempDir()
		if e := os.Symlink(outside, filepath.Join(x.Workspace, "sub")); e != nil {
			t.Fatal(e)
		}
		lease := &stubLease{accept: true}
		_, e := RunChecks(ctx, x, []workbench.DeliveryCheck{{
			ID:             "symlink",
			Argv:           []string{"/bin/touch", "dlv-symlink.txt"},
			Dir:            "sub",
			TimeoutSeconds: 300,
		}}, lease)
		if e == nil {
			t.Fatal("symlinked check directory accepted")
		}
		if _, e := os.Stat(filepath.Join(outside, "dlv-symlink.txt")); e == nil {
			t.Fatal("check wrote through symlink")
		}
	})

	t.Run("environment stays on the whitelist", func(t *testing.T) {
		x := checksFixture(t)
		t.Setenv("KAIWU_GITHUB_API_TOKEN", "leaky-token")
		t.Setenv("GIT_FAKE_TOKEN", "leaky-git")
		t.Setenv("SSH_AUTH_SOCK", "leak")
		envPath := filepath.Join(t.TempDir(), "env.txt")
		lease := &stubLease{accept: true}
		if _, e := RunChecks(ctx, x, []workbench.DeliveryCheck{checkCommand(t, "dump-env", envPath, 300)}, lease); e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(envPath)
		if e != nil {
			t.Fatal(e)
		}
		dump := string(b)
		for _, bad := range []string{"KAIWU_GITHUB_API_TOKEN", "leaky-token", "GIT_FAKE_TOKEN", "leaky-git", "SSH_AUTH_SOCK"} {
			if strings.Contains(dump, bad) {
				t.Fatalf("check environment leaked %s", bad)
			}
		}
		if !strings.Contains(dump, "PATH=") {
			t.Fatal("check environment lost PATH")
		}
	})

	t.Run("left-behind descendants are reaped", func(t *testing.T) {
		x := checksFixture(t)
		token := fmt.Sprintf("dlv-a-%d", time.Now().UnixNano())
		lease := &stubLease{accept: true}
		ev, e := RunChecks(ctx, x, []workbench.DeliveryCheck{checkCommand(t, "abandon", token, 300)}, lease)
		if e != nil {
			t.Fatal(e)
		}
		if !ev.Passed {
			t.Fatal("clean check failed", ev.Summary)
		}
		end := time.Now().Add(2 * time.Second)
		for lingering(t, token) {
			if time.Now().After(end) {
				t.Fatal("descendant survived the check")
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	t.Run("no change creates no duplicate commit", func(t *testing.T) {
		x := checksFixture(t)
		lease := &stubLease{accept: true}
		ev, e := RunChecks(ctx, x, []workbench.DeliveryCheck{checkCommand(t, "dump-env", filepath.Join(t.TempDir(), "env2.txt"), 300)}, lease)
		if e != nil {
			t.Fatal(e)
		}
		if !ev.Passed {
			t.Fatal("clean workspace check failed", ev.Summary)
		}
		if head := headOf(t, x.Workspace); head != x.BaseSHA {
			t.Fatalf("unexpected commit %s", head)
		}
	})

	t.Run("candidate changes during checks invalidate evidence", func(t *testing.T) {
		x := checksFixture(t)
		lease := &stubLease{accept: true}
		ev, e := RunChecks(ctx, x, []workbench.DeliveryCheck{{
			ID:             "mutate",
			Argv:           []string{"/bin/touch", "generated"},
			Dir:            ".",
			TimeoutSeconds: 300,
		}}, lease)
		if e != nil {
			t.Fatal(e)
		}
		if info, e := os.Stat(filepath.Join(x.Workspace, "generated")); e != nil || info.IsDir() {
			t.Fatal("fixture check did not run", e)
		}
		if ev.Passed {
			t.Fatal("unknown post-check workspace changes passed")
		}
	})
}

var _ agent.DeliveryLease = (*stubLease)(nil)
