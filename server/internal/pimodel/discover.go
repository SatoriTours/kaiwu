package pimodel

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func credentialClient() *http.Client {
	return &http.Client{Timeout: 9 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("无效地址")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("无法解析接口地址")
		}
		for _, item := range ips {
			ip := item.IP
			if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
				return nil, errors.New("不允许的接口地址")
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("无法解析接口地址")
		}
		d := net.Dialer{Timeout: 5 * time.Second}
		return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 7 * time.Second, DisableKeepAlives: true}}
}
func (c *Connections) Discover(ctx context.Context, in ConnectionInput) ([]Model, error) {
	endpoint, err := normalizeEndpoint(in.BaseURL)
	if err != nil {
		return nil, err
	}
	if len(in.Key) > 16384 || strings.ContainsAny(in.Key, "\r\n") {
		return nil, errors.New("API Key 格式无效")
	}
	if in.ID != "" {
		c.mu.Lock()
		r, ok := c.find(in.ID)
		c.mu.Unlock()
		if !ok || r.Revision != in.Revision {
			return nil, ErrConnectionConflict
		}
		if r.Kind != "api_key" {
			return nil, errors.New("账号模型请通过 pi 模型列表刷新")
		}
		// A stored key must never be sent to a newly supplied origin or path.
		if in.Key == "" {
			if endpoint != r.BaseURL || in.API != r.API {
				return nil, errors.New("接口地址或协议变更后，请重新输入 API Key")
			}
			in.Key = r.Key
		}
	}
	if in.Key == "" {
		return nil, errors.New("请先填写 API Key")
	}
	u, _ := url.Parse(endpoint)
	switch in.API {
	case "openai-completions", "openai-responses":
		u.Path = strings.TrimRight(u.Path, "/") + "/models"
	case "anthropic-messages":
		if !strings.HasSuffix(u.Path, "/v1") {
			u.Path = strings.TrimRight(u.Path, "/") + "/v1"
		}
		u.Path += "/models"
	case "google-generative-ai":
		if u.Path == "" {
			u.Path = "/v1beta"
		}
		u.Path = strings.TrimRight(u.Path, "/") + "/models"
	default:
		return nil, errors.New("请选择受支持的接口协议")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, errors.New("模型获取请求无效")
	}
	req.Header.Set("Accept", "application/json")
	switch in.API {
	case "anthropic-messages":
		req.Header.Set("x-api-key", in.Key)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "google-generative-ai":
		req.Header.Set("x-goog-api-key", in.Key)
	default:
		req.Header.Set("Authorization", "Bearer "+in.Key)
	}
	client := credentialClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("无法获取模型，请检查地址、网络或凭据；也可手动添加")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("服务商未返回模型列表，请检查地址和 Key，或手动添加")
	}
	b, err := readLimited(response.Body, 1<<20)
	if err != nil {
		return nil, errors.New("模型列表过大或不完整")
	}
	var data struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"display_name"`
		} `json:"data"`
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &data) != nil || (data.Data == nil && data.Models == nil) {
		return nil, errors.New("接口不支持此格式的模型列表，可手动添加")
	}
	result := []Model{}
	seen := map[string]bool{}
	add := func(id, name string) {
		if len(result) >= 256 || !safeString(id, 300) || strings.HasPrefix(id, "!") || seen[id] {
			return
		}
		seen[id] = true
		if !safeString(name, 300) {
			name = id
		}
		result = append(result, Model{ID: id, Name: name})
	}
	for _, m := range data.Data {
		add(m.ID, m.Name)
	}
	for _, m := range data.Models {
		add(strings.TrimPrefix(m.Name, "models/"), m.DisplayName)
	}
	return result, nil
}
