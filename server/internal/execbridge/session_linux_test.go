//go:build linux

package execbridge

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"golang.org/x/sys/unix"
)

// Only this explicitly selected test helper executes in the child. No shell,
// pi, credentials, model, network listener, or real systemd unit is involved.
func TestBridgeChildHelper(t *testing.T) {
	if os.Getenv("LANG") != "kaiwu-bridge-test-helper-v1" {
		return
	}
	args := os.Args
	at := -1
	for i, v := range args {
		if v == "--" {
			at = i
			break
		}
	}
	if at < 0 || at+2 >= len(args) {
		os.Exit(21)
	}
	marker, mode := args[at+1], args[at+2]
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(22)
	}
	defer f.Close()
	// Private lease/spool/config descriptors and ambient credentials must not
	// reach the task. Only explicit stdio survives the actual exec boundary.
	if os.Getenv("KAIWU_SYNTHETIC_PARENT_SECRET") != "" {
		os.Exit(24)
	}
	entries, _ := os.ReadDir("/proc/self/fd")
	for _, entry := range entries {
		target, _ := os.Readlink("/proc/self/fd/" + entry.Name())
		base := filepath.Base(target)
		if strings.HasPrefix(base, "lease") || strings.HasPrefix(base, "spool") || strings.Contains(target, "memfd:kaiwu") {
			os.Exit(25)
		}
	}
	fmt.Fprintln(f, "spawn")
	fmt.Fprintln(os.Stdout, "ready")
	fmt.Fprintln(os.Stderr, "helper-stderr")
	if mode == "overflow" {
		fmt.Fprint(os.Stdout, strings.Repeat("x", 32768))
		return
	}
	if mode == "block" {
		time.Sleep(10 * time.Second)
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		line := scan.Text()
		fmt.Fprintln(f, line)
		fmt.Fprintln(os.Stdout, "echo:"+line)
		if line == "exit" {
			return
		}
	}
	fmt.Fprintln(f, "UNEXPECTED_EOF")
}

type syntheticAttestor struct{ caps executionenv.Capabilities }

func (a syntheticAttestor) Attest(context.Context, executionenv.Spec, executionenv.Identity) (executionenv.Capabilities, error) {
	return a.caps, nil
}

type sessionFixture struct {
	s         *Session
	host      *HostChannel
	bridge    *BridgeChannel
	epoch     uint64
	files     []*os.File
	expect    BundleExpectation
	marker    string
	config    SessionConfig
	bootstrap Bootstrap
	key       []byte
	identity  executionenv.Identity
}

func newSessionFixture(t *testing.T, limit int64, mode string, mutate func(*SessionConfig, *executionenv.Capabilities)) *sessionFixture {
	t.Helper()
	t.Setenv("KAIWU_SYNTHETIC_PARENT_SECRET", "synthetic-do-not-inherit")
	b, key, id := authFixture(t)
	files, want := descriptorFixture(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec := executionenv.Spec{Mode: executionenv.ManagedSandbox, Owner: executionenv.Owner{ProjectID: "p", ConversationID: "c", TaskID: "t", ExecutionID: "e"}, Kind: "pi", Program: program, Dir: dir, ProjectRoot: dir, Args: []string{"-test.run=^TestBridgeChildHelper$", "--", marker, mode}, Env: []string{"HOME=" + dir, "TMPDIR=" + dir, "LANG=kaiwu-bridge-test-helper-v1"}, Budget: executionenv.Budget{CPUQuotaUSec: 10000, MemoryBytes: 64 << 20, PidsMax: 32, DiskBytes: 1 << 20}, Network: executionenv.NetworkGrant{Mode: "deny"}}
	cfg := SessionConfig{SchemaVersion: 1, Spec: spec, SpoolBytes: limit}
	caps := executionenv.Capabilities{Platform: "linux", EvidenceID: "SYNTHETIC-local-test-not-isolation", PrivateIPC: true, SocketFilter: true, UserManager: true, PrivateRoot: true, PrivateUsers: true, PrivatePID: true, PrivateNetwork: true, CPU: true, Memory: true, Pids: true, Disk: executionenv.DiskObservation{ProjectID: "p", Root: dir, EvidenceID: "SYNTHETIC-not-quota", HardLimit: 1 << 20, CoversAllWrites: true}}
	if mutate != nil {
		mutate(&cfg, &caps)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	b.SpecHash = hex.EncodeToString(sum[:])
	fd, err := unix.MemfdCreate("kaiwu-session-test", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	cf := os.NewFile(uintptr(fd), "config")
	t.Cleanup(func() { cf.Close() })
	if _, err = cf.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err = unix.FcntlInt(cf.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_SEAL); err != nil {
		t.Fatal(err)
	}
	files[0] = cf
	var stat unix.Stat_t
	unix.Fstat(fd, &stat)
	want.Config = FileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}
	host, bridge := sessionPair(t, b, key, id)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sent := make(chan error, 1)
	go func() { sent <- host.SendBundle(ctx, files, want) }()
	bundle, err := bridge.ReceiveBundle(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	s, err := PrepareSession(ctx, bridge, id, bundle, want, syntheticAttestor{caps})
	if err != nil {
		if mutate != nil {
			return &sessionFixture{files: files, marker: marker}
		}
		t.Fatal(err)
	}
	epoch, err := s.Attach(bridge)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &sessionFixture{s: s, host: host, bridge: bridge, epoch: epoch, files: files, expect: want, marker: marker, config: cfg, bootstrap: b, key: key, identity: id}
	t.Cleanup(func() { s.testCleanup(); host.Detach(); bridge.Detach() })
	return fixture
}
func sessionPair(t *testing.T, b Bootstrap, key []byte, id executionenv.Identity) (*HostChannel, *BridgeChannel) {
	t.Helper()
	left, right := unixPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan *BridgeChannel, 1)
	errs := make(chan error, 1)
	go func() { c, err := AuthenticateBridge(ctx, right, b); done <- c; errs <- err }()
	host, err := AuthenticateHost(ctx, left, b, key, fixedVerifier{&fixedPeer{identity: id}})
	if err != nil {
		t.Fatal(err)
	}
	bridge := <-done
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Detach(); bridge.Detach() })
	return host, bridge
}
func waitSessionText(t *testing.T, s *Session, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var after uint64
	var output strings.Builder
	for {
		frames, err := s.Events(ctx, after, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			after = frame.Sequence
			if frame.Kind == Stdout {
				output.Write(frame.Data)
			}
		}
		if strings.Contains(output.String(), text) {
			return
		}
	}
}
func TestBootstrapBeforeCommitAndMissingQuotaNeverExecutes(t *testing.T) {
	f := newSessionFixture(t, 16384, "echo", nil)
	if _, err := os.Stat(f.marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("prepared process executed before commit")
	}
	bad := newSessionFixture(t, 16384, "echo", func(_ *SessionConfig, c *executionenv.Capabilities) { c.Disk = executionenv.DiskObservation{} })
	if bad.s != nil {
		t.Fatal("missing quota attestation accepted")
	}
	if info, err := bad.files[2].Stat(); err != nil || info.Size() != 0 {
		t.Fatal("missing capability wrote spool before admission")
	}
	if _, err := os.Stat(bad.marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing capability launched host fallback")
	}
}
func TestAttachDoesNotReplayPromptOrSendPIEOF(t *testing.T) {
	f := newSessionFixture(t, 16384, "echo", nil)
	s := f.s
	if err := s.Commit(context.Background(), f.bridge, f.epoch, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(context.Background(), f.bridge, f.epoch, 1); err != nil {
		t.Fatal(err)
	}
	waitSessionText(t, s, "ready")
	if err := s.Input(context.Background(), f.bridge, f.epoch, 1, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	waitSessionText(t, s, "echo:first")
	if err := s.Input(context.Background(), f.bridge, f.epoch, 1, []byte("first\n")); !errors.Is(err, executionenv.ErrConflict) {
		t.Fatal("unconfirmed input resent")
	}
	f.host.Detach()
	f.bridge.Detach()
	f.files[1].Close()
	host, bridge := sessionPair(t, f.bootstrap, f.key, f.identity)
	defer host.Detach()
	defer bridge.Detach()
	epoch, err := s.Attach(bridge)
	if err != nil || epoch <= f.epoch {
		t.Fatal("new attachment has no epoch")
	}
	if err := s.Input(context.Background(), f.bridge, epoch, 1, []byte("stale\n")); !errors.Is(err, executionenv.ErrConflict) {
		t.Fatal("old connection used new epoch")
	}
	if err := s.Input(context.Background(), bridge, epoch, 1, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	waitSessionText(t, s, "echo:second")
	if err := s.Input(context.Background(), bridge, epoch, 2, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	state, err := s.Wait(ctx)
	if err != nil || !state.Exited || state.ExitCode != 0 || state.Gap {
		t.Fatalf("unexpected child state: %+v %v", state, err)
	}
	content, err := os.ReadFile(f.marker)
	if err != nil || string(content) != "spawn\nfirst\nsecond\nexit\n" {
		t.Fatalf("spawn/input/EOF replay: %q %v", content, err)
	}
	lock, err := os.OpenFile(f.files[1].Name(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != unix.EWOULDBLOCK {
		t.Fatal("child exit/detach released lease before group proof")
	}
	if s.CloseAfterGroupGone(executionenv.Observation{Identity: f.identity, State: executionenv.OwnedRunning, GroupPopulated: true}) == nil {
		t.Fatal("live group released lease")
	}
	if err := s.CloseAfterGroupGone(executionenv.Observation{Identity: f.identity, State: executionenv.OwnedGone}); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal("group proof failed to release last lease")
	}
}
func TestRealChildOverflowExitZeroIsNotSuccess(t *testing.T) {
	f := newSessionFixture(t, 256, "overflow", nil)
	if err := f.s.Commit(context.Background(), f.bridge, f.epoch, 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	state, err := f.s.Wait(ctx)
	if !errors.Is(err, ErrGap) || !state.Gap || !state.Exited || state.ExitCode != 0 {
		t.Fatalf("overflow invented clean success: %+v %v", state, err)
	}
	if info, err := f.files[2].Stat(); err != nil || info.Size() > 256 {
		t.Fatal("spool exceeded hard local byte bound")
	}
}

func (s *Session) testCleanup() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = s.Wait(ctx)
		cancel()
	}
	s.mu.Lock()
	if s.stdin != nil {
		s.stdin.Close()
	}
	for _, r := range s.readers {
		r.Close()
	}
	s.bundle.Close()
	s.mu.Unlock()
}

func TestInputTimeoutKeepsChildAndStickyGap(t *testing.T) {
	f := newSessionFixture(t, 16384, "block", nil)
	if err := f.s.Commit(context.Background(), f.bridge, f.epoch, 1); err != nil {
		t.Fatal(err)
	}
	waitSessionText(t, f.s, "ready")
	// Pin this fixture's private pipe capacity instead of assuming the host
	// default is smaller than one legal input frame. This changes no sysctl.
	f.s.mu.Lock()
	raw, pipeErr := f.s.stdin.SyscallConn()
	if pipeErr == nil {
		var controlErr error
		pipeErr = raw.Control(func(fd uintptr) { _, controlErr = unix.FcntlInt(fd, unix.F_SETPIPE_SZ, 4096) })
		if pipeErr == nil {
			pipeErr = controlErr
		}
	}
	f.s.mu.Unlock()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	// File.Fd would disable Go's pipe deadlines; SyscallConn preserves polling.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := f.s.Input(ctx, f.bridge, f.epoch, 1, []byte(strings.Repeat("x", 65536)))
	if time.Since(start) > time.Second {
		t.Fatal("stdin write escaped context deadline")
	}

	if !errors.Is(err, ErrGap) || !f.s.State().Gap {
		t.Fatalf("partial write invented receipt: %v", err)
	}
	if _, err := f.s.Events(context.Background(), 0, false); !errors.Is(err, ErrGap) {
		t.Fatal("partial input left clean completion stream")
	}
	if f.s.State().Exited {
		t.Fatal("input timeout killed child instead of detach/unknown")
	}
}
func TestSessionManifestRejectsAliasesAndUnknownSecrets(t *testing.T) {
	for _, data := range []string{`{"SchemaVersion":1,"schemaversion":2}`, `{"ſchemaVersion":1}`, `{"SchemaVersion":1,"Spec":{"Budget":{"MemoryBytes":1,"memorybytes":2}}}`, `{"SchemaVersion":1,"Secret":"never-reflect-this"}`, `{} {}`, `{"Spec":{"Env":[],"ENV":[]}}`} {
		_, err := decodeSessionConfig([]byte(data))
		if err == nil || strings.Contains(err.Error(), "never-reflect-this") {
			t.Fatal("invalid manifest accepted or reflected")
		}
	}
}
