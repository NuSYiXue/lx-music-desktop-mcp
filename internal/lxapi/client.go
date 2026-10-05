// Package lxapi 封装 LX Music Desktop 的 Open API。
//
// 本包只做两件事，而且是刻意只做这两件事：
//
//  1. 把 HTTP 端点包装成方法，响应体一律以 json.RawMessage 原样返回，
//     绝不解析后重组。因为 MusicInfo 的 meta 字段集因音源而异
//     （kg 带 hash、tx 带 strMediaMid、mg 带 copyrightId/lrcUrl），
//     少一个字段就会导致播放静默失败。
//
//  2. 全局限流。LX 的 Open API 服务端是单线程的，超过约 5 个并发连接
//     会偶发 ECONNRESET / socket hang up。限流放在客户端层，
//     这样无论上层怎么并发调用都不会打爆服务端。
package lxapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL 是 LX Open API 的默认地址。
const DefaultBaseURL = "http://127.0.0.1:23330"

const (
	requestTimeout = 15 * time.Second
	maxAttempts    = 3
	retryDelay     = 200 * time.Millisecond
	maxConcurrency = 5
)

// ErrUnreachable 表示连不上 LX Music 的 Open API 服务。
var ErrUnreachable = errors.New("无法连接 LX Music 的 Open API 服务")

// APIError 是服务端返回的非 2xx 响应。
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *APIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		body = "(空响应体)"
	}
	return fmt.Sprintf("Open API %s %s 返回 HTTP %d：%s", e.Method, e.Path, e.StatusCode, body)
}

// Client 是 Open API 的 HTTP 客户端，可安全并发使用。
type Client struct {
	base string
	hc   *http.Client
	sem  chan struct{}
}

// New 创建客户端。baseURL 为空时使用 DefaultBaseURL。
func New(baseURL string) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{
		base: base,
		hc:   &http.Client{Timeout: requestTimeout},
		sem:  make(chan struct{}, maxConcurrency),
	}
}

// BaseURL 返回当前使用的服务地址。
func (c *Client) BaseURL() string { return c.base }

// request 描述一次调用。
type request struct {
	method string
	path   string
	query  url.Values
	body   any
}

// do 执行一次调用，带网络抖动重试。返回值是原始响应字节。
func (c *Client) do(ctx context.Context, r request) ([]byte, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	target := c.base + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}

	var payload []byte
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败：%w", err)
		}
		payload = b
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, r.method, target, reader)
		if err != nil {
			return nil, err
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.hc.Do(req)
		if err != nil {
			if isRetryable(err) {
				lastErr = err
				continue
			}
			return nil, unreachable(c.base, err)
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			if isRetryable(readErr) {
				lastErr = readErr
				continue
			}
			return nil, unreachable(c.base, readErr)
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Method:     r.method,
				Path:       r.path,
				Body:       string(body),
			}
		}
		return body, nil
	}

	return nil, unreachable(c.base, lastErr)
}

func (c *Client) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) release() { <-c.sem }

// unreachable 把底层网络错误包装成一句人和模型都能照着做的提示。
func unreachable(base string, err error) error {
	return fmt.Errorf(
		"%w（%s）：%v。请确认：LX Music 正在运行；已在设置中开启 Open API 服务；"+
			"若改过端口，用环境变量 LX_OPEN_API 指定完整地址（如 http://127.0.0.1:12345）",
		ErrUnreachable, base, err,
	)
}

// isRetryable 判断是否值得重试：只针对连接被重置一类的瞬时故障。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, frag := range []string{"connection reset", "broken pipe", "unexpected eof", "eof"} {
		if strings.Contains(s, frag) {
			return true
		}
	}
	return false
}
