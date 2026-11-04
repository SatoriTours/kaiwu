package execbridge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
)

type SessionConfig struct {
	SchemaVersion int
	Spec          executionenv.Spec
	SpoolBytes    int64
}

// Attestor is a trusted native backend, never data from config/HTTP. It must
// honor ctx and verify actual mounts/namespaces/budgets inside this invocation.
// There is deliberately no default implementation or host-execution fallback.
type Attestor interface {
	Attest(context.Context, executionenv.Spec, executionenv.Identity) (executionenv.Capabilities, error)
}
type SessionState struct {
	Attempted, Executed, Exited, Gap bool
	ExitCode                         int
	Last                             uint64
}
type Session struct {
	mu                                  sync.Mutex
	bootstrap                           Bootstrap
	identity                            executionenv.Identity
	config                              SessionConfig
	bundle                              *Bundle
	spool                               *Spool
	channel                             *BridgeChannel
	epoch, inputSequence                uint64
	attempted, executed, exited, closed bool
	exitCode                            int
	cmd                                 *exec.Cmd
	stdin                               *os.File
	readers                             []*os.File
	finished                            chan struct{}
	finishOnce                          sync.Once
	pumps                               sync.WaitGroup
	gap                                 atomic.Bool
}

// PrepareSession consumes bundle ownership, including on failure. It is usable
// only in a hardened, attested bridge, not as a generic host command runner.
// The factory is not wired into the server or a CLI entry point yet.
func PrepareSession(ctx context.Context, c *BridgeChannel, id executionenv.Identity, b *Bundle, w BundleExpectation, a Attestor) (out *Session, err error) {
	if b == nil {
		return nil, executionenv.ErrUnavailable
	}
	defer func() {
		if out == nil {
			b.Close()
		}
	}()
	if runtime.GOOS != "linux" || c == nil || c.closed.Load() || !validBootstrap(c.bootstrap) || id.Validate(true) != nil || !peerMatches(c.bootstrap, id) || a == nil {
		return nil, executionenv.ErrUnavailable
	}
	if validateBundle(b.Files, w) != nil {
		return nil, ErrBundle
	}
	info, err := b.Files[0].Stat()
	if err != nil {
		return nil, ErrBundle
	}
	data := make([]byte, int(info.Size()))
	if _, err = b.Files[0].ReadAt(data, 0); err != nil {
		return nil, ErrBundle
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != c.bootstrap.SpecHash {
		return nil, ErrBundle
	}
	cfg, err := decodeSessionConfig(data)
	if err != nil {
		return nil, err
	}
	if cfg.SchemaVersion != 1 || cfg.Spec.Mode != executionenv.ManagedSandbox || cfg.Spec.Kind != "pi" || cfg.SpoolBytes < 128 || cfg.SpoolBytes > maxSpoolBytes || cfg.SpoolBytes > cfg.Spec.Budget.DiskBytes || w.HasLease != (cfg.Spec.Owner.TaskID != "") {
		return nil, executionenv.ErrInvalid
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	copySpec := cfg.Spec
	copySpec.Args = append([]string(nil), cfg.Spec.Args...)
	copySpec.Env = append([]string{}, cfg.Spec.Env...)
	copySpec.Mounts = append([]executionenv.MountGrant(nil), cfg.Spec.Mounts...)
	caps, err := a.Attest(checkCtx, copySpec, id)
	if err != nil || checkCtx.Err() != nil {
		return nil, executionenv.ErrUnavailable
	}
	if err := executionenv.Check(cfg.Spec, caps); err != nil {
		return nil, err
	}
	file := b.Files[len(b.Files)-1]
	stat, err := file.Stat()
	if err != nil || stat.Size() != 0 {
		return nil, ErrGap
	} // never revive a dead bridge from a spool alone
	spool, err := OpenSpool(file, id.RunID, cfg.SpoolBytes)
	if err != nil {
		return nil, err
	}
	if b.Files[0].Close() != nil {
		return nil, ErrBundle
	}
	bootstrap := c.bootstrap
	bootstrap.HostPublicKey = append(ed25519.PublicKey(nil), bootstrap.HostPublicKey...)
	return &Session{bootstrap: bootstrap, identity: id, config: cfg, bundle: b, spool: spool, exitCode: -1, finished: make(chan struct{})}, nil
}
func (s *Session) authorized(c *BridgeChannel, epoch uint64) bool {
	return !s.closed && c != nil && !c.closed.Load() && s.channel == c && epoch != 0 && epoch == s.epoch
}
func (s *Session) Attach(c *BridgeChannel) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || c == nil || c.closed.Load() || !validBootstrap(c.bootstrap) || c.bootstrap.Identity != s.bootstrap.Identity || c.bootstrap.SpecHash != s.bootstrap.SpecHash || !bytes.Equal(c.bootstrap.HostPublicKey, s.bootstrap.HostPublicKey) || s.epoch == math.MaxUint64 {
		return 0, executionenv.ErrConflict
	}
	next := s.epoch + 1
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], next)
	if _, err := s.spool.Append(Attached, b[:]); err != nil {
		s.markGap()
	} // a gap must not prevent authenticated status/lease recovery
	if s.channel != nil && s.channel != c {
		s.channel.Detach()
	}
	s.channel = c
	s.epoch = next
	s.inputSequence = 0
	return next, nil
}
func (s *Session) Commit(ctx context.Context, c *BridgeChannel, epoch, sequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authorized(c, epoch) || sequence != 1 {
		return executionenv.ErrConflict
	}
	if s.gap.Load() {
		return ErrGap
	}
	if s.attempted {
		if s.executed {
			return nil
		}
		return executionenv.ErrUnavailable
	}
	if ctx.Err() != nil {
		return executionenv.ErrUnavailable
	}
	s.attempted = true
	if _, err := s.spool.Append(LaunchAttempt, nil); err != nil {
		s.markGap()
		s.finishOnce.Do(func() { close(s.finished) })
		return ErrGap
	}
	var all []*os.File
	pipe := func() (*os.File, *os.File, error) {
		r, w, err := os.Pipe()
		if err == nil {
			all = append(all, r, w)
		}
		return r, w, err
	}
	fail := func() error {
		for _, f := range all {
			f.Close()
		}
		if _, err := s.spool.Append(StartFailed, nil); err != nil {
			s.markGap()
		}
		s.finishOnce.Do(func() { close(s.finished) })
		return executionenv.ErrUnavailable
	}
	inR, inW, err := pipe()
	if err != nil {
		return fail()
	}
	outR, outW, err := pipe()
	if err != nil {
		return fail()
	}
	errR, errW, err := pipe()
	if err != nil {
		return fail()
	}
	// The connection/request context deliberately does NOT own this child. It
	// survives detach; the native unit controller is responsible for whole-group stop.
	cmd := exec.Command(s.config.Spec.Program, append([]string(nil), s.config.Spec.Args...)...)
	cmd.Dir = s.config.Spec.Dir
	cmd.Env = append([]string{}, s.config.Spec.Env...)
	cmd.Stdin = inR
	cmd.Stdout = outW
	cmd.Stderr = errW
	s.cmd = cmd
	if cmd.Start() != nil {
		return fail()
	}
	inR.Close()
	outW.Close()
	errW.Close()
	s.stdin = inW
	s.readers = []*os.File{outR, errR}
	s.executed = true
	s.pumps.Add(2)
	go s.pump(outR, Stdout)
	go s.pump(errR, Stderr)
	if _, err := s.spool.Append(Started, nil); err != nil {
		s.markGap()
	}
	go func() {
		_ = cmd.Wait()
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		s.mu.Lock()
		s.exited = true
		s.exitCode = code
		s.mu.Unlock()
		s.pumps.Wait()
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(int32(code)))
		if _, err := s.spool.Append(ProcessExit, b[:]); err != nil {
			s.markGap()
		}
		s.finishOnce.Do(func() { close(s.finished) })
	}()
	return nil
}
func (s *Session) pump(f *os.File, kind FrameKind) {
	defer s.pumps.Done()
	defer f.Close()
	b := make([]byte, 4096)
	for {
		n, err := f.Read(b)
		if n > 0 {
			if _, e := s.spool.Append(kind, b[:n]); e != nil {
				s.markGap()
			}
		}
		if err != nil {
			if err != io.EOF {
				s.markGap()
			}
			return
		}
	}
}
func (s *Session) Input(ctx context.Context, c *BridgeChannel, epoch, sequence uint64, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.authorized(c, epoch) || !s.executed || s.exited || sequence == 0 || sequence != s.inputSequence+1 || len(data) == 0 || len(data) > maxFrameData {
		return executionenv.ErrConflict
	}
	if s.gap.Load() {
		return ErrGap
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return executionenv.ErrUnavailable
	}
	// Reserve the input ID durably before writing any byte. Neither a partial
	// write nor a lost response permits replay of this ID. No prompt body is logged.
	s.inputSequence = sequence
	var ticket [16]byte
	binary.BigEndian.PutUint64(ticket[:8], epoch)
	binary.BigEndian.PutUint64(ticket[8:], sequence)
	if _, err := s.spool.Append(InputAttempt, ticket[:]); err != nil {
		s.markGap()
		return ErrGap
	}
	end, _ := ctx.Deadline()
	if s.stdin.SetWriteDeadline(end) != nil {
		s.markGap()
		return ErrGap
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { s.stdin.SetWriteDeadline(time.Now()); close(done) })
	defer func() {
		if !stop() {
			<-done
		}
		s.stdin.SetWriteDeadline(time.Time{})
	}()
	if writeFrame(s.stdin, data) != nil {
		s.markGap()
		return ErrGap
	}
	if _, err := s.spool.Append(InputReceipt, ticket[:]); err != nil {
		s.markGap()
		return ErrGap
	}
	return nil
}
func (s *Session) Events(ctx context.Context, after uint64, wait bool) ([]Frame, error) {
	if wait {
		if err := s.spool.WaitAfter(ctx, after); err != nil {
			return nil, err
		}
	}
	return s.spool.ReadAfter(after, 128, 1<<20)
}
func (s *Session) State() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, gap := s.spool.Last()
	return SessionState{Attempted: s.attempted, Executed: s.executed, Exited: s.exited, Gap: gap || s.gap.Load(), ExitCode: s.exitCode, Last: last}
}
func (s *Session) Wait(ctx context.Context) (SessionState, error) {
	select {
	case <-ctx.Done():
		return s.State(), ctx.Err()
	case <-s.finished:
		state := s.State()
		if state.Gap {
			return state, ErrGap
		}
		if !state.Exited {
			return state, executionenv.ErrUnavailable
		}
		return state, nil
	}
}

// Direct child Wait, EOF, or detach never releases the lease. Only the trusted
// controller's exact full-identity, whole-cgroup gone proof may release it.
func (s *Session) CloseAfterGroupGone(proof executionenv.Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.identity.Validate(true) != nil || proof.Identity != s.identity || proof.State != executionenv.OwnedGone || proof.GroupPopulated {
		return executionenv.ErrConflict
	}
	if !s.closed {
		s.closed = true
		s.finishOnce.Do(func() { close(s.finished) })
		if s.channel != nil {
			s.channel.Detach()
		}
		if s.stdin != nil {
			s.stdin.Close()
		}
		for _, r := range s.readers {
			r.Close()
		}
		s.bundle.Close()
	}
	return nil
}

func (s *Session) markGap() { s.gap.Store(true); s.spool.Fail() }
