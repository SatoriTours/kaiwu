//go:build linux

package gitops

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProcessGroupCleanupRetainsLeaderIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & printf started")
	var out bytes.Buffer
	cmd.Stdout = &out
	setupProcess(cmd)
	kill := cmd.Cancel
	retained := false
	cmd.Cancel = func() error {
		b, e := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/stat")
		retained = e == nil && strings.Fields(strings.SplitN(string(b), ") ", 2)[1])[0] == "Z"
		return kill()
	}
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	if e := waitProcess(cmd); e != nil {
		t.Fatal(e)
	}
	if !retained {
		t.Fatal("group cleanup ran after leader identity was reaped")
	}
	if e := kill(); e != os.ErrProcessDone {
		t.Fatal("late cleanup could signal reused group", e)
	}
}
