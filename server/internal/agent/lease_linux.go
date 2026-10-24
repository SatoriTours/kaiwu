//go:build linux

package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type executionLease struct {
	mu                sync.Mutex
	file              *os.File
	marker            string
	boot              string
	attempted, closed bool
	// groupKnown/groupPID remember the writer group this lease itself recorded,
	// so a second launch can never overwrite the marker that still proves it.
	groupKnown bool
	groupPID   int
	// poison marks a launch the lease knows exists but can never attribute, so
	// no release may clear the marker for it.
	poison bool
}
type processOwner struct {
	Boot  string
	Group int
}

func (l *executionLease) inheritedFile() *os.File { return l.file }

// The OS lock prevents concurrent hosts. The durable marker survives SIGKILL:
// a new host must verify the old group is absent before it may acquire a writer.
// Unknown launch windows fail closed; no stored PID is ever used to kill.
func acquireExecutionLease(workspace string) (*executionLease, error) {
	return acquireLease(filepath.Dir(workspace), "execution.lock", "process-owner.json")
}
func acquireProjectMergeLease(commonDir string) (*executionLease, error) {
	return acquireLease(commonDir, "kaiwu-merge.lock", "kaiwu-merge-owner.json")
}
func acquireLease(parent, lockName, markerName string) (*executionLease, error) {
	f, e := os.OpenFile(filepath.Join(parent, lockName), os.O_RDWR|os.O_CREATE, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		_ = f.Close()
		return nil, errors.New("原任务执行者仍占用工作区，未启动另一个进程")
	}
	l := &executionLease{file: f, marker: filepath.Join(parent, markerName)}
	accepted := false
	defer func() {
		if !accepted {
			_ = f.Close()
		}
	}()
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return nil, errors.New("无法核实执行进程归属")
	}
	l.boot = strings.TrimSpace(string(boot))
	if b, err := os.ReadFile(l.marker); err == nil {
		var owner processOwner
		if json.Unmarshal(b, &owner) != nil || owner.Boot == "" {
			return nil, errors.New("旧执行归属记录损坏，请先检查保留的工作区")
		}
		if owner.Boot == l.boot {
			if owner.Group <= 0 {
				return nil, errors.New("上次启动期间服务异常退出，无法确认旧进程归属；请先检查保留的工作区")
			}
			gone, err := groupAbsent(owner.Group)
			if err != nil || !gone {
				return nil, errors.New("旧任务进程仍存在或无法核实已退出，未启动另一个执行者")
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if e = l.writeOwner(0); e != nil {
		return nil, e
	}
	accepted = true
	return l, nil
}

// procStatGone reports whether a /proc/<pid>/stat read failure only means the
// process disappeared between readdir and read. Linux returns ESRCH for a pid
// that exited inside that window as well as ENOENT for one that never came
// back, and os.IsNotExist recognises neither wrapper for ESRCH. Treating a
// vanished unrelated process as an unverifiable Git group makes the lease
// refuse ownership forever, so both "gone" signals are classified here.
func procStatGone(err error) bool {
	if err == nil {
		return false
	}
	return os.IsNotExist(err) || errors.Is(err, syscall.ESRCH)
}

func groupAbsent(group int) (bool, error) {
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return false, e
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if procStatGone(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		i := strings.LastIndex(string(b), ") ")
		if i < 0 {
			return false, errors.New("invalid process identity")
		}
		fields := strings.Fields(string(b)[i+2:])
		if len(fields) < 3 {
			return false, errors.New("incomplete process identity")
		}
		pgid, err := strconv.Atoi(fields[2])
		if err != nil {
			return false, err
		}
		if pgid == group && fields[0] != "Z" && fields[0] != "X" {
			return false, nil
		}
	}
	return true, nil
}

func (l *executionLease) writeOwner(pid int) error {
	b, _ := json.Marshal(processOwner{Boot: l.boot, Group: pid})
	f, e := os.OpenFile(l.marker+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(l.marker+".tmp", l.marker)
}

var errGroupUnproven = errors.New("上一个 Git 进程组未确认退出，未执行后续命令")

// settle clears a previous writer group only when it is provably gone. An
// unproven group keeps the durable marker, so a later launch of the same
// operation can never overwrite it with an already reaped successor.
func (l *executionLease) settle() error {
	if !l.groupKnown {
		return nil
	}
	gone, err := groupAbsent(l.groupPID)
	if err != nil || !gone {
		return errGroupUnproven
	}
	l.groupKnown, l.groupPID = false, 0
	return nil
}
func (l *executionLease) attempt() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("执行归属已关闭，未记录 Git 启动")
	}
	if l.poison || (l.attempted && !l.groupKnown) {
		// A launch window that recorded no process can never be reopened: the
		// child may exist, so a new Attempt must not overwrite its record.
		return errGroupUnproven
	}
	if e := l.settle(); e != nil {
		return e
	}
	l.attempted = true
	// Every launch has its own unknown window, including successive Git calls.
	// Never leave a previous, already reaped process as proof of a new writer.
	return l.writeOwner(0)
}
func (l *executionLease) started(pid int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("执行归属已关闭，未记录 Git 进程")
	}
	if l.poison {
		return errGroupUnproven
	}
	if pid <= 0 {
		l.poison = true
		return errors.New("无法记录 Git 进程归属")
	}
	if e := l.settle(); e != nil {
		return e
	}
	// Record ownership before the durable write, so a marker write failure still
	// leaves this lease unable to claim its process group is gone.
	l.groupKnown, l.groupPID, l.attempted = true, pid, true
	return l.writeOwner(pid)
}

// noProcess clears the unknown launch window when the caller can prove no child
// process was ever started (resolver or exec.Start failure). Without that proof
// the marker stays, so a plain failure cannot silently free ownership.
func (l *executionLease) noProcess() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("执行归属已关闭，未记录未启动证明")
	}
	if l.poison {
		// A launch the lease cannot attribute is never "nothing started"; only
		// the process really exiting can resolve it.
		return errors.New("存在未确认的 Git 进程归属，未记录未启动证明")
	}
	// A group this lease really recorded is only cleared by a proven exit; a
	// caller-provided "no process" claim may never erase it.
	if e := l.settle(); e != nil {
		return e
	}
	l.groupKnown, l.groupPID, l.attempted = false, 0, false
	if e := os.Remove(l.marker); e != nil && !os.IsNotExist(e) {
		return e
	}
	return nil
}

// unproven reports whether this lease still owns a group it cannot prove gone,
// independent of any controller flag or transport callback.
func (l *executionLease) unproven() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.groupKnown {
		gone, err := groupAbsent(l.groupPID)
		return err != nil || !gone
	}
	// A launch that recorded no process is unknown, not empty.
	return l.attempted
}
func (l *executionLease) unused() bool { l.mu.Lock(); defer l.mu.Unlock(); return !l.attempted }
func (l *executionLease) release(confirmed bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	gone := true
	if l.groupKnown {
		g, err := groupAbsent(l.groupPID)
		gone = err == nil && g
	}
	// Only an unused lease or a recorded group proven gone may drop ownership.
	// A launch window whose child was never attributed keeps the marker: the
	// caller's confirmation is not evidence and cannot erase that window, and a
	// poisoned launch is never cleared.
	drop := !l.attempted || (l.groupKnown && gone)
	_ = confirmed
	var e error
	switch {
	case drop:
		e = os.Remove(l.marker)
		if os.IsNotExist(e) {
			e = nil
		}
	case confirmed:
		e = errors.New("Git 进程组未确认退出，已保留归属记录")
	}
	return errors.Join(e, l.file.Close())
}

func processGroupGone(pid int) bool { gone, e := groupAbsent(pid); return e == nil && gone }
