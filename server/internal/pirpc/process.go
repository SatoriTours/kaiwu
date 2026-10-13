package pirpc

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"
)

type processExit struct {
	ExitCode *int
	Reaped   bool
	Err      error
}
type process struct {
	stdin, stdout, stderr            *os.File
	cmd                              *exec.Cmd
	pid                              int
	exited                           chan struct{}
	exit                             processExit // published before exited closes
	kill                             func() error
	stdinOnce, readersOnce, stopOnce sync.Once
	stdinErr, readersErr, stopErr    error
	stopExit                         processExit
	forced                           bool
	ownedGroup                       bool
	groupMu                          sync.Mutex
	groupRetired                     bool
	groupErr                         error
}

func startProcess(s LaunchSpec) (*process, error) {
	if err := validateLaunch(s); err != nil {
		return nil, err
	}
	var files []*os.File
	pipe := func() (*os.File, *os.File, error) {
		r, w, err := os.Pipe()
		if err == nil {
			files = append(files, r, w)
		}
		return r, w, err
	}
	cleanup := func() {
		for _, f := range files {
			_ = f.Close()
		}
	}
	inR, inW, err := pipe()
	if err != nil {
		cleanup()
		return nil, newError("io", NotSent, err)
	}
	outR, outW, err := pipe()
	if err != nil {
		cleanup()
		return nil, newError("io", NotSent, err)
	}
	errR, errW, err := pipe()
	if err != nil {
		cleanup()
		return nil, newError("io", NotSent, err)
	}
	cmd := exec.Command(s.Program, append([]string(nil), s.Args...)...)
	cmd.Dir = s.Dir
	cmd.Env = append([]string{}, s.Env...)
	cmd.Stdin = inR
	cmd.Stdout = outW
	cmd.Stderr = errW
	if err = configureProcessGroup(cmd, s.OwnProcessGroup); err != nil {
		cleanup()
		return nil, newError("io", NotSent, err)
	}
	if err = cmd.Start(); err != nil {
		cleanup()
		return nil, newError("io", NotSent, err)
	}
	// The parent owns only these three ends; Cmd.Wait must not close readers.
	_ = inR.Close()
	_ = outW.Close()
	_ = errW.Close()
	p := &process{stdin: inW, stdout: outR, stderr: errR, cmd: cmd, pid: cmd.Process.Pid, exited: make(chan struct{}), kill: cmd.Process.Kill}
	p.ownedGroup = s.OwnProcessGroup
	if p.ownedGroup {
		p.kill = func() error {
			p.groupMu.Lock()
			defer p.groupMu.Unlock()
			if p.groupRetired {
				return nil
			}
			return stopProcessGroup(p.pid)
		}
	}
	go func() {
		if p.ownedGroup {
			p.groupErr = retireProcessGroup(p.pid, p.kill)
			p.groupMu.Lock()
			p.groupRetired = true
			p.groupMu.Unlock()
		}
		err := cmd.Wait()
		r := processExit{Err: err}
		if cmd.ProcessState != nil {
			code := cmd.ProcessState.ExitCode()
			r.ExitCode = &code
			r.Reaped = true
		}
		p.exit = r
		close(p.exited)
	}()
	return p, nil
}
func (p *process) result() processExit {
	<-p.exited
	r := p.exit
	if r.ExitCode != nil {
		v := *r.ExitCode
		r.ExitCode = &v
	}
	return r
}
func (p *process) closeStdin() error {
	p.stdinOnce.Do(func() { p.stdinErr = p.stdin.Close() })
	return p.stdinErr
}
func (p *process) closeReaders() error {
	p.readersOnce.Do(func() { p.readersErr = errors.Join(p.stdout.Close(), p.stderr.Close()) })
	return p.readersErr
}
func (p *process) stop(grace, reapTimeout time.Duration) (processExit, bool, error) {
	p.stopOnce.Do(func() {
		p.stopErr = p.closeStdin()
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-p.exited:
			p.stopExit = p.result()
		case <-timer.C:
			p.forced = true
			p.stopErr = errors.Join(p.stopErr, ErrShutdownTimeout)
			if err := p.kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				p.stopErr = errors.Join(p.stopErr, newError("cleanup", NotSent, err))
			}
			reaper := time.NewTimer(reapTimeout)
			defer reaper.Stop()
			select {
			case <-p.exited:
				p.stopExit = p.result()
			case <-reaper.C:
				p.stopErr = errors.Join(p.stopErr, ErrCleanup)
			}
		}
		if p.stopExit.Reaped == false {
			p.stopErr = errors.Join(p.stopErr, ErrCleanup)
		}
		if p.ownedGroup && p.stopExit.Reaped {
			p.stopErr = errors.Join(p.stopErr, p.groupErr)
		}
	})
	r := p.stopExit
	if r.ExitCode != nil {
		v := *r.ExitCode
		r.ExitCode = &v
	}
	return r, p.forced, p.stopErr
}
