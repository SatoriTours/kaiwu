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
}
type processOwner struct {
	Boot  string
	Group int
}

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
		if os.IsNotExist(err) {
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
func (l *executionLease) attempt() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempted = true
	// Every launch has its own unknown window, including successive Git calls.
	// Never leave a previous, already reaped process as proof of a new writer.
	return l.writeOwner(0)
}
func (l *executionLease) started(pid int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writeOwner(pid)
}
func (l *executionLease) unused() bool { l.mu.Lock(); defer l.mu.Unlock(); return !l.attempted }
func (l *executionLease) release(confirmed bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	var e error
	if confirmed || !l.attempted {
		e = os.Remove(l.marker)
		if os.IsNotExist(e) {
			e = nil
		}
	}
	return errors.Join(e, l.file.Close())
}

func processGroupGone(pid int) bool { gone, e := groupAbsent(pid); return e == nil && gone }
