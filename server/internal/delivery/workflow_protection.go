package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/SatoriTours/kaiwu/server/internal/gitops"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

// workflowFingerprint freezes Git blobs, file modes and path names, not YAML
// syntax or workflow IDs. Protect all of .github, including local actions and
// symlink entries, so an unchanged workflow cannot call a replaced local action.
// Git tree inspection never follows a worktree symlink or loads its contents.
func workflowFingerprint(ctx context.Context, dir, tree string) (string, error) {
	if len(tree) != 40 || strings.Trim(tree, "0123456789abcdef") != "" {
		return "", errors.New("保护定义缺少完整的 Git tree/commit 标识")
	}
	// Local refs/replace are not uploaded: always inspect the actual object
	// that would be pushed, including all descendant trees, not its substitute.
	out, err := gitops.Run(ctx, dir, nil, "--no-replace-objects", "ls-tree", "-r", "-z", tree, "--", ".github")
	if err != nil {
		return "", errors.New("实际 workflow 定义不可读")
	}
	sum := sha256.Sum256([]byte(out))
	return "workflows-v1:" + hex.EncodeToString(sum[:]), nil
}

// protectedDefinitionsFingerprint extends the workflow baseline with explicitly
// declared check files. Never follow symlinks or infer arbitrary program
// dependencies. Missing files, directories and submodules fail closed.
func protectedDefinitionsFingerprint(ctx context.Context, dir, tree string, checks []workbench.DeliveryCheck) (string, error) {
	paths, err := workbench.DeliveryDefinitionPaths(checks)
	if err != nil {
		return "", err
	}
	workflow, err := workflowFingerprint(ctx, dir, tree)
	if err != nil {
		return "", err
	}
	// Preserve baselines for existing cycles that declare no additional paths.
	if len(paths) == 0 {
		return workflow, nil
	}
	args := append([]string{"--no-replace-objects", "ls-tree", "--full-tree", "-z", tree, "--"}, paths...)
	out, err := gitops.Run(ctx, dir, nil, args...)
	if err != nil {
		return "", errors.New("已声明检查定义不可读")
	}
	found := map[string]bool{}
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		meta, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			return "", errors.New("保护定义必须是 Git 中的普通文件，不能是目录、符号链接或子模块")
		}
		found[name] = true
	}
	for _, p := range paths {
		if !found[p] {
			return "", fmt.Errorf("主分支或候选缺少已声明保护定义文件：%s", p)
		}
	}
	payload, _ := json.Marshal(struct {
		Workflow string
		Paths    []string
		Entries  string
	}{workflow, paths, out})
	sum := sha256.Sum256(payload)
	return "definitions-v1:" + hex.EncodeToString(sum[:]), nil
}

func (m *Manager) verifyWorkflowTree(d workbench.Delivery, dir, tree string) (string, string) {
	if d.ProtectedFingerprint == "" {
		return "protected_definitions_missing", "旧交付周期没有冻结实际 workflow，请人工复核并开启新周期"
	}
	actual, err := protectedDefinitionsFingerprint(m.ctx, dir, tree, d.Policy.Checks)
	if err != nil {
		return "protected_definitions_unavailable", err.Error()
	}
	if actual != d.ProtectedFingerprint {
		return "protected_definitions_changed", "workflow、.github 或已声明检查定义已变化，已停止自动交付，请人工审核"
	}
	return "", ""
}

// verifyDefinitionsAfterChecks also fails closed if a check made a special
// file that prevented RunChecks from returning a final candidate tree.
func (m *Manager) verifyDefinitionsAfterChecks(d workbench.Delivery, dir, tree string) (string, string) {
	if tree == "" {
		snapshot, err := gitops.CaptureWorkspaceSnapshot(m.ctx, dir, nil)
		if err != nil {
			return "protected_definitions_unavailable", "检查后实际定义无法核查，请人工审核"
		}
		tree = snapshot.Tree
	}
	return m.verifyWorkflowTree(d, dir, tree)
}

// gateWorkflowCommits runs alongside the policy gate, including recovery/CI.
// Exact candidate/main commit objects are checked again before they may be
// pushed or accepted. Dirty working contents are checked under the write lease
// by the local/integration checking stages.
func (m *Manager) gateWorkflowCommits(d workbench.Delivery) (string, string) {
	if d.Phase == "queued" {
		return "", ""
	}
	p, _, _, ok := executionOf(m.store.Snapshot(), d)
	if !ok {
		return "workspace_missing", "项目或任务已不存在"
	}
	if d.ProtectedFingerprint == "" {
		return "protected_definitions_missing", "旧交付周期没有冻结实际 workflow，请人工复核并开启新周期"
	}
	if d.CandidateSHA != "" {
		if code, reason := m.verifyWorkflowTree(d, p.Path, d.CandidateSHA); code != "" {
			return code, reason
		}
	}
	switch d.Phase {
	case "integration_checking", "pushing_main", "main_ci":
		return m.verifyWorkflowTree(d, p.Path, d.LocalMainSHA)
	}
	return "", ""
}
