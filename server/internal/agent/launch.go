package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"github.com/SatoriTours/kaiwu/server/internal/pimodel"
	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
	"github.com/SatoriTours/kaiwu/server/internal/workbench"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Program, DataDir, PiAgentDir string
	BaseArgs, ExtraEnv           []string
}

func (m *Manager) launch(c workbench.Conversation) (pirpc.LaunchSpec, error) {
	root := filepath.Join(m.cfg.DataDir, "sessions", c.ID)
	for _, dir := range []string{root, filepath.Join(root, "home"), filepath.Join(root, "agent")} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			return pirpc.LaunchSpec{}, e
		}
	}
	if c.Profile.CredentialSource == "server_pi" {
		if err := pimodel.Prepare(m.cfg.PiAgentDir, filepath.Join(root, "agent"), c.Profile.Provider); err != nil {
			return pirpc.LaunchSpec{}, err
		}
	}
	dir, tools, prompt := root, "read,grep,find,ls", "项目绝对路径为 "+m.store.Snapshot().Projects[c.ProjectID].Path+"。仅可用只读工具访问该项目；当前工作目录是会话私有目录。"
	_, x, development := executionFor(m.store.Snapshot(), c.ID)
	if development {
		if x.Workspace == "" || !pirpc.ProcessGroupsSupported() {
			return pirpc.LaunchSpec{}, errors.New("development workspace unavailable")
		}
		dir, tools = x.Workspace, "read,write,edit,bash,grep,find,ls"
		prompt = "当前任务的独立工作区是 " + dir + "。在此完成任务并说明修改及实际验证；不要自动提交、合并、推送或删除工作区。原项目目录为 " + m.store.Snapshot().Projects[c.ProjectID].Path + "，请保留其内容。"
	}
	args := append([]string{}, m.cfg.BaseArgs...)
	args = append(args, "--mode", "rpc", "--session-dir", root, "--name", "开悟独立对话", "--tools", tools, "--no-extensions", "--no-mcp", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve", "--offline", "--append-system-prompt", prompt)
	if c.SessionFile != "" && (hasHistory(c) || fileExists(c.SessionFile)) {
		if e := m.validateSession(c, c.SessionFile); e != nil {
			return pirpc.LaunchSpec{}, e
		}
		args = append(args, "--session", c.SessionFile)
	} else {
		if hasHistory(c) {
			return pirpc.LaunchSpec{}, errors.New("original session file is missing")
		}
		args = append(args, "--session-id", c.SessionID)
	}
	if c.Profile.Model != "" {
		args = append(args, "--provider", c.Profile.Provider, "--model", c.Profile.Model)
	}
	if c.Profile.Thinking != "" {
		args = append(args, "--thinking", c.Profile.Thinking)
	}
	env := pimodel.Environment(filepath.Join(root, "home"), filepath.Join(root, "agent"))
	env = append(env, m.cfg.ExtraEnv...)
	return pirpc.LaunchSpec{Program: m.cfg.Program, Args: args, Dir: dir, Env: env, OwnProcessGroup: development}, nil
}

func hasHistory(c workbench.Conversation) bool {
	for _, message := range c.Messages {
		if message.Delivery != "pending" && message.Delivery != "failed" && message.Delivery != "not_sent" {
			return true
		}
	}
	return false
}
func fileExists(file string) bool { _, e := os.Lstat(file); return e == nil || !os.IsNotExist(e) }
func (m *Manager) validateLocation(c workbench.Conversation, file string) error {
	root, e := filepath.EvalSymlinks(filepath.Join(m.cfg.DataDir, "sessions", c.ID))
	if e != nil {
		return e
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(file))
	if e != nil {
		return e
	}
	if !filepath.IsAbs(file) || parent != root || filepath.Ext(file) != ".jsonl" {
		return errors.New("session path outside private directory")
	}
	return nil
}

// pi itself creates a new session for missing/empty --session files. Fail closed.
func (m *Manager) validateSession(c workbench.Conversation, file string) error {
	root, e := filepath.EvalSymlinks(filepath.Join(m.cfg.DataDir, "sessions", c.ID))
	if e != nil {
		return e
	}
	canonical, e := filepath.EvalSymlinks(file)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(root, canonical)
	if e != nil || !filepath.IsAbs(file) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("session outside private directory")
	}
	f, e := os.Open(canonical)
	if e != nil {
		return e
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return errors.New("session header missing")
	}
	var header struct {
		Type, ID string
		Version  int
	}
	if json.Unmarshal(scanner.Bytes(), &header) != nil || header.Type != "session" || header.ID != c.SessionID || header.Version != 3 {
		return errors.New("original session header mismatch")
	}
	return nil
}
