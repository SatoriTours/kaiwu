//go:build linux

package preview

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

func setupProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func openManifest(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

type processGroup struct {
	mu                 sync.Mutex
	cmd                *exec.Cmd
	signalled, retired bool
}

func (p *processGroup) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.retired || p.signalled {
		return
	}
	p.signalled = true
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}
func (p *processGroup) wait() error {
	// As in gitops: leave the exited leader unreaped so its PID/PGID remains
	// reserved until descendants have been signalled. stop and wait serialize
	// retirement; no path ever signals a numeric group after cmd.Wait reaps it.
	var info unix.Siginfo
	var e error
	for {
		e = unix.Waitid(unix.P_PID, p.cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if e != syscall.EINTR {
			break
		}
	}
	p.mu.Lock()
	if e == nil && !p.signalled {
		p.signalled = true
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
	p.retired = true
	p.mu.Unlock()
	return errors.Join(e, p.cmd.Wait())
}
func groupMembers(group int) ([]string, error) {
	dirs, e := os.ReadDir("/proc")
	if e != nil {
		return nil, e
	}
	var members []string
	for _, dir := range dirs {
		if _, e := strconv.Atoi(dir.Name()); e != nil {
			continue
		}
		b, e := os.ReadFile(filepath.Join("/proc", dir.Name(), "stat"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		i := strings.LastIndex(string(b), ") ")
		if i < 0 {
			return nil, errors.New("invalid process stat")
		}
		f := strings.Fields(string(b)[i+2:])
		if len(f) < 3 {
			return nil, errors.New("incomplete process stat")
		}
		pg, e := strconv.Atoi(f[2])
		if e != nil {
			return nil, e
		}
		if pg == group && f[0] != "Z" && f[0] != "X" {
			members = append(members, dir.Name())
		}
	}
	return members, nil
}
func groupGone(group int) bool {
	members, e := groupMembers(group)
	return e == nil && len(members) == 0
}

// Every listener on this port must be loopback-only and its socket inode must
// occur in a live member of the process group we actually launched.
func ownedPort(port, group int) error {
	inodes := map[string]bool{}
	for _, name := range []string{"tcp", "tcp6"} {
		b, e := os.ReadFile(filepath.Join("/proc/net", name))
		if e != nil {
			return e
		}
		for _, line := range strings.Split(string(b), "\n")[1:] {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "0A" {
				continue
			}
			ip, hexport, ok := strings.Cut(f[1], ":")
			n, e := strconv.ParseInt(hexport, 16, 32)
			if !ok || e != nil || int(n) != port {
				continue
			}
			if name != "tcp" || ip != "0100007F" {
				return errors.New("预览监听地址不是指定 IPv4 回环地址")
			}
			inodes[f[9]] = false
		}
	}
	if len(inodes) == 0 {
		return errors.New("预览端口尚未监听")
	}
	members, e := groupMembers(group)
	if e != nil {
		return e
	}
	for _, pid := range members {
		fds, e := os.ReadDir(filepath.Join("/proc", pid, "fd"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		for _, fd := range fds {
			link, e := os.Readlink(filepath.Join("/proc", pid, "fd", fd.Name()))
			if e != nil {
				continue
			}
			if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
				inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
				if _, ok := inodes[inode]; ok {
					inodes[inode] = true
				}
			}
		}
	}
	for _, owned := range inodes {
		if !owned {
			return errors.New("端口不属于本任务预览进程组")
		}
	}
	return nil
}

type lockLease struct {
	file         *os.File
	marker, boot string
	attempted    bool
}

func acquireLock(tree string) (Lease, error) {
	parent := filepath.Dir(tree)
	f, e := os.OpenFile(filepath.Join(parent, "execution.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = f.Close()
		}
	}()
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return nil, errors.New("原执行者或预览仍占用任务工作区")
	}
	b, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return nil, e
	}
	l := &lockLease{file: f, marker: filepath.Join(parent, "process-owner.json"), boot: strings.TrimSpace(string(b))}
	if b, e := os.ReadFile(l.marker); e == nil {
		var old struct {
			Boot  string
			Group int
		}
		if json.Unmarshal(b, &old) != nil || old.Boot == "" {
			return nil, errors.New("旧进程归属记录损坏")
		}
		if old.Boot == l.boot && (old.Group <= 0 || !groupGone(old.Group)) {
			return nil, errors.New("旧进程仍存在或无法确认启动窗口，未重复启动")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	accepted = true
	return l, nil
}
func (l *lockLease) File() *os.File { return l.file }
func (l *lockLease) write(pid int) error {
	b, _ := json.Marshal(struct {
		Boot  string
		Group int
	}{l.boot, pid})
	f, e := os.OpenFile(l.marker+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return e
	}
	return os.Rename(l.marker+".tmp", l.marker)
}
func (l *lockLease) Attempt() error        { l.attempted = true; return l.write(0) }
func (l *lockLease) Started(pid int) error { return l.write(pid) }
func (l *lockLease) Release(safe bool) error {
	if l.file == nil {
		return nil
	}
	var e error
	if safe || !l.attempted {
		e = os.Remove(l.marker)
		if os.IsNotExist(e) {
			e = nil
		}
	}
	e = errors.Join(e, l.file.Close())
	l.file = nil
	return e
}
