// Package gitops checks local Git facts and integrates explicitly approved work.
package gitops

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Runner func(context.Context, string, ...string) (string, error)

func Args(dir string, args ...string) []string {
	return append([]string{"-C", dir, "--no-pager", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.attributesFile=" + os.DevNull, "-c", "commit.gpgSign=false", "-c", "merge.gpgSign=false", "-c", "merge.verifySignatures=false", "-c", "log.showSignature=false", "-c", "merge.autoStash=false", "-c", "rerere.enabled=false", "-c", "core.quotePath=false"}, args...)
}
func Env() []string {
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			env = append(env, v)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_MERGE_AUTOEDIT=no", "GIT_LITERAL_PATHSPECS=1", "LC_ALL=C")
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8<<20 {
		return 0, errors.New("Git 输出超过安全上限")
	}
	return b.Buffer.Write(p)
}
func Run(ctx context.Context, dir string, track func(int) error, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", Args(dir, args...)...)
	cmd.Env = Env()
	cmd.WaitDelay = time.Second
	out := &limitedBuffer{}
	cmd.Stdout = out
	cmd.Stderr = &limitedBuffer{}
	setupProcess(cmd)
	if e := cmd.Start(); e != nil {
		return "", e
	}
	if track != nil {
		if e := track(cmd.Process.Pid); e != nil {
			killProcess(cmd)
			_ = waitProcess(cmd)
			return "", e
		}
	}
	e := waitProcess(cmd)
	return out.String(), e
}
func output(ctx context.Context, dir string, args ...string) (string, error) {
	s, e := Run(ctx, dir, nil, args...)
	return strings.TrimSpace(s), e
}
