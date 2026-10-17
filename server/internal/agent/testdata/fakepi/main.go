// Isolated fixture: no model calls or arbitrary tools; execution writes one fixed test file.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	session := "fixture"
	sessionDir := ""
	development := false
	for i, arg := range os.Args {
		if arg == "--session-id" && i+1 < len(os.Args) {
			session = os.Args[i+1]
		}
		if arg == "--session-dir" && i+1 < len(os.Args) {
			sessionDir = os.Args[i+1]
		}
		if arg == "--tools" && i+1 < len(os.Args) {
			development = strings.Contains(os.Args[i+1], "write")
		}
	}
	file := ""
	for i, arg := range os.Args {
		if arg == "--session" && i+1 < len(os.Args) {
			file = os.Args[i+1]
		}
	}
	if file == "" {
		cwd, _ := os.Getwd()
		if sessionDir != "" {
			cwd = sessionDir
		}
		file = filepath.Join(cwd, session+".jsonl")
		b, _ := json.Marshal(map[string]any{"type": "session", "id": session, "version": 3})
		_ = os.WriteFile(file, append(b, '\n'), 0600)
	} else {
		b, _ := os.ReadFile(file)
		var h struct{ ID string }
		_ = json.Unmarshal([]byte(strings.Split(string(b), "\n")[0]), &h)
		session = h.ID
	}
	out := json.NewEncoder(os.Stdout)
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		var c map[string]any
		if json.Unmarshal(scan.Bytes(), &c) != nil {
			os.Exit(2)
		}
		if c["type"] == "prompt" {
			c["message"] = strings.Split(fmt.Sprint(c["message"]), "\n\n<kaiwu-reply-requirements>")[0]
		}
		data := map[string]any{"sessionId": session, "sessionFile": file, "isStreaming": false, "isCompacting": false, "pendingMessageCount": 0}
		if c["type"] == "prompt" {
			data = map[string]any{"disposition": "started"}
		}
		if c["type"] == "get_available_models" {
			data = map[string]any{"models": []map[string]any{
				{"provider": "fixture", "id": "isolated-test", "name": "隔离测试模型", "reasoning": true, "contextWindow": 200000, "apiKey": "must-not-reach-browser"},
				{"provider": "fixture", "id": "another-test", "name": "另一个测试模型", "reasoning": false},
				{"provider": "second", "id": "isolated-test", "name": "同名模型", "reasoning": false},
			}}
		}
		_ = out.Encode(map[string]any{"type": "response", "id": c["id"], "command": c["type"], "success": true, "data": data})
		if c["type"] == "prompt" {
			time.Sleep(150 * time.Millisecond)
			if c["message"] == "__DISCONNECT_FIXTURE__" {
				os.Exit(0)
			}
			if development {
				_ = out.Encode(map[string]any{"type": "tool_execution_start", "toolCallId": "fixture-write", "toolName": "write", "args": map[string]string{"path": "kaiwu-fixture-result.txt"}})
				err := os.WriteFile("kaiwu-fixture-result.txt", []byte(fmt.Sprint(c["message"])), 0600)
				_ = out.Encode(map[string]any{"type": "tool_execution_end", "toolCallId": "fixture-write", "toolName": "write", "result": map[string]string{"text": "fixed fixture file written"}, "isError": err != nil})
			}
			chunks := []string{fmt.Sprintf("[隔离协议测试] 已收到：%v", c["message"])}
			if c["message"] == "__MARKDOWN_STREAM_FIXTURE__" {
				chunks = []string{"## 流式格式\n\n```go\npackage main\n", "func main() { println(\"ready\") }\n", "```\n\n**完成**"}
			}
			if strings.Contains(fmt.Sprint(c["message"]), "结构化登录恢复") {
				_ = out.Encode(map[string]any{"type": "tool_execution_start", "toolCallId": "fixture-check", "toolName": "bash", "args": map[string]string{"command": "fixture check"}})
				_ = out.Encode(map[string]any{"type": "tool_execution_end", "toolCallId": "fixture-check", "toolName": "bash", "result": map[string]string{"text": "PASS fixture"}, "isError": false})
				chunks = []string{`刷新恢复已实现，结果已整理。<kaiwu-report>{"summary":"登录恢复已实现，等待确认","stage":"awaiting_review","plan":["保存登录","验证恢复"],"results":["刷新自动恢复","退出撤销凭证"],"checks":[{"title":"登录恢复检查","claim":"passed","toolId":"fixture-check"}],"blockers":[],"next":["确认使用效果"],"question":""}</kaiwu-report>`}
			}
			for i, chunk := range chunks {
				if i > 0 {
					time.Sleep(700 * time.Millisecond)
				}
				_ = out.Encode(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": chunk}})
			}
			_ = out.Encode(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "stopReason": "stop"}})
			_ = out.Encode(map[string]any{"type": "agent_settled"})
		}
	}
}
