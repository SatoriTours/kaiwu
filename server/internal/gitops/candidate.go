package gitops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// Candidate freezes one checked working state: HEAD, the tree the staged
// candidate will produce, a fingerprint over files/blob/mode/index/HEAD and
// the protected-scope fingerprint. The protected scope itself travels with
// the candidate so commit-time checks stay bound to the capture-time rules.
type Candidate struct {
	HEAD, Tree, Fingerprint, ProtectedFingerprint string
	Files                                         []workbench.GitFile
	protected                                     []string
}

func trackedRun(ctx context.Context, dir string, lease ProcessLease, args ...string) (string, error) {
	if lease != nil {
		if e := lease.Attempt(); e != nil {
			return "", e
		}
	}
	track := func(int) error { return nil }
	if lease != nil {
		track = func(id int) error { return lease.Started(id) }
	}
	return Run(ctx, dir, track, args...)
}

// workingTree returns the tree hash the current working contents would
// produce, computed through a temporary index so the real index is untouched.
func workingTree(ctx context.Context, dir string, lease ProcessLease) (string, error) {
	f, e := os.CreateTemp("", "kaiwu-index-")
	if e != nil {
		return "", e
	}
	name := f.Name()
	_ = f.Close()
	// Git must create the index file itself; an empty file reads as a
	// truncated index.
	_ = os.Remove(name)
	defer os.Remove(name)
	env := append(Env(), "GIT_INDEX_FILE="+name)
	track := func(int) error { return nil }
	if lease != nil {
		if e = lease.Attempt(); e != nil {
			return "", e
		}
		track = func(id int) error { return lease.Started(id) }
	}
	if _, e = runEnv(ctx, dir, env, track, "add", "--all", "--", "."); e != nil {
		return "", errors.New("无法暂存候选内容以核对 tree")
	}
	tree, e := runEnv(ctx, dir, env, track, "write-tree")
	if e != nil {
		return "", errors.New("无法核对候选 tree")
	}
	return strings.TrimSpace(tree), nil
}

func captureFingerprint(ctx context.Context, dir string, protected []string, lease ProcessLease) (Candidate, error) {
	// JSON-encodable normalization keeps nil and empty scopes comparable.
	if protected == nil {
		protected = []string{}
	}
	c := Candidate{protected: protected}
	files, e := Files(ctx, dir)
	if e != nil {
		return c, e
	}
	c.Files = files
	if c.HEAD, e = Head(ctx, dir, "HEAD"); e != nil {
		return c, e
	}
	blobs, e := workingBlobs(ctx, dir, files)
	if e != nil {
		return c, e
	}
	modes, e := workingModes(ctx, dir, files)
	if e != nil {
		return c, e
	}
	index, e := trackedRun(ctx, dir, lease, "ls-files", "--stage", "-z")
	if e != nil {
		return c, e
	}
	h := sha256.New()
	b, _ := json.Marshal(struct {
		Files []workbench.GitFile
		Blobs map[string]string
		Modes map[string]string
		Index string
		HEAD  string
		Scope []string
	}{files, blobs, modes, index, c.HEAD, protected})
	h.Write(b)
	c.Fingerprint = hex.EncodeToString(h.Sum(nil))
	scope, _ := json.Marshal(protected)
	h2 := sha256.Sum256(scope)
	c.ProtectedFingerprint = hex.EncodeToString(h2[:])
	if c.Tree, e = workingTree(ctx, dir, lease); e != nil {
		return c, e
	}
	return c, nil
}

// CaptureCandidate freezes the task working state with every protection used
// by merge review: ownership, private paths, unfinished Git operations,
// deduped staged files and commit scope.
func CaptureCandidate(ctx context.Context, p workbench.Project, t workbench.Task, x workbench.Execution, serviceRoots ...string) (Candidate, error) {
	c := Candidate{}
	if x.Workspace == "" {
		return c, errors.New("任务执行缺少工作区")
	}
	if e := validateOwner(ctx, p, t, x); e != nil {
		return c, e
	}
	protected, e := privateScope(p, x, serviceRoots)
	if e != nil {
		return c, e
	}
	if e = unfinished(ctx, x.Workspace); e != nil {
		return c, e
	}
	files, e := Files(ctx, x.Workspace)
	if e != nil {
		return c, e
	}
	if len(files) > 0 {
		if e = eligible(ctx, x.Workspace, files, protected...); e != nil {
			return c, e
		}
	}
	// A clean final tree can still contain private data in reachable commits.
	// Inspect every introduced history, not only currently dirty candidates.
	head, err := Head(ctx, x.Workspace, "HEAD")
	if err != nil {
		return c, err
	}
	if e = committedScope(ctx, x.Workspace, x.BaseSHA, head, protected...); e != nil {
		return c, e
	}
	return captureFingerprint(ctx, x.Workspace, protected, nil)
}

// CaptureWorkspaceSnapshot freezes a bare workspace fingerprint without
// ownership assumptions; the caller owns writer protection via its lease.
func CaptureWorkspaceSnapshot(ctx context.Context, dir string, lease ProcessLease) (Candidate, error) {
	if dir == "" {
		return Candidate{}, errors.New("缺少工作区")
	}
	info, e := os.Stat(dir)
	if e != nil || !info.IsDir() {
		return Candidate{}, errors.New("任务工作区不可用")
	}
	return captureFingerprint(ctx, dir, nil, lease)
}

// CommitCandidate commits exactly the checked candidate. Any change after the
// check — content, staging, mode, symlink or a new sensitive file — refails
// the candidate without creating a commit.
func CommitCandidate(ctx context.Context, x workbench.Execution, c Candidate, message, name, email string, runner Runner) (string, error) {
	if x.Workspace == "" {
		return "", errors.New("任务执行缺少工作区")
	}
	if name == "" {
		name, _ = output(ctx, x.Workspace, "config", "--local", "--get", "user.name")
	}
	if email == "" {
		email, _ = output(ctx, x.Workspace, "config", "--local", "--get", "user.email")
	}
	if name == "" || email == "" {
		name, email = "Kaiwu", "kaiwu@localhost"
	}
	if strings.ContainsAny(name+email, "\r\n\x00<>") || strings.TrimSpace(name) == "" || strings.TrimSpace(email) == "" {
		return "", errors.New("提交身份无效，请在高级设置中修改")
	}
	fresh, e := captureFingerprint(ctx, x.Workspace, c.protected, nil)
	if e != nil {
		return "", fmt.Errorf("候选快照不可读: %w", e)
	}
	if fresh.Fingerprint != c.Fingerprint || fresh.HEAD != c.HEAD || fresh.ProtectedFingerprint != c.ProtectedFingerprint {
		return "", errors.New("候选在检查后发生变化，请重新检查；未创建提交")
	}
	if _, e := runner(ctx, x.Workspace, "add", "--all", "--", "."); e != nil {
		return "", errors.New("暂存候选文件失败，保留工作区供检查")
	}
	staged, e := Run(ctx, x.Workspace, nil, "ls-files", "--stage", "-z")
	if e != nil {
		return "", e
	}
	blobs, e := workingBlobs(ctx, x.Workspace, fresh.Files)
	if e != nil {
		return "", e
	}
	modes, e := workingModes(ctx, x.Workspace, fresh.Files)
	if e != nil {
		return "", e
	}
	actual := map[string]string{}
	actualModes := map[string]string{}
	for _, row := range strings.Split(staged, "\x00") {
		meta, path, ok := strings.Cut(row, "\t")
		if !ok {
			continue
		}
		parts := strings.Fields(meta)
		if len(parts) != 3 || parts[2] != "0" {
			return "", errors.New("暂存区存在未解决条目")
		}
		actual[path] = parts[1]
		actualModes[path] = parts[0]
	}
	for path, sha := range blobs {
		if actual[path] != sha || actualModes[path] != modes[path] {
			return "", errors.New("暂存内容与检查不一致，请重新检查；未创建提交")
		}
	}
	now, _ := Head(ctx, x.Workspace, "HEAD")
	if now != c.HEAD {
		return "", errors.New("任务提交在写入前已变化，请重新检查")
	}
	// The real index is what git commit consumes. Rebuilding a temporary
	// index from the worktree cannot detect an index-only race on a clean file.
	tree, e := output(ctx, x.Workspace, "write-tree")
	if e != nil {
		return "", e
	}
	if tree != c.Tree {
		return "", errors.New("实际暂存 tree 与检查证据不一致；未创建提交")
	}
	if _, e := runner(ctx, x.Workspace, "-c", "user.name="+name, "-c", "user.email="+email, "commit", "--no-gpg-sign", "-m", message); e != nil {
		return "", errors.New("候选提交未确认成功，请检查工作区；未自动重试")
	}
	sha, err := Head(ctx, x.Workspace, "HEAD")
	if err != nil {
		return "", err
	}
	committedTree, treeErr := output(ctx, x.Workspace, "rev-parse", sha+"^{tree}")
	parent, parentErr := output(ctx, x.Workspace, "rev-parse", sha+"^1")
	if treeErr != nil || parentErr != nil || committedTree != c.Tree || parent != c.HEAD {
		return "", errors.New("提交 parent/tree 与已检查候选不符；保留现场，禁止上传")
	}
	return sha, nil
}
