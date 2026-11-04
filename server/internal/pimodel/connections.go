package pimodel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

var ErrConnectionConflict = errors.New("连接已被修改，请刷新后重试")

type Connection struct {
	ID       string   `json:"id"`
	Revision int      `json:"revision"`
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	API      string   `json:"api"`
	BaseURL  string   `json:"baseUrl"`
	Models   []string `json:"models"`
	Kind     string   `json:"kind"`
	HasKey   bool     `json:"hasKey"`
}
type ConnectionInput struct {
	ID       string   `json:"id,omitempty"`
	Revision int      `json:"revision,omitempty"`
	Name     string   `json:"name"`
	API      string   `json:"api"`
	BaseURL  string   `json:"baseUrl"`
	Key      string   `json:"key,omitempty"`
	Models   []string `json:"models"`
}
type connectionRecord struct {
	Connection
	Key  string          `json:"key,omitempty"`
	Auth json.RawMessage `json:"auth,omitempty"`
}
type connectionFile struct {
	Version int                `json:"version"`
	HostID  string             `json:"hostId"`
	Records []connectionRecord `json:"records"`
}
type Connections struct {
	mu          sync.Mutex
	root        string
	data        connectionFile
	pending     map[string]oauthPending
	oauthClient *http.Client
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func OpenConnections(root string) (*Connections, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, errors.New("无法准备连接配置目录")
	}
	c := &Connections{root: root, data: connectionFile{Version: 1, HostID: randomID(), Records: []connectionRecord{}}, pending: map[string]oauthPending{}}
	path := filepath.Join(root, "pi-connections.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return nil, errors.New("连接配置文件无效")
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &c.data) != nil || c.data.Version != 1 || len(c.data.HostID) != 32 || len(c.data.Records) > 32 {
		return nil, errors.New("无法读取连接配置")
	}
	if _, err = hex.DecodeString(c.data.HostID); err != nil {
		return nil, errors.New("连接配置身份无效")
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, errors.New("无法保护连接配置")
	}
	return c, nil
}
func cloneConnection(c Connection) Connection { c.Models = append([]string{}, c.Models...); return c }
func (c *Connections) List() []Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Connection, 0, len(c.data.Records))
	for _, r := range c.data.Records {
		result = append(result, cloneConnection(r.Connection))
	}
	return result
}
func (c *Connections) find(id string) (connectionRecord, bool) {
	for _, r := range c.data.Records {
		if r.ID == id {
			return r, true
		}
	}
	return connectionRecord{}, false
}
func safeString(s string, n int) bool {
	return s != "" && len(s) <= n && !strings.ContainsFunc(s, unicode.IsControl)
}
func normalizeEndpoint(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(value, "\r\n\\") {
		return "", errors.New("请输入不含账号、查询参数或片段的 HTTP(S) 接口基础地址")
	}
	host := strings.ToLower(u.Hostname())
	if host == "metadata.google.internal" {
		return "", errors.New("不允许访问元数据地址")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return "", errors.New("不允许访问元数据或链路本地地址")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}
func validateInput(in ConnectionInput) (ConnectionInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !safeString(in.Name, 80) {
		return in, errors.New("连接名称不能为空且最多 80 字节")
	}
	switch in.API {
	case "openai-completions", "openai-responses", "anthropic-messages", "google-generative-ai":
	default:
		return in, errors.New("请选择受支持的接口协议")
	}
	var err error
	in.BaseURL, err = normalizeEndpoint(in.BaseURL)
	if err != nil {
		return in, err
	}
	if len(in.Key) > 16384 || strings.ContainsFunc(in.Key, unicode.IsControl) {
		return in, errors.New("API Key 格式或长度无效")
	}
	if len(in.Models) == 0 || len(in.Models) > 128 {
		return in, errors.New("请至少选择一个模型，最多 128 个")
	}
	seen := map[string]bool{}
	models := []string{}
	for _, id := range in.Models {
		id = strings.TrimSpace(id)
		if !safeString(id, 300) || strings.HasPrefix(id, "!") {
			return in, errors.New("模型 ID 格式无效")
		}
		if !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	in.Models = models
	return in, nil
}
func (c *Connections) commit(records []connectionRecord) error {
	next := c.data
	next.Records = records
	b, err := json.Marshal(next)
	if err != nil {
		return errors.New("无法保存连接")
	}
	f, err := os.CreateTemp(c.root, ".pi-connections-")
	if err != nil {
		return errors.New("无法保存连接")
	}
	defer os.Remove(f.Name())
	err = f.Chmod(0600)
	if err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(c.root, "pi-connections.json"))
	}
	if err != nil {
		return errors.New("连接未保存，请检查存储状态")
	}
	c.data = next
	if dir, e := os.Open(c.root); e == nil {
		_ = dir.Sync()
		dir.Close()
	}
	return nil
}
func (c *Connections) Save(in ConnectionInput) (Connection, error) {
	in, err := validateInput(in)
	if err != nil {
		return Connection{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r, exists := c.find(in.ID)
	if in.ID != "" && (!exists || r.Revision != in.Revision) {
		return Connection{}, ErrConnectionConflict
	}
	if exists && r.Kind != "api_key" {
		return Connection{}, errors.New("账号连接请使用重新授权")
	}
	if !exists {
		if len(c.data.Records) >= 32 {
			return Connection{}, errors.New("最多保存 32 个连接")
		}
		r.Connection = Connection{ID: randomID(), Kind: "api_key"}
		r.Provider = "kaiwu-" + r.ID
	}
	if exists && in.Key == "" && (in.BaseURL != r.BaseURL || in.API != r.API) {
		return Connection{}, errors.New("接口地址或协议变更后，请重新输入 API Key")
	}
	if in.Key != "" {
		r.Key = in.Key
	}
	if r.Key == "" {
		return Connection{}, errors.New("请输入 API Key")
	}
	r.Name = in.Name
	r.API = in.API
	r.BaseURL = in.BaseURL
	r.Models = in.Models
	r.HasKey = true
	r.Revision++
	records := append([]connectionRecord{}, c.data.Records...)
	if exists {
		for i := range records {
			if records[i].ID == r.ID {
				records[i] = r
			}
		}
	} else {
		records = append(records, r)
	}
	if err = c.commit(records); err != nil {
		return Connection{}, err
	}
	return cloneConnection(r.Connection), nil
}
func (c *Connections) Remove(id string, revision int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.find(id)
	if !ok || r.Revision != revision {
		return ErrConnectionConflict
	}
	records := make([]connectionRecord, 0, len(c.data.Records)-1)
	for _, r := range c.data.Records {
		if r.ID != id {
			records = append(records, r)
		}
	}
	return c.commit(records)
}

// Literal credentials must never become pi commands or environment substitutions.
func literalKey(key string) string {
	key = strings.ReplaceAll(key, "$", "$$")
	if strings.HasPrefix(key, "!") {
		key = "$!" + key[1:]
	}
	return key
}

func (c *Connections) Prepare(source, target, provider string) error {
	c.mu.Lock()
	records := append([]connectionRecord{}, c.data.Records...)
	c.mu.Unlock()
	// Existing session files are immutable snapshots, including OAuth refreshes.
	allExist := true
	for _, name := range []string{"auth.json", "models.json"} {
		if info, e := os.Lstat(filepath.Join(target, name)); e == nil {
			if !info.Mode().IsRegular() {
				return errors.New("会话凭据文件归属异常")
			}
		} else if os.IsNotExist(e) {
			allExist = false
		} else {
			return errors.New("无法检查会话凭据")
		}
	}
	if allExist {
		return nil
	}
	if strings.HasPrefix(provider, "kaiwu-") {
		found := false
		for _, r := range records {
			found = found || r.Provider == provider
		}
		if !found {
			return errors.New("连接已被移除，请重新选择模型")
		}
	}
	stage, err := os.MkdirTemp(c.root, ".pi-config-")
	if err != nil {
		return errors.New("无法准备连接配置")
	}
	defer os.RemoveAll(stage)
	if err = Prepare(source, stage, provider); err != nil {
		return err
	}
	auth := map[string]json.RawMessage{}
	modelFile := map[string]json.RawMessage{}
	ab, _ := os.ReadFile(filepath.Join(stage, "auth.json"))
	mb, _ := os.ReadFile(filepath.Join(stage, "models.json"))
	_ = json.Unmarshal(ab, &auth)
	_ = json.Unmarshal(mb, &modelFile)
	providers := map[string]json.RawMessage{}
	_ = json.Unmarshal(modelFile["providers"], &providers)
	for _, r := range records {
		if provider != "" && r.Provider != provider {
			continue
		}
		if r.Kind == "oauth" {
			auth[r.Provider] = r.Auth
			// An owned native OAuth credential must use its native destination,
			// never inherit a personal proxy/header override from another account.
			delete(providers, r.Provider)
			continue
		}
		auth[r.Provider], _ = json.Marshal(map[string]string{"type": "api_key", "key": literalKey(r.Key)})
		models := []map[string]any{}
		for _, id := range r.Models {
			models = append(models, map[string]any{"id": id, "name": id})
		}
		providers[r.Provider], _ = json.Marshal(map[string]any{"name": r.Name, "api": r.API, "baseUrl": r.BaseURL, "authHeader": r.API == "openai-completions" || r.API == "openai-responses", "models": models})
	}
	p, _ := json.Marshal(providers)
	modelFile["providers"] = p
	if err = os.MkdirAll(target, 0700); err != nil {
		return errors.New("无法准备会话凭据")
	}
	for name, value := range map[string]any{"auth.json": auth, "models.json": modelFile} {
		b, _ := json.Marshal(value)
		f, e := os.OpenFile(filepath.Join(target, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(e) {
			continue
		}
		if e != nil {
			return errors.New("无法写入会话凭据")
		}
		_, e = f.Write(b)
		ce := f.Close()
		if e != nil || ce != nil {
			return errors.New("无法写入会话凭据")
		}
	}
	return nil
}
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("响应过大或不完整")
	}
	return b, nil
}
