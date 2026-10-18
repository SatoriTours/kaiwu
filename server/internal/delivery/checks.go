// Package delivery executes the fixed automated delivery loop: local checks,
// candidate commits, precise pushes and CI observation.
package delivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/agent"
	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// checkCommandCap bounds one check invocation regardless of configured
// timeout. Runtime caps are deliberately package vars so tests can shorten
// them; production keeps the embedded defaults.
var (
	checkCommandCap = 300 * time.Second
	checkOutputCap  = 8 << 20
	checkSummaryCap = 256 << 10
)

// CheckEvidence is the durable local check record. Fingerprint/Tree bind the
// evidence to one exact workspace state; a later commit must match both.
type CheckEvidence struct {
	Fingerprint string
	Tree        string
	Summary     string
	Passed      bool
	Truncated   bool
}

// checkEnv is the whitelist baseline for check children: no credential, Git
// or agent-specific variables ever reach a check process.
var checkEnvAllow = map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true}

func checkEnv() []string {
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if checkEnvAllow[key] {
			env = append(env, v)
		}
	}
	return append(env, "LC_ALL=C", "TMPDIR="+os.TempDir())
}

func validateCheck(check workbench.DeliveryCheck) error {
	if check.ID == "" || len(check.Argv) == 0 || strings.TrimSpace(check.Argv[0]) == "" || check.TimeoutSeconds <= 0 {
		return fmt.Errorf("检查 %q 配置无效", check.ID)
	}
	if check.Dir != "" && (!filepath.IsLocal(check.Dir) || strings.ContainsAny(check.Dir, "\\\x00")) {
		return fmt.Errorf("检查 %q 目录必须位于任务工作区内", check.ID)
	}
	return nil
}

// checkDir resolves one check working directory inside the canonical
// workspace; symlinks must not point outside it.
func checkDir(ws string, check workbench.DeliveryCheck) (string, error) {
	d := check.Dir
	if d == "" {
		d = "."
	}
	if !filepath.IsLocal(d) {
		return "", fmt.Errorf("检查 %q 目录越界", check.ID)
	}
	resolved, e := filepath.EvalSymlinks(filepath.Join(ws, d))
	if e != nil {
		return "", fmt.Errorf("检查目录 %q 不可用", check.Dir)
	}
	rel, e := filepath.Rel(ws, resolved)
	if e != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return "", fmt.Errorf("检查目录 %q 越出任务工作区", check.Dir)
	}
	if info, e := os.Stat(resolved); e != nil || !info.IsDir() {
		return "", fmt.Errorf("检查目录 %q 不是目录", check.Dir)
	}
	return resolved, nil
}

type cappedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > checkOutputCap {
		b.truncated = true
		return 0, fmt.Errorf("检查输出超过 %.1f MiB 上限", float64(checkOutputCap)/1024/1024)
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string { return b.buf.String() }

// errLeaseRefused aborts RunChecks outright: a writer may never run alongside
// an unconfirmed owner, and callers distinguish it from a check failure.
var errLeaseRefused = fmt.Errorf("写入租约拒绝启动检查")

func leaseRefused(err error) bool {
	return err != nil && errors.Is(err, errLeaseRefused)
}

// runOne starts one argv check inside dir. Lease attempt/started happen
// around the launch window exactly like Git integration; the process group
// is reaped (including descendants) before evidence exists.
func runOne(ctx context.Context, dir string, check workbench.DeliveryCheck, lease agent.DeliveryLease) (string, bool, error) {
	if lease != nil {
		if e := lease.Attempt(); e != nil {
			return "", false, fmt.Errorf("检查 %q 被拒绝: %w: %w", check.ID, errLeaseRefused, e)
		}
	}
	budget := time.Duration(check.TimeoutSeconds) * time.Second
	if budget > checkCommandCap {
		budget = checkCommandCap
	}
	cctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	cmd := exec.CommandContext(cctx, check.Argv[0], check.Argv[1:]...)
	cmd.Dir = dir
	cmd.Env = checkEnv()
	cmd.WaitDelay = time.Second
	out := &cappedBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	gitops.StartGroup(cmd)
	if e := cmd.Start(); e != nil {
		gitops.KillGroup(cmd)
		return out.String(), out.truncated, fmt.Errorf("检查 %q 无法启动: %w", check.ID, e)
	}
	if lease != nil {
		if e := lease.Started(cmd.Process.Pid); e != nil {
			gitops.KillGroup(cmd)
			_ = gitops.WaitGroup(cmd)
			return out.String(), out.truncated, fmt.Errorf("检查 %q 被拒绝: %w: %w", check.ID, errLeaseRefused, e)
		}
	}
	e := gitops.WaitGroup(cmd)
	if e == nil {
		return out.String(), out.truncated, nil
	}
	if cctx.Err() != nil {
		return out.String(), out.truncated, fmt.Errorf("检查 %q 超时（预算 %s）", check.ID, budget)
	}
	if out.truncated {
		return out.String(), out.truncated, fmt.Errorf("检查 %q 输出超限: %w", check.ID, e)
	}
	return out.String(), false, fmt.Errorf("检查 %q 退出码非零: %w", check.ID, e)
}

func clampSummary(b *bytes.Buffer) string {
	if b.Len() <= checkSummaryCap {
		return b.String()
	}
	data := b.Bytes()
	return string(data[len(data)-checkSummaryCap:])
}

func appendSummary(b *bytes.Buffer, format string, args ...any) {
	fmt.Fprintf(b, format+"\n", args...)
}

// RunChecks executes every configured check once, argv-direct with bounded
// resources, then re-verifies the candidate fingerprint: unknown workspace
// changes invalidate the evidence instead of silently joining the commit.
func RunChecks(ctx context.Context, x workbench.Execution, checks []workbench.DeliveryCheck, lease agent.DeliveryLease) (CheckEvidence, error) {
	ev := CheckEvidence{}
	if x.Workspace == "" {
		return ev, fmt.Errorf("任务执行缺少工作区")
	}
	ws, e := filepath.EvalSymlinks(x.Workspace)
	if e != nil {
		return ev, fmt.Errorf("任务工作区不可用: %w", e)
	}
	if info, e := os.Stat(ws); e != nil || !info.IsDir() {
		return ev, fmt.Errorf("任务工作区不可用")
	}
	before, e := gitops.CaptureWorkspaceSnapshot(ctx, ws, nil)
	if e != nil {
		return ev, fmt.Errorf("候选快照不可读: %w", e)
	}
	summary := &bytes.Buffer{}
	passed, truncated := true, false
	for _, check := range checks {
		if e = validateCheck(check); e != nil {
			return ev, e
		}
		dir, e := checkDir(ws, check)
		if e != nil {
			return ev, e
		}
		out, childTrunc, err := runOne(ctx, dir, check, lease)
		if leaseRefused(err) {
			return ev, err
		}
		if childTrunc {
			truncated = true
		}
		if err != nil {
			passed = false
			appendSummary(summary, "检查 %s 失败:%v\n%s", check.ID, err, out)
		} else {
			appendSummary(summary, "检查 %s 通过:%s", check.ID, out)
		}
	}
	after, e := gitops.CaptureWorkspaceSnapshot(ctx, ws, nil)
	if e != nil {
		return ev, fmt.Errorf("候选快照不可读: %w", e)
	}
	if after.Fingerprint != before.Fingerprint {
		passed = false
		appendSummary(summary, "候选内容在检查期间变化，本地证据失效")
	}
	if truncated {
		passed = false
		appendSummary(summary, "检查输出超过上限，证据不完整")
	}
	ev = CheckEvidence{Fingerprint: after.Fingerprint, Tree: after.Tree, Summary: clampSummary(summary), Passed: passed, Truncated: truncated}
	return ev, nil
}
