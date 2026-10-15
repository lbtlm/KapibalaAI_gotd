package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/domain"
	"github.com/lbtlm/KapibalaAI_gotd/backend/internal/provider"
	"net/http/httptest"
	"os"
	"testing"
)

// This opt-in smoke calls the configured real model but never Telegram.
func TestLiveProviderToolConversationAndAudit(t *testing.T) {
	if os.Getenv("RUN_LIVE_PROVIDER") != "true" {
		t.Skip("explicit live-provider run required")
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Fatal("provider key not configured")
	}
	cfg := provider.Config{DatabaseURL: database(t), Mode: "openai", APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: os.Getenv("OPENAI_BASE_URL"), Model: os.Getenv("OPENAI_MODEL"), APIStyle: os.Getenv("OPENAI_API_STYLE")}
	p, e := provider.New(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	s := httptest.NewServer(p.Handler())
	defer s.Close()
	contextText := `{"groupId":"smoke","triggerMessages":[{"msgId":"m1","senderPlatformUserId":"outside","text":"这是一项授权的只读模型工具协议测试。请先调用get_recent_messages读取测试输入，再调用finish。禁止调用send_message或kick_user；不会连接Telegram或执行平台操作。","sentAt":"2026-10-14T14:00:00Z"}],"policy":{"autoKickEnabled":false},"ownPlatformUserIds":[]}`
	req := domain.TurnRequest{RunID: "live-smoke", Tools: domain.Tools(), Messages: []domain.ChatMessage{{Role: "user", Content: []domain.Block{{Type: "text", Text: contextText}}}}}
	sawRead, finished := false, false
	for n := 0; n < 6; n++ {
		st, out := post(t, s, "/agent/turn", req)
		if st != 200 || len(out.Content) != 1 {
			t.Fatalf("turn contract failed HTTP %d", st)
		}
		if out.StopReason == "end_turn" {
			finished = true
			break
		}
		b := out.Content[0]
		if code := domain.ValidateInput(b.Name, b.Input); code != "" {
			t.Fatalf("real model invalid tool %s", code)
		}
		t.Logf("turn %d tool=%s", n+1, b.Name)
		var result string
		switch b.Name {
		case "get_recent_messages":
			sawRead = true
			result = `{"messages":[{"msgId":"m1","senderPlatformUserId":"outside","isOwn":false,"text":"你好","sentAt":"2026-10-14T14:00:00Z"}],"truncated":false}`
		case "finish":
			finished = true
			result = `{"ok":true}`
		default:
			t.Fatalf("read-only provider smoke requested forbidden tool %s", b.Name)
		}
		req.Messages = append(req.Messages, domain.ChatMessage{Role: "assistant", Content: out.Content}, domain.ChatMessage{Role: "user", Content: []domain.Block{{Type: "tool_result", ToolUseID: b.ID, Content: result}}})
		if finished {
			break
		}
	}
	if !sawRead || !finished {
		t.Fatalf("tool conversation incomplete read=%t finish=%t", sawRead, finished)
	}
	body, _ := json.Marshal(map[string]any{"text": "你好，收到你的消息。", "groupId": "smoke"})
	res, e := s.Client().Post(s.URL+"/agent/audit", "application/json", bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var verdict struct {
		Verdict string `json:"verdict"`
	}
	if json.NewDecoder(res.Body).Decode(&verdict) != nil || res.StatusCode != 200 || verdict.Verdict != "pass" {
		t.Fatalf("live audit contract failed HTTP %d verdict=%s", res.StatusCode, verdict.Verdict)
	}
}
