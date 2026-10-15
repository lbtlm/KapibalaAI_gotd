package platform

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestDuplicatedAuthorizationKeepsSpecificCauseAndTerminalState(t *testing.T) {
	input := &externalError{Status: 401, Code: "AUTH_KEY_DUPLICATED"}
	var output *apiError
	if !errors.As(accountGatewayError(input), &output) || output.status != 401 || output.code != "AUTH_KEY_DUPLICATED" || safeAccountCode(input) != "AUTH_KEY_DUPLICATED" || terminalForError(input) != "session_expired" {
		t.Fatal("duplicated authorization lost its public cause or terminal status")
	}
}

func TestGatewayConnectionBudgetDoesNotCutOffReadiness(t *testing.T) {
	a := &App{cfg: Config{GatewayURL: "http://gateway"}, client: &http.Client{Timeout: 12 * time.Second}}
	if a.requestClient("http://gateway", "POST", "/accounts/one/connect").Timeout <= 30*time.Second {
		t.Fatal("outer request expires before Gateway connection readiness")
	}
	for _, path := range []string{"/groups/one/send", "/accounts/one/check", "/agent/turn"} {
		if a.requestClient("http://gateway", "POST", path).Timeout != 12*time.Second {
			t.Fatal("unrelated operation budget changed")
		}
	}
	if a.client.Timeout != 12*time.Second {
		t.Fatal("shared HTTP client was mutated")
	}
}

func TestSequenceGenerationBudgetIsScoped(t *testing.T) {
	a := &App{cfg: Config{AgentURL: "http://agent"}, client: &http.Client{Timeout: 12 * time.Second}}
	if a.requestClient("http://agent", "POST", "/sequences/generate").Timeout <= 50*time.Second || a.requestClient("http://agent", "POST", "/agent/turn").Timeout != 12*time.Second || a.client.Timeout != 12*time.Second {
		t.Fatal("generation must outlive model deadline without changing Agent turns")
	}
}

func TestGatewayTransportErrorsKeepTimeoutDistinctFromUnavailable(t *testing.T) {
	for _, tt := range []struct {
		err    error
		code   string
		status int
	}{
		{context.DeadlineExceeded, "NETWORK_TIMEOUT", 504},
		{&net.OpError{Op: "dial", Net: "tcp", Err: net.ErrClosed}, "GATEWAY_UNAVAILABLE", 503},
		{context.Canceled, "GATEWAY_UNAVAILABLE", 503},
	} {
		e := transportFailure(tt.err)
		if e.Code != tt.code || e.Status != tt.status {
			t.Fatalf("transport classification: got %s/%d", e.Code, e.Status)
		}
	}
}
