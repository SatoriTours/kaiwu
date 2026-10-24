package gitops

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/githubauth"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// RemoteTarget is the frozen, validated remote identity of a delivery.
type RemoteTarget struct {
	RepositoryID int64
	URL          string
	MainRef      string
	TaskRef      string
}

var remoteURLAllowed = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9.-]+/[A-Za-z0-9_.-]+\.git$`)
var sshURLAllowed = regexp.MustCompile(`^git@github\.com:[A-Za-z0-9.-]+/[A-Za-z0-9_.-]+\.git$`)
var branchNameAllowed = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var taskIDAllowed = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ResolveRemote builds the frozen remote target from the authorized policy,
// re-verifying the project's remote configuration.
func ResolveRemote(ctx context.Context, project workbench.Project, policy workbench.ProjectDeliveryPolicy, taskID string) (RemoteTarget, error) {
	target := RemoteTarget{}
	repo := policy.Repository
	if repo.ID == 0 {
		return target, errors.New("交付授权缺少仓库标识")
	}
	url := repo.URL
	if !remoteURLAllowed.MatchString(url) && !sshURLAllowed.MatchString(url) {
		return target, fmt.Errorf("远程地址 %q 不是受支持的 github.com 地址", url)
	}
	if _, e := parsed(url); e != nil {
		return target, e
	}
	if !branchNameAllowed.MatchString(repo.MainBranch) || strings.Contains(repo.MainBranch, "..") {
		return target, fmt.Errorf("主分支 %q 不是受支持的名称", repo.MainBranch)
	}
	if !taskIDAllowed.MatchString(taskID) {
		return target, errors.New("任务标识不是受支持的格式")
	}
	if e := verifyRemoteConfig(ctx, project.Path, url); e != nil {
		return target, e
	}
	target.RepositoryID = repo.ID
	target.URL = url
	target.MainRef = repo.MainBranch
	target.TaskRef = "refs/heads/kaiwu/task-" + taskID
	return target, nil
}

func parsed(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil {
		return nil, errors.New("远程地址不得携带凭据")
	}
	return u, nil
}

// verifyRemoteConfig checks the project remote still matches the frozen URL:
// a single fetch URL, no extra push URLs, and no insteadOf rewrites.
func verifyRemoteConfig(ctx context.Context, path, frozen string) error {
	names, e := output(ctx, path, "config", "--get-regexp", `^remote\..*\.url$`)
	if e != nil && !strings.Contains(e.Error(), "no such section") {
		// no remotes configured is fine for fetch-by-URL flows, but the
		// frozen remote must exist.
		return errors.New("项目远程配置不可读")
	}
	match := ""
	for _, line := range strings.Split(names, "\n") {
		line = strings.TrimSpace(line)
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")
		if value == frozen {
			if match != "" && match != name {
				return errors.New("同一冻结地址配置在多个远程上")
			}
			match = name
		}
	}
	if match == "" {
		return errors.New("项目未配置与授权一致的远程")
	}
	pushURLs, e := output(ctx, path, "config", "--get-all", "remote."+match+".pushurl")
	if e == nil && strings.TrimSpace(pushURLs) != "" {
		for _, u := range strings.Split(pushURLs, "\n") {
			if strings.TrimSpace(u) != frozen {
				return errors.New("远程存在额定的 pushurl")
			}
		}
	}
	rewrites, e := output(ctx, path, "config", "--get-regexp", `^url\..*\.insteadof$`)
	if e == nil && strings.TrimSpace(rewrites) != "" {
		return errors.New("存在 URL rewrite 配置，远程地址不可信")
	}
	helper, e := output(ctx, path, "config", "--get-regexp", `^remote\..*\.vcs$`)
	if e == nil && strings.TrimSpace(helper) != "" {
		return errors.New("存在 remote helper 配置")
	}
	return rejectCredentialHelper(ctx, path)
}

// RemoteTransport executes one remote git command. Production uses the
// fixed git program with controlled credentials; tests inject fakes.
type RemoteTransport interface {
	Run(ctx context.Context, dir string, target RemoteTarget, args []string, credentialRef string, lease ProcessLease) (string, error)
}

type RemoteTransportFunc func(ctx context.Context, dir string, target RemoteTarget, args []string, credentialRef string, lease ProcessLease) (string, error)

func (f RemoteTransportFunc) Run(ctx context.Context, dir string, target RemoteTarget, args []string, credentialRef string, lease ProcessLease) (string, error) {
	return f(ctx, dir, target, args, credentialRef, lease)
}

// ProcessLease coordinates process-group ownership for remote operations.
type ProcessLease interface {
	File() *os.File
	Attempt() error
	Started(pid int) error
}

type RemoteConfig struct {
	Program  string
	Resolver *githubauth.Resolver
	// Transport is a test injection point; production assembly must leave
	// it nil so the fixed git implementation is used.
	Transport RemoteTransport
}

// RemoteRunner performs read-only observe, fetch and verified push against
// exactly one frozen remote target.
type RemoteRunner struct {
	program   string
	resolver  *githubauth.Resolver
	transport RemoteTransport
}

func NewRemoteRunner(cfg RemoteConfig) *RemoteRunner {
	program := cfg.Program
	if program == "" {
		program = "git"
	}
	return &RemoteRunner{program: program, resolver: cfg.Resolver, transport: cfg.Transport}
}

// networkTimeout bounds one remote git command.
const networkTimeout = 120 * time.Second

func (r *RemoteRunner) run(ctx context.Context, dir string, target RemoteTarget, args []string, credentialRef string, lease ProcessLease) (string, error) {
	if u, e := urlParse(target.URL); e != nil || (u.User != nil) {
		return "", errors.New("远程地址不得携带凭据")
	}
	if r.transport != nil {
		out, e := r.transport.Run(ctx, dir, target, args, credentialRef, lease)
		// A transport reports its own process fate through the lease. Close the
		// launch window only when the lease itself can prove nothing is running,
		// so an abstract runner can neither hide a live group nor lock the
		// workspace forever after a failed launch.
		return out, e
	}
	return r.productionRun(ctx, dir, target, args, credentialRef, lease)
}

func urlParse(raw string) (*url.URL, error) { return url.Parse(raw) }

// productionRun executes git with the frozen env, controlled credentials
// and the inherited lease file, tracking the process group.
func (r *RemoteRunner) productionRun(ctx context.Context, dir string, target RemoteTarget, args []string, credentialRef string, lease ProcessLease) (string, error) {
	var extra []*os.File
	var config []string
	env := Env()
	if r.resolver != nil && credentialRef != "" {
		cred, e := r.resolver.Git(ctx, credentialRef, target.URL)
		if e != nil {
			// No child process was ever started, so the launch window is provably
			// empty and must not keep ownership for a later writer.
			finishNoProcess(lease)
			return "", e
		}
		defer func() { _ = cred.Close() }()
		env = append(env, cred.Env...)
		// Only a credential the service resolver actually produced may install
		// the controlled helper. The pipe stays fd 3; the lease file follows.
		config = cred.Config
		extra = append(extra, cred.Files...)
	}
	if lease != nil && lease.File() != nil {
		extra = append(extra, lease.File())
	}
	ctx, cancel := context.WithTimeout(ctx, networkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", bridgeArgs(dir, config, args...)...)
	cmd.Env = env
	cmd.ExtraFiles = extra
	cmd.WaitDelay = time.Second
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &limitedBuffer{}
	setupProcess(cmd)
	if e := cmd.Start(); e != nil {
		// exec.Start failed, so this command has no process group to account for.
		finishNoProcess(lease)
		return "", e
	}
	pid := 0
	if lease != nil {
		pid = cmd.Process.Pid
		if e := lease.Started(pid); e != nil {
			killProcess(cmd)
			_ = waitProcess(cmd)
			finishLease(lease, pid, groupGone(pid))
			return "", e
		}
	}
	e := waitProcess(cmd)
	if lease != nil {
		// Ownership may only be released when this writer's group is provably
		// gone; a surviving group must keep the durable marker.
		finishLease(lease, pid, groupGone(pid))
	}
	return out.String(), e
}

// leaseNoProcess is an optional extension of ProcessLease: proof that a launch
// window produced no child process at all (resolver or exec.Start failure).
type leaseNoProcess interface {
	NoProcess()
}

func finishNoProcess(lease ProcessLease) {
	if f, ok := lease.(leaseNoProcess); ok {
		f.NoProcess()
	}
}

// LeaseTrack builds the gitops.Run tracking callback for a lease, so a
// transport that really spawns Git attributes the child it actually started.
// Callers running real commands must not claim "no process" instead.
func LeaseTrack(lease ProcessLease) func(int) error {
	if lease == nil {
		return nil
	}
	return lease.Started
}

// leaseFinisher is an optional extension of ProcessLease: a lease that wants to
// know the fate of the writer's process group. Leases that do not implement it
// keep their existing behaviour.
type leaseFinisher interface {
	Finished(pid int, groupGone bool)
}

func finishLease(lease ProcessLease, pid int, gone bool) {
	if f, ok := lease.(leaseFinisher); ok {
		f.Finished(pid, gone)
	}
}

// Observe returns the current remote SHA of a ref, or "" when the ref does
// not exist. It proves results after an unknown push outcome.
func (r *RemoteRunner) Observe(ctx context.Context, dir string, target RemoteTarget, ref, credentialRef string, lease ProcessLease) (string, error) {
	if !branchRefAllowed(ref) {
		return "", fmt.Errorf("观察 ref %q 不在受支持的分支范围", ref)
	}
	if lease != nil {
		if e := lease.Attempt(); e != nil {
			return "", e
		}
	}
	out, e := r.run(ctx, dir, target, []string{"ls-remote", "--exit-code", target.URL, ref}, credentialRef, lease)
	if e != nil {
		if exit, ok := exitCode(e); ok && exit == 2 {
			return "", nil // ref absent
		}
		return "", e
	}
	for _, line := range strings.Split(out, "\n") {
		sha, refName, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && refName == ref {
			return sha, nil
		}
	}
	return "", nil
}

func exitCode(e error) (int, bool) {
	var x *exec.ExitError
	if errors.As(e, &x) {
		return x.ExitCode(), true
	}
	return 0, false
}

// Fetch fetches exactly one remote branch into a service-owned observation
// ref, without tags.
func (r *RemoteRunner) Fetch(ctx context.Context, dir string, target RemoteTarget, ref, credentialRef string, lease ProcessLease) (string, error) {
	if !branchRefAllowed(ref) {
		return "", fmt.Errorf("抓取 ref %q 不在受支持的分支范围", ref)
	}
	localRef := observeRef(ref)
	if lease != nil {
		if e := lease.Attempt(); e != nil {
			return "", e
		}
	}
	_, e := r.run(ctx, dir, target, []string{"fetch", "--no-tags", target.URL, ref + ":" + localRef}, credentialRef, lease)
	if e != nil {
		return "", e
	}
	return output(ctx, dir, "rev-parse", localRef)
}

func observeRef(ref string) string {
	name := strings.TrimPrefix(ref, "refs/heads/")
	return "refs/kaiwu/observe/" + name
}

// Push pushes one exact SHA to one exact branch ref (no force) and verifies
// the remote actually holds the SHA. It never implies CI success.
func (r *RemoteRunner) Push(ctx context.Context, dir string, target RemoteTarget, sha, ref, credentialRef string, lease ProcessLease) (string, error) {
	if !branchRefAllowed(ref) {
		return "", fmt.Errorf("推送 ref %q 不在受支持的分支范围", ref)
	}
	if len(sha) != 40 || !isHex(sha) {
		return "", errors.New("推送 SHA 不是受支持的完整提交标识")
	}
	if lease != nil {
		if e := lease.Attempt(); e != nil {
			return "", e
		}
	}
	// No force marker: a non fast-forward must fail loudly.
	_, e := r.run(ctx, dir, target, []string{"push", target.URL, sha + ":" + ref}, credentialRef, lease)
	if e != nil {
		// The outcome is unknown only for network failures; a clean git
		// rejection is a definite refusal. Either way the caller must
		// Observe before claiming success.
		return "", fmt.Errorf("推送未确认成功，请观察远程后再判定: %w", e)
	}
	remote, e := r.Observe(ctx, dir, target, ref, credentialRef, lease)
	if e != nil {
		return "", fmt.Errorf("推送后无法核验远程: %w", e)
	}
	if remote != sha {
		return "", fmt.Errorf("远程 ref 与推送 SHA 不一致: %s != %s", remote, sha)
	}
	return remote, nil
}

func branchRefAllowed(ref string) bool {
	name, ok := strings.CutPrefix(ref, "refs/heads/")
	if !ok || name == "" || !branchNameAllowed.MatchString(name) || strings.Contains(name, "..") {
		return false
	}
	return true
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
