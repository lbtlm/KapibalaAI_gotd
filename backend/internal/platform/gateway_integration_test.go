package platform_test

import (
	"context"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"os"
	"testing"
)

// Runs the production gateway HTTP handler rather than the transport fixture.
// The caller must supply an isolated fake-mode gateway, never a Telegram one.
func TestGatewayServiceGroupJobContract(t *testing.T) {
	url := os.Getenv("GATEWAY_INTEGRATION_URL")
	if url == "" {
		t.Skip("isolated production fake gateway URL required")
	}
	a, s := fixture(t, platform.Config{GatewayURL: url})
	a.Start(context.Background())
	token, _ := login(t, s, "admin")
	id := createGroup(t, s, token)
	st, b, _ := call(t, s, "GET", "/api/groups/"+id, token, "")
	if st != 200 || len(b["members"].([]any)) != 3 {
		t.Fatalf("production gateway group job %d %v", st, b)
	}
}
