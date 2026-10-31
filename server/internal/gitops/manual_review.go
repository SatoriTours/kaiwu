package gitops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// maxUploadCommits bounds the disclosed upload range instead of truncating it.
const maxUploadCommits = 200

// ManualRemote is the frozen remote target selected from the project's own
// configuration. It carries no policy or numeric repository identity.
type ManualRemote struct {
	Name          string
	URL           string
	MainRef       string
	CredentialRef string
}

// ResolveManualRemote picks the manual remote from the real project config:
// origin when present, otherwise the only configured remote. Only supported
// github.com addresses are accepted and the config must not be rewritten or
// ambiguous.
func ResolveManualRemote(ctx context.Context, p workbench.Project) (ManualRemote, error) {
	target := ManualRemote{}
	names, e := remoteNames(ctx, p.Path)
	if e != nil {
		return target, e
	}
	name := ""
	if _, ok := names["origin"]; ok {
		name = "origin"
	} else if len(names) == 1 {
		for n := range names {
			name = n
		}
	} else if len(names) == 0 {
		return target, errors.New("项目未配置远程仓库")
	} else {
		return target, errors.New("项目配置了多个远程仓库且没有 origin，无法确定审核目标")
	}
	urls, e := output(ctx, p.Path, "config", "--get-all", "remote."+name+".url")
	if e != nil {
		return target, errors.New("项目远程配置不可读")
	}
	values := nonEmpty(urls)
	if len(values) != 1 {
		return target, errors.New("远程地址必须唯一，当前存在多个 fetch 地址")
	}
	url, e := workbench.CanonicalGitHubURL(values[0])
	if e != nil {
		return target, e
	}
	if e := verifyManualRemote(ctx, p.Path, name, url); e != nil {
		return target, e
	}
	branch, e := ResolveMain(ctx, p)
	if e != nil {
		return target, e
	}
	if !branchNameAllowed.MatchString(branch) || strings.Contains(branch, "..") {
		return target, fmt.Errorf("主分支 %q 不是受支持的名称", branch)
	}
	target.Name = name
	target.URL = url
	target.MainRef = "refs/heads/" + branch
	if strings.HasPrefix(url, "ssh://") {
		target.CredentialRef = "server_github_ssh"
	} else {
		target.CredentialRef = "server_github_https"
	}
	return target, nil
}

func remoteNames(ctx context.Context, path string) (map[string]bool, error) {
	raw, e := output(ctx, path, "config", "--get-regexp", `^remote\..*\.url$`)
	names := map[string]bool{}
	if e != nil {
		if strings.Contains(e.Error(), "no such section") || strings.Contains(e.Error(), "exit status 1") {
			return names, nil
		}
		return names, errors.New("项目远程配置不可读")
	}
	for _, line := range nonEmpty(raw) {
		key, _, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		names[strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")] = true
	}
	return names, nil
}

// verifyManualRemote rejects ambiguous or rewritten remote configuration: a
// conflicting push URL, URL rewrites in either direction, or a remote helper.
func verifyManualRemote(ctx context.Context, path, name, url string) error {
	pushURLs, e := output(ctx, path, "config", "--get-all", "remote."+name+".pushurl")
	if e == nil {
		for _, u := range nonEmpty(pushURLs) {
			canonical, err := workbench.CanonicalGitHubURL(u)
			if err != nil || canonical != url {
				return errors.New("远程存在不一致的 pushurl")
			}
		}
	}
	for _, key := range []string{`^url\..*\.insteadof$`, `^url\..*\.pushinsteadof$`} {
		if raw, e := output(ctx, path, "config", "--get-regexp", key); e == nil && len(nonEmpty(raw)) > 0 {
			return errors.New("存在 URL rewrite 配置，远程地址不可信")
		}
	}
	helper, e := output(ctx, path, "config", "--get-regexp", `^remote\..*\.vcs$`)
	if e == nil && len(nonEmpty(helper)) > 0 {
		return errors.New("存在 remote helper 配置")
	}
	if e := rejectHTTPTransportOverrides(ctx, path); e != nil {
		return e
	}
	return rejectCredentialHelper(ctx, path)
}

// rejectCredentialHelper refuses any repository-controlled git credential
// helper, generic or URL-scoped (including ones pulled in by an include file).
// Such config would execute an arbitrary program on the first 401 response.
func rejectCredentialHelper(ctx context.Context, path string) error {
	raw, e := output(ctx, path, "config", "--get-regexp", `^credential\..*helper$`)
	if e != nil {
		if strings.Contains(e.Error(), "exit status") || strings.Contains(e.Error(), "no such section") {
			return nil
		}
		return errors.New("项目 credential 配置不可读")
	}
	for _, line := range nonEmpty(raw) {
		key, value, ok := strings.Cut(line, " ")
		// An empty value only resets git's helper list; it never runs anything.
		if ok && strings.Contains(key, "helper") && strings.TrimSpace(value) != "" {
			return errors.New("存在 credential helper 配置，远程操作不可信")
		}
	}
	return nil
}

func nonEmpty(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// CheckCredential proves the service can obtain the frozen Git credential
// without any network access.
func (r *RemoteRunner) CheckCredential(ctx context.Context, target RemoteTarget, ref string) error {
	if r.resolver == nil {
		return errors.New("服务未配置 GitHub Git 凭据")
	}
	cred, e := r.resolver.Git(ctx, ref, target.URL)
	if e != nil {
		return e
	}
	return cred.Close()
}

// ReviewRemote performs the read-only manual review. It only observes the
// remote (ls-remote), never fetches, never touches the index or refs, and never
// pushes. A blocked push still returns the complete local proof.
// contentProof binds the review to the exact local content when the local
// preview has no fingerprint, which is the case for an already merged task.
func contentProof(ctx context.Context, dir string, local workbench.MergePreview) (string, error) {
	h := sha256.New()
	for _, spec := range []struct{ sha, label string }{{local.MainSHA, "main"}, {local.TaskSHA, "task"}} {
		if spec.sha == "" {
			return "", errors.New("缺少内容提交")
		}
		tree, e := output(ctx, dir, "rev-parse", "--verify", spec.sha+"^{tree}")
		if e != nil {
			return "", e
		}
		io.WriteString(h, spec.label+"\x00"+strings.TrimSpace(tree)+"\x00")
	}
	io.WriteString(h, local.MainBranch+"\x00"+local.MainSHA+"\x00"+local.TaskSHA)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// localSafety re-verifies, independently of the preview, every local admission
// protection a push inherits: task worktree ownership, main checkout state,
// private/service data scoping and the committed file scope. It returns the
// protected paths so the caller can re-scope the exact upload set.
func localSafety(ctx context.Context, p workbench.Project, t workbench.Task, x workbench.Execution, local workbench.MergePreview, serviceRoots []string) (string, []string, error) {
	if x.Workspace == "" || x.Branch == "" {
		return "", nil, errors.New("任务还没有独立工作区，无法审核推送")
	}
	if e := validateOwner(ctx, p, t, x); e != nil {
		return "", nil, e
	}
	protected, e := privateScope(p, x, serviceRoots)
	if e != nil {
		return "", nil, e
	}
	if e = unfinished(ctx, x.Workspace); e != nil {
		return "", nil, e
	}
	dirty, e := Files(ctx, x.Workspace)
	if e != nil {
		return "", nil, e
	}
	if len(dirty) > 0 {
		if e = eligible(ctx, x.Workspace, dirty, protected...); e != nil {
			return "", nil, e
		}
	}
	branch, e := ResolveMain(ctx, p)
	if e != nil {
		return "", nil, e
	}
	if e = mainReady(ctx, p, branch); e != nil {
		return "", nil, e
	}
	if e = committedScope(ctx, x.Workspace, x.BaseSHA, local.TaskSHA, protected...); e != nil {
		return "", nil, e
	}
	return "owner+main-ready+private-scope+worktree+committed-scope", protected, nil
}

// RemoteTarget converts a resolved manual remote into the runner target.
func RemoteTargetFromManual(m ManualRemote) RemoteTarget {
	return RemoteTarget{URL: m.URL, MainRef: m.MainRef}
}

func RemoteTargetOf(m ManualRemote) RemoteTarget {
	return RemoteTargetFromManual(m)
}

// ReviewRemote builds the read-only push review. It never fetches, never writes
// a ref or the index and never stores a credential: it observes the remote,
// verifies the local safety evidence and discloses the exact upload set.
func ReviewRemote(ctx context.Context, runner *RemoteRunner, p workbench.Project, t workbench.Task, x workbench.Execution, local workbench.MergePreview, req workbench.MergeReviewRequest, serviceRoots ...string) (workbench.MergeReview, error) {
	review := workbench.MergeReview{MergePreview: local, Uploads: []workbench.MergeUpload{}, Pending: []workbench.GitFile{}}
	if local.NeedsCommit {
		review.Pending = append(review.Pending, local.Files...)
		// Pending work is not a commit yet; say so on every path, including a
		// blocked one, so the review never implies the commit exists.
		review.PendingNote = "待提交改动尚未形成提交，推送时会先提交再合并；该提交不在本次上传列表内"
	}
	review.Mode, review.RoundID = req.Mode, req.RoundID
	blocked := func(reason string) (workbench.MergeReview, error) {
		review.CanPush = false
		review.PushReason = reason
		review.ReviewFingerprint = reviewFingerprint(review)
		return review, nil
	}
	review.LocalMainSHA = local.MainSHA
	taskSHA := local.TaskSHA
	if taskSHA == "" {
		return blocked("任务尚未产生可审核的提交")
	}

	// The local integration facts are computed and recorded before anything can
	// block, so a later remote or credential failure still reports accurately
	// whether the committed task is already in the local main branch.
	//
	// AlreadyMerged means the committed taskSHA is an ancestor of the local
	// main branch. It deliberately does not describe uncommitted work.
	// PushOnly is the flag that decides whether the caller may skip the
	// commit+merge step, so it also requires that nothing is pending.
	localMerged, e := Ancestor(ctx, p.Path, taskSHA, local.MainSHA)
	if e != nil || local.MainSHA == "" {
		return blocked("无法核对本地主分支是否已包含本任务")
	}
	review.AlreadyMerged = localMerged
	review.PushOnly = localMerged && !local.NeedsCommit && len(review.Pending) == 0

	remote, e := ResolveManualRemote(ctx, p)
	if e != nil {
		return blocked(e.Error())
	}
	review.RemoteName, review.RemoteURL, review.MainRef = remote.Name, remote.URL, remote.MainRef
	target := RemoteTarget{URL: remote.URL, MainRef: remote.MainRef}

	// Local admission first: every refusal the local merge inherits must also
	// block the push, so a rejected task can never look pushable. An already
	// merged task is refused locally by design, so it is verified here instead.
	if !localMerged && !local.CanMerge {
		reason := local.Reason
		if reason == "" {
			reason = "本地合并预检未通过"
		}
		return blocked("本地合并不允许，不能推送：" + reason)
	}
	proof, protected, e := localSafety(ctx, p, t, x, local, serviceRoots)
	if e != nil {
		return blocked("本地安全核验未通过：" + e.Error())
	}
	review.SafetyProof = proof
	// Only the resolver reference name is disclosed; it is not a credential.
	review.CredentialRef = remote.CredentialRef
	if review.Fingerprint == "" {
		// An already merged task is refused by the local preview before it
		// computes a fingerprint, so the review derives its own content proof.
		if f, fe := contentProof(ctx, p.Path, local); fe == nil {
			review.Fingerprint = f
		} else {
			return blocked("无法核对本地内容指纹")
		}
	}

	// A missing service credential blocks only the push review; the local proof
	// stays valid and the conservative planned-result range is still disclosed.
	if e := runner.CheckCredential(ctx, target, remote.CredentialRef); e != nil {
		if uploads, ue := uploadRange(ctx, p.Path, "", []string{local.MainSHA, taskSHA}); ue == nil {
			review.Uploads = uploads
		}
		return blocked("缺少 Git 远程凭据，无法观察远程状态：" + e.Error())
	}
	observed, observeErr := runner.Observe(ctx, p.Path, target, remote.MainRef, remote.CredentialRef, nil)
	if observeErr != nil {
		return blocked("远程状态观察失败：" + observeErr.Error())
	}
	review.ObservedRemoteSHA = observed
	if observed != "" {
		// The remote object must be known locally; guessing or fetching is not
		// acceptable for a review.
		if _, e := output(ctx, p.Path, "rev-parse", "--verify", observed+"^{commit}"); e != nil {
			return blocked("远程主分支提交在本地不可用，无法完整确定上传范围")
		}
		// The planned result must be able to fast-forward the observed remote;
		// otherwise somebody else moved the branch and forcing is out of scope.
		ff, e := Ancestor(ctx, p.Path, observed, local.MainSHA)
		if e != nil {
			return blocked("无法核对远程快进关系")
		}
		if !ff {
			contained, ce := Ancestor(ctx, p.Path, observed, taskSHA)
			if ce != nil || !contained {
				return blocked("远程主分支已有本地不包含的提交，无法快进，也不会强制推送")
			}
		}
	}
	// The upload set is every commit reachable from either merge input and not
	// from the observed remote, so extra local main commits are never hidden.
	uploads, e := uploadRange(ctx, p.Path, observed, []string{local.MainSHA, taskSHA})
	if e != nil {
		return blocked(e.Error())
	}
	review.Uploads = uploads
	// The same commit set is what would be uploaded, so the private-file scope is
	// re-verified over all of it, including files later deleted.
	if e := committedScopeSet(ctx, p.Path, observed, []string{local.MainSHA, taskSHA}, protected...); e != nil {
		return blocked("待上传提交包含需先处理的私密文件或不安全路径")
	}
	if len(uploads) == 0 && !local.NeedsCommit {
		if review.AlreadyMerged {
			return blocked("本地主分支已包含本任务，远程也已包含全部提交，无需推送")
		}
		return blocked("没有需要上传的提交")
	}
	review.CanPush = true
	review.ReviewFingerprint = reviewFingerprint(review)
	return review, nil
}

// remoteMainContains reports whether the local main already integrated the task.
func remoteMainContains(ctx context.Context, dir, mainSHA, taskSHA string) bool {
	if mainSHA == "" || taskSHA == "" {
		return false
	}
	ok, e := Ancestor(ctx, dir, taskSHA, mainSHA)
	return e == nil && ok
}

// uploadRange lists the exact commits a push would upload: everything reachable
// from any head and not from the observed remote base. An empty base means the
// whole history would create the branch. Overflows are refused, never truncated.
func uploadRange(ctx context.Context, dir, base string, heads []string) ([]workbench.MergeUpload, error) {
	args := []string{"log", "--format=%H%x09%s", "-n", strconv.Itoa(maxUploadCommits + 1)}
	for _, head := range heads {
		if head == "" {
			return nil, errors.New("待上传范围缺少拟集成的提交")
		}
		if len(head) != 40 || !isHex(head) {
			return nil, errors.New("待上传范围的提交标识无效")
		}
	}
	args = append(args, heads...)
	if base != "" {
		if len(base) != 40 || !isHex(base) {
			return nil, errors.New("远程观察提交标识无效")
		}
		args = append(args, "--not", base)
	}
	args = append(args, "--")
	raw, e := output(ctx, dir, args...)
	if e != nil {
		return nil, errors.New("无法完整列出待上传提交")
	}
	seen := map[string]bool{}
	uploads := make([]workbench.MergeUpload, 0, maxUploadCommits)
	for _, row := range nonEmpty(raw) {
		sha, subject, ok := strings.Cut(row, "\t")
		if !ok || len(sha) != 40 || !isHex(sha) {
			return nil, errors.New("提交列表格式异常")
		}
		if seen[sha] {
			continue
		}
		seen[sha] = true
		uploads = append(uploads, workbench.MergeUpload{SHA: sha, Subject: subject})
		if len(uploads) > maxUploadCommits {
			return nil, fmt.Errorf("待上传提交超过 %d 个，请先整理分支后再审核", maxUploadCommits)
		}
	}
	return uploads, nil
}

// reviewFingerprint binds the actual main/task content, the remote configuration
// and observation, the upload list, and the review mode and round.
func reviewFingerprint(r workbench.MergeReview) string {
	payload := struct {
		Mode, RoundID            string
		Preview                  string
		RemoteName, RemoteURL    string
		MainRef, Observed        string
		LocalMain, Task          string
		Uploads                  []workbench.MergeUpload
		Pending                  []workbench.GitFile
		SafetyProof, PendingNote string
		AlreadyMerged, PushOnly  bool
		NeedsCommit, CanMerge    bool
	}{r.Mode, r.RoundID, r.Fingerprint, r.RemoteName, r.RemoteURL, r.MainRef, r.ObservedRemoteSHA, r.LocalMainSHA, r.TaskSHA, r.Uploads, r.Pending, r.SafetyProof, r.PendingNote, r.AlreadyMerged, r.PushOnly, r.NeedsCommit, r.CanMerge}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
