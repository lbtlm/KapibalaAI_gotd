package gateway

import (
	"context"
	"testing"

	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
)

// Pure SDK error-boundary input: no Telegram client, session or simulated RPC.
func TestSDKTerminalInitializationFailureStopsRetryAndKeepsCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := &ingestGate{cancel: cancel}
	logger := startupLogger("diagnostic-input", gate.fail)
	logger.Info("Restarting connection", zap.Error(&tgerr.Error{Code: 406, Type: "AUTH_KEY_DUPLICATED"}))
	if ctx.Err() == nil {
		t.Fatal("SDK kept retrying a revoked authorization")
	}
	if e := platformError(gate.err()); e == nil || e.Code != "AUTH_KEY_DUPLICATED" || e.Status != 401 {
		t.Fatal("authorization failure lost its specific cause", e)
	}
	s := &Service{state: initialState()}
	s.state.Accounts["a"] = &account{Status: "idle", ConnectionID: "cid"}
	s.accountFailure("a", gate.err())
	if s.state.Accounts["a"].Status != "session_expired" {
		t.Fatal("revoked account was not terminal")
	}
}

func TestSDKNetworkRetryDoesNotExpireAuthorization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := &ingestGate{cancel: cancel}
	logger := startupLogger("diagnostic-input", gate.fail)
	logger.Info("Restarting connection", zap.Error(context.DeadlineExceeded))
	if ctx.Err() != nil || gate.err() != nil {
		t.Fatal("temporary network error invalidated authorization")
	}
}
