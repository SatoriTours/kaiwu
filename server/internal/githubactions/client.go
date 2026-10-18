// Package githubactions observes GitHub Actions read-only: exact run gates,
// failure evidence and sanitized logs. It never dispatches, reruns or cancels.
package githubactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SatoriTours/kaiwu/server/internal/workbench"
)

const (
	// Verified against the official REST docs on 2026-10-17; see
	// testdata/contracts.md for the exact quote and source.
	defaultAPIVersion = "2026-03-10"
	mediaType         = "application/vnd.github+json"
	apiBase           = "https://api.github.com"

	perPage        = 100
	maxPages       = 20
	requestTimeout = 15 * time.Second
	retryDelay     = 10 * time.Second
	defaultRetry   = retryDelay
	// LogReadLimit bounds a single log download; larger logs are truncated.
	LogReadLimit int64 = 8 << 20 // 8 MiB
	// SummaryLimit bounds the sanitized evidence handed to the model.
	SummaryLimit = 64 << 10 // 64 KiB
	// StageSummaryLimit bounds persisted stage summaries downstream.
	StageSummaryLimit = 256 << 10 // 256 KiB
)

// Observation gate states.
const (
	StateWaiting = "waiting"
	StatePassed  = "passed"
	StateFailed  = "failed"
	StateBlocked = "blocked"
)

type ClientConfig struct {
	HTTP       *http.Client
	APIVersion string
	// Token resolves a credential reference to a bearer token at request
	// time. Tokens are never stored on the client.
	Token func(context.Context, string) (string, error)
}

// APIError classifies HTTP and protocol failures for the delivery manager.
type APIError struct {
	State, Code string
	RetryAt     time.Time
	Status      int
	err         error
}

func (e *APIError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.err)
	}
	return e.Code
}

func (e *APIError) Unwrap() error { return e.err }

type cacheEntry struct {
	etag string
	body []byte
}

// Client is a read-only GitHub Actions REST client with a fixed API host.
type Client struct {
	http       *http.Client
	apiVersion string
	token      func(context.Context, string) (string, error)

	mu    sync.Mutex
	cache map[string]cacheEntry
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Token == nil {
		return nil, errors.New("缺少凭据解析器")
	}
	version := cfg.APIVersion
	if version == "" {
		version = defaultAPIVersion
	}
	if strings.ContainsAny(version, " \t\r\n") {
		return nil, errors.New("API 版本无效")
	}
	base := cfg.HTTP
	if base == nil {
		base = &http.Client{}
	}
	cl := *base
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 10 {
			return errors.New("重定向过多")
		}
		// Credentials never follow a redirect off the API host.
		if req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}
	return &Client{http: &cl, apiVersion: version, token: cfg.Token, cache: map[string]cacheEntry{}}, nil
}

// apiResponse is a decoded JSON body with its HTTP status.
type apiResponse struct {
	status      int
	header      http.Header
	content     []byte
	truncated   bool
	notModified bool
}

// fetch performs a GET against the fixed API host and returns a JSON body.
func (c *Client) fetch(ctx context.Context, credentialRef, path string, query url.Values, conditional bool) (*apiResponse, error) {
	resp, e := c.fetchBounded(ctx, credentialRef, path, query, 32<<20, conditional)
	if e != nil {
		return nil, e
	}
	resp.truncated = false
	return resp, nil
}

// fetchBounded performs a GET against the fixed API host with a bounded
// timeout and body read. A 304 returns the cached body unchanged.
func (c *Client) fetchBounded(ctx context.Context, credentialRef, path string, query url.Values, limit int64, conditional bool) (*apiResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	endpoint := apiBase + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	token, err := c.token(ctx, credentialRef)
	if err != nil {
		return nil, &APIError{State: StateBlocked, Code: "credential_unavailable", err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, &APIError{State: StateBlocked, Code: "protocol_error", err: err}
	}
	req.Header.Set("Accept", mediaType)
	req.Header.Set("X-GitHub-Api-Version", c.apiVersion)
	req.Header.Set("Authorization", "Bearer "+token)

	key := path + "?" + query.Encode()
	c.mu.Lock()
	entry, cached := c.cache[key]
	if len(c.cache) > 128 {
		c.cache = map[string]cacheEntry{}
	}
	c.mu.Unlock()
	cacheable := limit > LogReadLimit // only JSON endpoints use ETags
	if conditional && cached && cacheable {
		req.Header.Set("If-None-Match", entry.etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &APIError{State: StateBlocked, Code: "network_error", RetryAt: time.Now().Add(defaultRetry), err: err}
	}
	defer resp.Body.Close()

	if conditional && cached && resp.StatusCode == http.StatusNotModified {
		return &apiResponse{status: http.StatusOK, header: resp.Header, content: entry.body, notModified: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, classifyStatus(resp)
	}
	// Read limit+1 bytes to detect truncation exactly.
	content, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, &APIError{State: StateBlocked, Code: "network_error", RetryAt: time.Now().Add(defaultRetry), err: err}
	}
	truncated := int64(len(content)) > limit
	if truncated {
		content = content[:limit]
	}
	if cacheable {
		c.mu.Lock()
		c.cache[key] = cacheEntry{etag: resp.Header.Get("ETag"), body: content}
		c.mu.Unlock()
	}
	return &apiResponse{status: resp.StatusCode, header: resp.Header, content: content, truncated: truncated}, nil
}

func classifyStatus(resp *http.Response) error {
	code := "server_error"
	retry := time.Time{}
	switch {
	case resp.StatusCode == 401:
		code = "auth_failed"
	case resp.StatusCode == 403:
		code = "forbidden"
	case resp.StatusCode == 404:
		code = "not_found"
	case resp.StatusCode == 429:
		code = "rate_limited"
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if seconds, e := strconv.Atoi(ra); e == nil && seconds > 0 {
				retry = time.Now().Add(time.Duration(seconds) * time.Second)
			}
		}
	case resp.StatusCode >= 500:
		code = "server_error"
		retry = time.Now().Add(defaultRetry)
	default:
		code = "protocol_error"
	}
	if retry.IsZero() && (resp.StatusCode == 429 || resp.StatusCode >= 500) {
		retry = time.Now().Add(defaultRetry)
	}
	return &APIError{State: StateBlocked, Code: code, RetryAt: retry, Status: resp.StatusCode}
}

// page carries Link-header pagination state.
type page struct {
	next int
}

func parsePage(header http.Header) page {
	link := header.Get("Link")
	if link == "" {
		return page{}
	}
	for _, part := range strings.Split(link, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start < 0 || end < start {
			continue
		}
		u, e := url.Parse(strings.TrimSpace(part[start+1 : end]))
		if e != nil {
			continue
		}
		n, e := strconv.Atoi(u.Query().Get("page"))
		if e != nil {
			continue
		}
		return page{next: n}
	}
	return page{}
}

func decode(content []byte, v interface{}) error {
	if e := json.Unmarshal(content, v); e != nil {
		return &APIError{State: StateBlocked, Code: "protocol_error", err: e}
	}
	return nil
}

// hasRequiredJobs reports whether the workflow requires specific job gates.
func hasRequiredJobs(wf workbench.DeliveryWorkflow) bool { return len(wf.Jobs) > 0 }
