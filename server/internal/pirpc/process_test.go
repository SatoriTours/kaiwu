package pirpc

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func processForTest(t *testing.T, s string) *process {
	t.Helper()
	p, err := startProcess(helperLaunch(t, s))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _, _ = p.stop(100*time.Millisecond, time.Second)
		_ = p.closeReaders()
		select {
		case <-p.exited:
		case <-time.After(3 * time.Second):
			t.Error("helper not reaped")
		}
	})
	return p
}
func awaitProcess(t *testing.T, p *process) {
	t.Helper()
	select {
	case <-p.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("process timeout")
	}
}
func TestProcessLaunchFailure(t *testing.T) {
	s := helperLaunch(t, "eof_exit")
	s.Program = s.Dir + "/missing"
	if p, err := startProcess(s); p != nil || !errors.Is(err, ErrIO) {
		t.Fatalf("p=%v err=%v", p, err)
	}
}
func TestProcessExplicitEnvAndDir(t *testing.T) {
	t.Setenv("KAIWU_TEST_SECRET", "synthetic-parent-secret")
	s := helperLaunch(t, "env_dir")
	p, err := startProcess(s)
	if err != nil {
		t.Fatal(err)
	}
	defer p.closeReaders()
	defer p.closeStdin()
	awaitProcess(t, p)
	data, err := io.ReadAll(p.stdout)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if json.Unmarshal(data, &got) != nil || got["cwd"] != s.Dir || got["secret"] != "" {
		t.Fatalf("env/dir: %s", data)
	}
}
func TestProcessGracefulStop(t *testing.T) {
	p := processForTest(t, "eof_exit")
	r, forced, err := p.stop(2*time.Second, time.Second)
	if err != nil || forced || !r.Reaped || r.ExitCode == nil || *r.ExitCode != 0 {
		t.Fatalf("%+v %v %v", r, forced, err)
	}
}
func TestProcessForcedStop(t *testing.T) {
	p := processForTest(t, "ignore_eof")
	r, forced, err := p.stop(100*time.Millisecond, time.Second)
	if !forced || !r.Reaped || !errors.Is(err, ErrShutdownTimeout) {
		t.Fatalf("%+v %v %v", r, forced, err)
	}
}
func TestProcessWaitKeepsOutput(t *testing.T) {
	p := processForTest(t, "last_output")
	awaitProcess(t, p)
	out, err := io.ReadAll(p.stdout)
	if err != nil || string(out) != "last-out" {
		t.Fatalf("%q %v", out, err)
	}
	e, err := io.ReadAll(p.stderr)
	if err != nil || string(e) != "last-err" {
		t.Fatalf("%q %v", e, err)
	}
}
func TestProcessStopIdempotent(t *testing.T) {
	p := processForTest(t, "eof_exit")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, f, err := p.stop(2*time.Second, time.Second)
			if err != nil || f || !r.Reaped {
				t.Errorf("%+v %v %v", r, f, err)
			}
		}()
	}
	wg.Wait()
	if err := p.closeReaders(); err != nil {
		t.Fatal(err)
	}
	if err := p.closeReaders(); err != nil {
		t.Fatal(err)
	}
}
