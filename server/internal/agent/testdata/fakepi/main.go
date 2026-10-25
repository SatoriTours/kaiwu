// Isolated fixture: no model calls or arbitrary tools; execution writes one fixed test file.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// interactionFixture is a fixed six-field product interaction (confirm, text,
// textarea, single_select, multi_select, number) used by the real browser E2E.
func interactionFixture(title string) string {
	return `{"title":"` + title + `","description":"请提供部署参数，提交后原会话继续。","allowCancel":true,"fields":[` +
		`{"id":"allow","type":"confirm","label":"是否允许继续部署","yesLabel":"允许","noLabel":"不允许"},` +
		`{"id":"note","type":"text","label":"变更标题","required":true,"maxLength":120},` +
		`{"id":"details","type":"textarea","label":"详细说明","maxLength":4000},` +
		`{"id":"env","type":"single_select","label":"目标环境","required":true,"allowOther":true,"options":[{"id":"staging","label":"测试环境"},{"id":"prod","label":"生产环境"}]},` +
		`{"id":"features","type":"multi_select","label":"启用特性","required":true,"allowOther":true,"options":[{"id":"metrics","label":"指标"},{"id":"tracing","label":"链路追踪"}],"minSelections":1,"maxSelections":3},` +
		`{"id":"port","type":"number","label":"端口","required":true,"integer":true,"min":0,"max":65535,"unit":"端口"}]}`
}

func secondInteractionFixture() string {
	return `{"title":"第二个问题","allowCancel":true,"fields":[` +
		`{"id":"second","type":"confirm","label":"是否继续第二轮","yesLabel":"继续","noLabel":"停止"},` +
		`{"id":"reason","type":"text","label":"第二项说明","maxLength":200}]}`
}

// nativeDialog emits one extension_ui_request and waits for the matching raw
// response, answering any unrelated command so the transport never stalls.
func nativeDialog(out *json.Encoder, scan *bufio.Scanner, session, file, model, provider string, request map[string]any) map[string]any {
	if err := out.Encode(request); err != nil {
		return nil
	}
	id := fmt.Sprint(request["id"])
	for scan.Scan() {
		var c map[string]any
		if json.Unmarshal(scan.Bytes(), &c) != nil {
			return nil
		}
		if c["type"] == "extension_ui_response" {
			if fmt.Sprint(c["id"]) == id {
				return c
			}
			continue
		}
		data := map[string]any{"sessionId": session, "sessionFile": file, "isStreaming": false, "isCompacting": false, "pendingMessageCount": 0, "model": map[string]any{"id": model, "provider": provider, "contextWindow": 200000}}
		_ = out.Encode(map[string]any{"type": "response", "id": c["id"], "command": c["type"], "success": true, "data": data})
	}
	return nil
}

func main() {
	for i, arg := range os.Args {
		if arg == "--serve-preview" && i+1 < len(os.Args) {
			port := os.Args[i+1]
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>服务进程网页</title></head><body><h1 id=\"proc-title\">开悟服务进程测试</h1></body></html>")
			})
			server := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
			_ = server.ListenAndServe()
			return
		}
	}
	session := "fixture"
	sessionDir := ""
	development := false
	model, provider := "isolated-test", "fixture"
	for i, arg := range os.Args {
		if arg == "--model" && i+1 < len(os.Args) {
			model = os.Args[i+1]
		}
		if arg == "--provider" && i+1 < len(os.Args) {
			provider = os.Args[i+1]
		}
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
		data := map[string]any{"sessionId": session, "sessionFile": file, "isStreaming": false, "isCompacting": false, "pendingMessageCount": 0, "model": map[string]any{"id": model, "provider": provider, "contextWindow": 200000}}
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
			promptText := fmt.Sprint(c["message"])
			chunks := []string{fmt.Sprintf("[隔离协议测试] 已收到：%v", c["message"])}
			if strings.Contains(promptText, "[ix-native-confirm]") {
				resp := nativeDialog(out, scan, session, file, model, provider, map[string]any{
					"type": "extension_ui_request", "id": "fixture-native-confirm", "method": "confirm",
					"title": "原生确认请求", "message": "是否继续清理当前会话缓存？",
				})
				confirmed := "missing"
				if resp != nil {
					confirmed = fmt.Sprint(resp["confirmed"])
				}
				chunks = []string{"原生确认结果：" + confirmed}
			} else if strings.Contains(promptText, "[ix-native-empty]") {
				resp := nativeDialog(out, scan, session, file, model, provider, map[string]any{
					"type": "extension_ui_request", "id": "fixture-native-empty", "method": "input",
					"title": "原生空串输入", "placeholder": "可以留空",
				})
				value, present := "", false
				if resp != nil {
					raw, ok := resp["value"]
					present = ok
					value = fmt.Sprint(raw)
				}
				chunks = []string{fmt.Sprintf("原生空串结果：present=%v len=%d", present, len(value))}
			} else if strings.Contains(promptText, "[ix-native-expire]") {
				_ = out.Encode(map[string]any{
					"type": "extension_ui_request", "id": "fixture-native-expire", "method": "confirm",
					"title": "会超时的原生确认", "message": "两秒内未回应则失效", "timeout": 1500,
				})
				chunks = []string{"原生请求已超时"}
				time.Sleep(5 * time.Second)
			} else if strings.Contains(promptText, "[ix-six]") {
				chunks = []string{"需要你确认部署参数。<kaiwu-interaction>" + interactionFixture("部署参数确认") + "</kaiwu-interaction>"}
			} else if strings.Contains(promptText, "[ix-two]") {
				chunks = []string{"第二个问题。<kaiwu-interaction>" + secondInteractionFixture() + "</kaiwu-interaction>"}
			} else if strings.Contains(promptText, "关于「") {
				chunks = []string{"已收到本轮交互答复，原会话继续推进。"}
			}
			chatOnly := strings.Contains(promptText, "__CHAT_FOCUS_QA__")
			if development && !chatOnly {
				_ = out.Encode(map[string]any{"type": "tool_execution_start", "toolCallId": "fixture-write", "toolName": "write", "args": map[string]string{"path": "kaiwu-fixture-result.txt"}})
				err := os.WriteFile("kaiwu-fixture-result.txt", []byte(fmt.Sprint(c["message"])), 0600)
				_ = out.Encode(map[string]any{"type": "tool_execution_end", "toolCallId": "fixture-write", "toolName": "write", "result": map[string]string{"text": "fixed fixture file written"}, "isError": err != nil})

				if strings.Contains(fmt.Sprint(c["message"]), "静态网页产物") {
					_ = os.WriteFile("index.html", []byte(`<!doctype html><html><head><meta charset="utf-8"><title>静态预览网页</title><link rel="stylesheet" href="assets/style.css"></head><body><h1 id="static-title">开悟静态网页测试</h1><img id="static-img" src="assets/pixel.png" alt="pixel" width="10" height="10"><script src="assets/app.js"></script></body></html>`), 0600)
					_ = os.MkdirAll("assets", 0755)
					_ = os.WriteFile("assets/style.css", []byte(`#static-title { color: rgb(24, 112, 60); font-family: sans-serif; }`), 0600)
					_ = os.WriteFile("assets/app.js", []byte(`document.addEventListener('DOMContentLoaded', () => { document.body.dataset.jsReady = 'true'; });`), 0600)
					// 1x1 transparent png
					pngData := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}
					_ = os.WriteFile("assets/pixel.png", pngData, 0600)
				}
				if strings.Contains(fmt.Sprint(c["message"]), "命令网页产物") {
					self, _ := os.Executable()
					if self == "" {
						self = "fakepi"
					}
					manifestBytes, _ := json.Marshal(map[string]any{
						"version": 1,
						"command": []string{self, "--serve-preview", "{port}"},
						"path":    "/",
					})
					_ = os.WriteFile("kaiwu-preview.json", manifestBytes, 0600)
				}
				if strings.Contains(fmt.Sprint(c["message"]), "命令网页变更") {
					self, _ := os.Executable()
					if self == "" {
						self = "fakepi"
					}
					manifestBytes, _ := json.Marshal(map[string]any{
						"version": 1,
						"command": []string{self, "--serve-preview", "{port}", "--v2"},
						"path":    "/",
					})
					_ = os.WriteFile("kaiwu-preview.json", manifestBytes, 0600)
				}
			}
			if c["message"] == "__MARKDOWN_STREAM_FIXTURE__" {
				chunks = []string{"## 流式格式\n\n```go\npackage main\n", "func main() { println(\"ready\") }\n", "```\n\n**完成**"}
			}
			if strings.Contains(fmt.Sprint(c["message"]), "结构化登录恢复") {
				_ = out.Encode(map[string]any{"type": "tool_execution_start", "toolCallId": "fixture-check", "toolName": "bash", "args": map[string]string{"command": "fixture check"}})
				_ = out.Encode(map[string]any{"type": "tool_execution_end", "toolCallId": "fixture-check", "toolName": "bash", "result": map[string]string{"text": "PASS fixture"}, "isError": false})
				chunks = []string{`刷新恢复已实现，结果已整理。<kaiwu-report>{"summary":"登录恢复已实现，等待确认","stage":"awaiting_review","plan":["保存登录","验证恢复"],"results":["刷新自动恢复","退出撤销凭证"],"checks":[{"title":"登录恢复检查","claim":"passed","toolId":"fixture-check"}],"blockers":[],"next":["确认使用效果"],"question":""}</kaiwu-report>`}
			}
			if chatOnly {
				chunks = []string{"这是实现说明，不修改代码。\n\n```go\n", "fmt.Println(\"world\")\n```\n\n", "最后一段解释同样保留。"}
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
