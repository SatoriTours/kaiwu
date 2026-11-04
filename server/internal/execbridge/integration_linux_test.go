//go:build linux

package execbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/executionenv"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"golang.org/x/sys/unix"
)

type hostDeathConfig struct {
	Bootstrap Bootstrap
	Key       []byte
	Identity  executionenv.Identity
	Expected  BundleExpectation
	Database  string
}

// This subprocess is a synthetic host, not a model or systemd unit. Descriptor
// 3/4 are direct test-only ExtraFiles, never assumed to cross a manager.
func TestBridgeHostDeathHelper(t *testing.T) {
	if os.Getenv("LANG") != "kaiwu-bridge-host-helper-v1" {
		return
	}
	fail := func() { fmt.Fprintln(os.Stderr, "synthetic host failed"); os.Exit(31) }
	var cfg hostDeathConfig
	metadata := os.NewFile(3, "synthetic-host-metadata")
	if json.NewDecoder(metadata).Decode(&cfg) != nil {
		fail()
	}
	metadata.Close()
	socket := os.NewFile(4, "synthetic-control")
	conn, err := net.FileConn(socket)
	socket.Close()
	if err != nil {
		fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h, err := AuthenticateHost(ctx, conn, cfg.Bootstrap, cfg.Key, fixedVerifier{&fixedPeer{identity: cfg.Identity}})
	if err != nil {
		fail()
	}
	endpoint, err := OpenRemote(ctx, h, func(context.Context) (executionenv.Observation, error) {
		return executionenv.Observation{Identity: cfg.Identity, State: executionenv.OwnedRunning, GroupPopulated: true}, nil
	})
	if err != nil {
		fail()
	}
	retained, err := endpoint.RecoverFiles(ctx, cfg.Expected)
	if err != nil {
		fail()
	}
	_ = retained // deliberately no Close/defer before process death
	journal, err := workbench.Open(cfg.Database, workbench.Profile{})
	if err != nil {
		fail()
	}
	row, err := CommitPrepared(ctx, journal, cfg.Identity.RunID, endpoint)
	if err != nil || row.Phase != executionenv.Running {
		fail()
	}
	if endpoint.Input(ctx, 1, []byte("host-before-death\n")) != nil {
		fail()
	}
	// Abrupt process exit skips all journal/socket/lease defer cleanup. Kernel
	// closes this actual host process's descriptors; the bridge must retain its OFD.
	os.Exit(0)
}
func TestLeaseSurvivesHostDeathWithSQLiteAndRealChild(t *testing.T) {
	f := newSessionFixture(t, 32768, "echo", nil)
	f.host.Detach()
	f.bridge.Detach()
	f.files[1].Close()
	db := filepath.Join(t.TempDir(), "runtime.db")
	journal, err := workbench.Open(db, workbench.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if err = journal.Update(func(st *workbench.State) error {
		st.Projects["p"] = workbench.Project{ID: "p", StorageMode: "managed"}
		st.Conversations["c"] = workbench.Conversation{ID: "c", ProjectID: "p", TaskID: "t", SessionID: "same-session", SessionFile: "same.jsonl", Messages: []workbench.Message{{ID: "m", Text: "keep"}}}
		st.Tasks["t"] = workbench.Task{ID: "t", ProjectID: "p", ConversationID: "c", ExecutionID: "e"}
		st.Executions["e"] = workbench.Execution{ID: "e", ConversationID: "c", TaskID: "t"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	row := executionenv.Record{Identity: f.bootstrap.Identity, Owner: f.config.Spec.Owner, Kind: "pi", SpecHash: f.bootstrap.SpecHash, Budget: f.config.Spec.Budget, Writer: true, Phase: executionenv.Starting}
	row, err = journal.ReserveRuntime(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	row.Identity = f.identity
	row.RendezvousInode = 1
	row.Phase = executionenv.Prepared
	row, err = journal.AdvanceRuntime(ctx, row, nil)
	if err != nil {
		t.Fatal(err)
	}
	left, right := unixPair(t)
	socket, err := left.File()
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	metadata, err := os.CreateTemp(t.TempDir(), "synthetic-host")
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	if err = json.NewEncoder(metadata).Encode(hostDeathConfig{Bootstrap: f.bootstrap, Key: f.key, Identity: f.identity, Expected: f.expect, Database: db}); err != nil {
		t.Fatal(err)
	}
	metadata.Seek(0, 0)
	bridgeDone := make(chan error, 1)
	go func() {
		bridge, err := AuthenticateBridge(ctx, right, f.bootstrap)
		if err == nil {
			err = f.s.Serve(ctx, bridge)
		}
		bridgeDone <- err
	}()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, program, "-test.run=^TestBridgeHostDeathHelper$")
	cmd.Env = []string{"LANG=kaiwu-bridge-host-helper-v1"}
	cmd.ExtraFiles = []*os.File{metadata, socket}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	socket.Close()
	left.Close() // no surviving host-side copy in the test parent
	if err = cmd.Wait(); err != nil {
		t.Fatalf("host fixture: %v %s", err, output.String())
	}
	select {
	case err := <-bridgeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("host death did not detach bridge")
	}
	state, err := journal.Runtime(ctx, f.identity.RunID)
	if err != nil || state.Phase != executionenv.Running {
		t.Fatal("durable launch receipt lost")
	}
	lock, err := os.OpenFile(f.files[1].Name(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if lockErr(lock) == nil {
		t.Fatal("host death released bridge lease")
	}
	host, bridge := sessionPair(t, f.bootstrap, f.key, f.identity)
	remote, done := serveFixture(t, f, host, bridge)
	recovered, err := RecoverLaunch(ctx, journal, f.identity.RunID, remote)
	if err != nil || recovered.Phase != executionenv.Running {
		t.Fatal("query-only recovery failed")
	}
	if err = remote.Input(ctx, 1, []byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(f.marker)
	if err != nil || string(content) != "spawn\nhost-before-death\nexit\n" {
		t.Fatalf("host death replay/EOF: %q %v", content, err)
	}
	st := journal.Snapshot()
	if c := st.Conversations["c"]; c.SessionID != "same-session" || c.SessionFile != "same.jsonl" || len(c.Messages) != 1 {
		t.Fatal("recovery rebound business session")
	}
	remote.Detach()
	<-done
}

func lockErr(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
