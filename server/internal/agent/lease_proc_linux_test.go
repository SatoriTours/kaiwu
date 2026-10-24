//go:build linux

package agent

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// TestProcStatGoneClassifiesDisappearedProcess pins the classification of a
// /proc/<pid>/stat read failure that only means the process vanished between
// readdir and read. Linux returns ESRCH, not ENOENT, for a pid that exits inside
// that window; os.IsNotExist does not recognise it, so an unrelated process
// disappearing during a churn was misread as "the Git group cannot be proven
// gone" and refused the lease. Both gone signals must classify as gone, and any
// other failure must stay fail-closed.
func TestProcStatGoneClassifiesDisappearedProcess(t *testing.T) {
	statPath := "/proc/31337/stat"
	cases := []struct {
		name string
		err  error
		gone bool
	}{
		{"enoent", &os.PathError{Op: "open", Path: statPath, Err: syscall.ENOENT}, true},
		{"esrch", &os.PathError{Op: "open", Path: statPath, Err: syscall.ESRCH}, true},
		{"esrch_wrapped", errors.Join(errors.New("read stat"), syscall.ESRCH), true},
		{"eacces", &os.PathError{Op: "open", Path: statPath, Err: syscall.EACCES}, false},
		{"eperm", &os.PathError{Op: "open", Path: statPath, Err: syscall.EPERM}, false},
		{"plain", errors.New("i/o error"), false},
	}
	for _, c := range cases {
		if got := procStatGone(c.err); got != c.gone {
			t.Errorf("%s: procStatGone = %t, want %t (%v)", c.name, got, c.gone, c.err)
		}
	}
}

// TestGroupAbsentSurvivesProcChurn reproduces the observed window against the
// real /proc: unrelated short-lived processes exit between readdir and the stat
// read, which Linux answers with ESRCH. A group that does not exist must still
// be reported gone instead of becoming unverifiable.
func TestGroupAbsentSurvivesProcChurn(t *testing.T) {
	for i := 0; i < 200; i++ {
		cmd := shortLivedProcess(t)
		if e := cmd.Start(); e != nil {
			t.Fatalf("cannot spawn churn process: %v", e)
		}
		gone, e := groupAbsent(1 << 30)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if e != nil {
			t.Fatalf("round %d: unrelated /proc churn made an absent group unverifiable: %v", i, e)
		}
		if !gone {
			t.Fatalf("round %d: absent group reported present", i)
		}
	}
}

// shortLivedProcess starts a process that exits immediately, so the pid is
// present in one /proc readdir pass and gone by the following stat read.
func shortLivedProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	exe, e := exec.LookPath("true")
	if e != nil {
		t.Fatalf("no short-lived binary available: %v", e)
	}
	return exec.Command(exe)
}
