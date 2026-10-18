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

// Match the current Web UI (lang="zh-CN"), including restored conversations.
const replyLanguageInstructions = `
开悟系统语言为简体中文（zh-CN）。所有面向用户的回复必须使用简体中文，包括普通对话、开发过程更新、问题、阻塞说明、最终结果，以及 <kaiwu-report> 中 summary、plan、results、checks.title、blockers、next、question 的自然语言内容。历史回复或工具输出为英文，不改变回复语言。代码、命令、路径、标识符、产品名、必要的原文引用保持原样；报告 JSON 字段名、stage/claim 枚举值及 toolId 必须保持协议规定的原值，不要翻译。
`

// Repeat presentation requirements with each explicit user turn so a warm or
// restored session does not drift back to the language of its earlier history.
const replyTurnInstructions = `

<kaiwu-reply-requirements>
本轮所有面向用户的内容使用简体中文，包括过程更新、摘要、问题、最终回复和结构化报告的自然语言字段；不要沿用历史英文叙述。代码、命令、路径、标识符及必要原文引用保留原样。
使用简短的 Markdown 段落；并列事项用列表，代码与命令用代码块，不把多个事项挤成一长行。真实可访问的 URL 单独一行，以 [查看效果](实际URL) 等明确文字提供可点击链接；保留完整协议、主机、端口和路径，不编造或改写地址，不把未验证的访问描述为已验证。
</kaiwu-reply-requirements>`

const taskPreviewInstructions = `
如果任务适合网页或 HTTP 效果验收，请在当前任务 worktree 根目录生成 kaiwu-preview.json：{"version":1,"command":["实际程序","参数","--host","127.0.0.1","--port","{port}"],"path":"/"}。command 是直接执行的 argv，不经 shell，至少一项使用 {port}，服务必须仅监听 127.0.0.1。使用项目实际可执行命令，不编造服务。此配置只声明预览，禁止自行启动常驻预览进程；用户点击“启动预览”后由开悟分配端口并启动。预览支持 GET/HEAD 和相对资源，不支持 WebSocket/HMR、应用登录或跨源资源。CLI/库等非网页任务不强造 manifest，请提供真实运行结果、使用说明和验证情况。
`

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
		prompt += taskPreviewInstructions
	}
	args := append([]string{}, m.cfg.BaseArgs...)
	args = append(args, "--mode", "rpc", "--session-dir", root, "--name", "开悟独立对话", "--tools", tools, "--no-extensions", "--no-mcp", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve", "--offline", "--append-system-prompt", prompt+workbench.ReportInstructions+workbench.InteractionInstructions+replyLanguageInstructions+replyTurnInstructions)
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
