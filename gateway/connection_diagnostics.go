package gateway

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"

	"github.com/gotd/td/tgerr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// SDK fields can include authorization material. Only classify errors; never
// forward fields, usernames, keys, message bodies or packet dumps to logs.
type connectionDiagnostics struct {
	accountID string
	onFailure func(error)
}

var diagnosticRPCType = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,80}$`)

func (d connectionDiagnostics) Enabled(l zapcore.Level) bool        { return l >= zapcore.DebugLevel }
func (d connectionDiagnostics) With(_ []zapcore.Field) zapcore.Core { return d }
func (d connectionDiagnostics) Check(e zapcore.Entry, c *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if d.Enabled(e.Level) {
		return c.AddCore(e, d)
	}
	return c
}
func (d connectionDiagnostics) Sync() error { return nil }
func (d connectionDiagnostics) Write(e zapcore.Entry, fields []zapcore.Field) error {
	stage := map[string]string{"Initializing": "initializing", "Retrying connection initialization": "initialization_retry", "SessionInit": "session_initialized", "Connection dead": "transport_dead", "Starting": "starting", "Connection restored from state": "session_restored", "Ready": "transport_ready", "Got self": "identity_ready", "Restarting connection": "transport_retry", "Got error on self": "identity_failed", "Closed": "closed"}[e.Message]
	if stage == "" {
		return nil
	}
	code := ""
	for _, f := range fields {
		if err, ok := f.Interface.(error); ok {
			code = connectionFailureClass(err)
			// gotd v0.112 retries initConnection RPC failures internally and
			// otherwise exposes only cancellation to Run's caller. Stop the
			// managed lifetime on explicit revoked/disabled authorization.
			if d.onFailure != nil {
				switch platformError(err).Code {
				case "AUTH_KEY_DUPLICATED", "SESSION_EXPIRED", "ACCOUNT_SUSPENDED":
					d.onFailure(err)
				}
			}
		}
	}
	slog.Info("Telegram connection stage", "accountId", d.accountID, "stage", stage, "cause", code)
	return nil
}
func connectionFailureClass(err error) string {
	if rpc, ok := tgerr.As(err); ok {
		if diagnosticRPCType.MatchString(rpc.Type) {
			return rpc.Type
		}
		return "TELEGRAM_RPC_ERROR"
	}
	var network *net.OpError
	if errors.As(err, &network) {
		if network.Timeout() {
			return "NETWORK_TIMEOUT"
		}
		return "NETWORK_" + strings.ToUpper(network.Op)
	}
	for _, term := range []string{"context deadline exceeded", "context canceled", "bad message", "bad server salt", "auth key not found", "unexpected EOF", "EOF"} {
		if strings.Contains(err.Error(), term) {
			return strings.ToUpper(strings.ReplaceAll(term, " ", "_"))
		}
	}
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	return fmt.Sprintf("%T", err)
}
func startupLogger(id string, onFailure func(error)) *zap.Logger {
	return zap.New(connectionDiagnostics{accountID: id, onFailure: onFailure})
}
