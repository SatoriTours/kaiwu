//go:build linux

package gitops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type groupRecorder struct {
	attempted bool
	pid       int
	fates     []bool
}

func (g *groupRecorder) File() *os.File { return nil }
func (g *groupRecorder) Attempt() error { g.attempted = true; return nil }
func (g *groupRecorder) Started(pid int) error {
	g.pid = pid
	return nil
}
func (g *groupRecorder) Finished(_ int, gone bool) { g.fates = append(g.fates, gone) }

// The real production remote runner must report the writer group's real fate,
// not just that a process was started: a surviving group may never look safe.
func TestProductionRemoteRunnerReportsGroupFate(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	work := filepath.Join(root, "work")
	for _, c := range [][]string{
		{"init", "--bare", "-b", "main", bare},
		{"init", "-b", "main", work},
	} {
		if out, e := exec.Command("git", c...).CombinedOutput(); e != nil {
			t.Fatalf("%v: %s %v", c, out, e)
		}
	}
	for _, c := range [][]string{
		{"config", "user.name", "T"}, {"config", "user.email", "t@example.invalid"},
		{"commit", "--allow-empty", "-m", "base"},
	} {
		if out, e := exec.Command("git", append([]string{"-C", work}, c...)...).CombinedOutput(); e != nil {
			t.Fatalf("%v: %s %v", c, out, e)
		}
	}
	sha := workspaceHead(t, work)
	runner := NewRemoteRunner(RemoteConfig{})
	rec := &groupRecorder{}
	// Keep the production Git/lease/process-group path real, but map the
	// approved URL to a temporary bare repository at the executable boundary.
	// The remote URL validator must not be weakened to allow file:// in tests.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	url := "https://github.com/owner/repo.git"
	script := fmt.Sprintf("#!/bin/bash\nargs=()\nfor arg in \"$@\"; do\n if [[ \"$arg\" == %q ]]; then arg=%q; fi\n args+=(\"$arg\")\ndone\nexec %q \"\u0024{args[@]}\"\n", url, bare, realGit)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	target := RemoteTarget{URL: url, MainRef: "refs/heads/main"}
	if _, e := runner.Push(t.Context(), work, target, sha, "refs/heads/main", "", rec); e != nil {
		t.Fatalf("push: %v", e)
	}
	if !rec.attempted || rec.pid <= 0 {
		t.Fatalf("production path did not attempt/start through the lease: %#v", rec)
	}
	if len(rec.fates) == 0 {
		t.Fatal("production path never reported the writer group fate")
	}
	for _, gone := range rec.fates {
		if !gone {
			t.Fatalf("completed push reported a surviving group: %v", rec.fates)
		}
	}
}

// groupGone, the helper production uses, must distinguish a live group from a
// dead one — a real process, no injection.
func TestGroupGoneDetectsLiveAndDeadGroups(t *testing.T) {
	cmd := exec.Command("sleep", "300")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	pid := cmd.Process.Pid
	if groupGone(pid) {
		t.Fatal("live group reported gone")
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_, _ = cmd.Process.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for !groupGone(pid) {
		if time.Now().After(deadline) {
			t.Fatal("dead group still reported alive")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func workspaceHead(t *testing.T, dir string) string {
	t.Helper()
	out, e := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if e != nil {
		t.Fatal(e)
	}
	return string(out[:40])
}
