package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type externalError struct {
	Status            int
	Code              string
	RetryAfterSeconds float64
	Body              string
}

func (e *externalError) Error() string { return e.Code }
func externalCode(e error) string {
	var x *externalError
	if errors.As(e, &x) {
		return x.Code
	}
	return "NETWORK_TIMEOUT"
}
func (a *App) gateway(ctx context.Context, method, path string, input, output any) error {
	return a.request(ctx, a.cfg.GatewayURL, method, path, input, output)
}
func (a *App) requestClient(base, method, path string) *http.Client {
	if base == a.cfg.AgentURL && method == http.MethodPost && (path == "/sequences/generate" || path == "/agent/reply-preview") {
		client := *a.client
		client.Timeout = 60 * time.Second
		return &client
	}
	if base == a.cfg.GatewayURL && method == http.MethodPost && (path == "/groups/import" || strings.HasPrefix(path, "/accounts/") && strings.HasSuffix(path, "/connect")) {
		client := *a.client
		client.Timeout = 40 * time.Second // Connection startup or multi-account import verification.
		return &client
	}
	return a.client
}
func transportFailure(err error) *externalError {
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return &externalError{Status: 504, Code: "NETWORK_TIMEOUT"}
	}
	return &externalError{Status: 503, Code: "GATEWAY_UNAVAILABLE"}
}
func (a *App) request(ctx context.Context, base, method, path string, input, output any) error {
	if input == nil && method == http.MethodPost {
		input = map[string]any{}
	}
	var body io.Reader
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, base+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := a.requestClient(base, method, path).Do(req)
	if e != nil {
		return transportFailure(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if e != nil {
		return transportFailure(e)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var v struct {
			Code  string  `json:"code"`
			Retry float64 `json:"retryAfterSeconds"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		json.Unmarshal(b, &v)
		if v.Code == "" {
			v.Code = v.Error.Code
		}
		if v.Code == "" {
			v.Code = "GATEWAY_UNAVAILABLE"
		}
		return &externalError{res.StatusCode, v.Code, v.Retry, string(b)}
	}
	if output != nil && json.Unmarshal(b, output) != nil {
		return fail(502, "GATEWAY_PROTOCOL_ERROR")
	}
	return nil
}
