// Package pimodel queries pi's own model registry without sending a prompt.
package pimodel

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Prepare copies only authentication and model definitions, never extensions,
// settings or history. A conversation keeps its own credentials, including any
// OAuth refresh pi writes there. The personal source is never modified.
func Prepare(source, target, provider string) error {
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	for _, name := range []string{"auth.json", "models.json"} {
		dest := filepath.Join(target, name)
		if _, err := os.Lstat(dest); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		data := map[string]json.RawMessage{}
		if source != "" {
			f, err := os.Open(filepath.Join(source, name))
			if err != nil && !os.IsNotExist(err) {
				return errors.New("无法读取服务器 pi 凭据或模型配置")
			}
			if err == nil {
				b, e := io.ReadAll(io.LimitReader(f, (4<<20)+1))
				f.Close()
				if e != nil || len(b) > 4<<20 || json.Unmarshal(b, &data) != nil || data == nil {
					return errors.New("服务器 pi 凭据或模型配置无效")
				}
			}
		}
		if name == "models.json" {
			providers := map[string]json.RawMessage{}
			if b := data["providers"]; b != nil && json.Unmarshal(b, &providers) != nil {
				return errors.New("服务器 pi 模型配置无效")
			}
			if provider != "" {
				providers = onlyProvider(providers, provider)
			}
			b, _ := json.Marshal(providers)
			data = map[string]json.RawMessage{"providers": b}
		} else if provider != "" {
			data = onlyProvider(data, provider)
		}
		b, err := json.Marshal(data)
		if err != nil {
			return errors.New("服务器 pi 配置无效")
		}
		var values any
		_ = json.Unmarshal(b, &values)
		if hasCommand(values) {
			return errors.New("服务器 pi 配置含命令凭据；请先改用静态凭据或环境变量")
		}
		if err := os.WriteFile(dest, b, 0600); err != nil {
			return err
		}
	}
	return nil
}
func onlyProvider(data map[string]json.RawMessage, provider string) map[string]json.RawMessage {
	selected := map[string]json.RawMessage{}
	if value, ok := data[provider]; ok {
		selected[provider] = value
	}
	return selected
}
func hasCommand(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.HasPrefix(strings.TrimSpace(v), "!")
	case map[string]any:
		for _, x := range v {
			if hasCommand(x) {
				return true
			}
		}
	case []any:
		for _, x := range v {
			if hasCommand(x) {
				return true
			}
		}
	}
	return false
}

// Environment excludes personal runtime hooks and extensions. Provider API keys
// remain in the child environment and are never returned by the catalog API.
func Environment(home, agentDir string) []string {
	env := []string{"HOME=" + home, "PI_CODING_AGENT_DIR=" + agentDir, "PI_SKIP_VERSION_CHECK=1", "PI_TELEMETRY=0"}
	for _, key := range []string{"PATH", "SystemRoot", "COMSPEC", "PATHEXT", "TMPDIR", "TMP", "TEMP", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENROUTER_API_KEY"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}
