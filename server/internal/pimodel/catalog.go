package pimodel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/pirpc"
)

type Model struct {
	Provider      string `json:"provider"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Reasoning     bool   `json:"reasoning"`
	ContextWindow int    `json:"contextWindow"`
}
type Catalog struct {
	mu                          sync.Mutex
	Program, SourceDir, DataDir string
	models                      []Model
	expires                     time.Time
}

func (c *Catalog) InvalidateCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expires = time.Time{}
}

func (c *Catalog) List(ctx context.Context) ([]Model, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Program == "" {
		return nil, errors.New("服务未找到 pi，请将 pi 加入服务 PATH 或设置 KAIWU_PI_PROGRAM 后重启")
	}
	if time.Now().Before(c.expires) {
		return append([]Model{}, c.models...), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, errors.New("无法准备模型检测目录")
	}
	root, err := os.MkdirTemp(c.DataDir, "model-probe-")
	if err != nil {
		return nil, errors.New("无法准备模型检测目录")
	}
	remove := true
	defer func() {
		if remove {
			_ = os.RemoveAll(root)
		}
	}()
	agentDir, home := filepath.Join(root, "agent"), filepath.Join(root, "home")
	if err = os.MkdirAll(home, 0700); err != nil {
		return nil, errors.New("无法准备模型检测目录")
	}
	if err = Prepare(c.SourceDir, agentDir, ""); err != nil {
		return nil, err
	}
	spec := pirpc.LaunchSpec{Program: c.Program, Dir: root, Env: Environment(home, agentDir), OwnProcessGroup: pirpc.ProcessGroupsSupported(), Args: []string{"--mode", "rpc", "--no-session", "--no-tools", "--no-extensions", "--no-mcp", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve", "--offline"}}
	client, err := pirpc.Start(ctx, spec, pirpc.Options{StartupTimeout: 8 * time.Second, ShutdownTimeout: time.Second, DrainTimeout: time.Second})
	if client != nil {
		defer func() {
			stop, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			_ = client.Close(stop)
			result, e := client.Wait(stop)
			remove = e == nil && result.Reaped && result.CleanupError == nil
		}()
	}
	if err != nil {
		return nil, errors.New("pi 模型检测启动失败；请检查 pi 及其运行时是否位于服务 PATH")
	}
	response, err := client.Call(ctx, pirpc.Command{Type: "get_available_models"})
	var data struct {
		Models []Model `json:"models"`
	}
	if err != nil || json.Unmarshal(response.Data, &data) != nil || data.Models == nil {
		return nil, errors.New("pi 未返回有效模型列表，请检查版本和服务器登录配置")
	}
	models := []Model{}
	seen := map[string]bool{}
	for _, model := range data.Models {
		key := model.Provider + "\x00" + model.ID
		if model.Provider == "" || model.ID == "" || len(model.Provider) > 200 || len(model.ID) > 300 || seen[key] {
			continue
		}
		seen[key] = true
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider == models[j].Provider {
			return models[i].ID < models[j].ID
		}
		return models[i].Provider < models[j].Provider
	})
	c.models, c.expires = models, time.Now().Add(30*time.Second)
	return append([]Model{}, models...), nil
}
