package platform_test

import (
	"bufio"
	"context"
	"fmt"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/platform"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestServerProcessHelper(t *testing.T) {
	if os.Getenv("KAPIBALA_CHILD_SERVER") != "true" {
		t.Skip("subprocess helper")
	}
	cfg := platform.Config{DatabaseURL: os.Getenv("KAPIBALA_CHILD_DSN"), GatewayURL: os.Getenv("KAPIBALA_CHILD_GATEWAY"), AgentURL: os.Getenv("KAPIBALA_CHILD_AGENT")}
	a, e := platform.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if e = a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	fmt.Printf("CHILD_URL=http://%s\n", l.Addr())
	if e = http.Serve(l, a.Handler()); e != nil {
		t.Fatal(e)
	}
}

func childServer(t *testing.T, cfg platform.Config) (*exec.Cmd, *httptest.Server) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestServerProcessHelper$", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), "KAPIBALA_CHILD_SERVER=true", "KAPIBALA_CHILD_DSN="+cfg.DatabaseURL, "KAPIBALA_CHILD_GATEWAY="+cfg.GatewayURL, "KAPIBALA_CHILD_AGENT="+cfg.AgentURL)
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(out)
	var base string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "CHILD_URL=") {
			base = strings.TrimPrefix(scanner.Text(), "CHILD_URL=")
			break
		}
	}
	if base == "" {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("child did not start")
	}
	return cmd, &httptest.Server{URL: base}
}
