package platform

import (
	"context"
	"testing"
)

// Test-only seams exercise durable file jobs without starting unrelated workers.
func DownloadMediaForTest(a *App, id string) error { return a.downloadMedia(context.Background(), id) }
func CleanMediaForTest(a *App) error               { return a.cleanMedia(context.Background()) }

func TestMediaURLRejectsForeignOriginsAndTraversal(t *testing.T) {
	for _, raw := range []string{"https://evil.test/media/a", "//evil.test/media/a", "/media/../secret", "/media/%2e%2e/secret", "/media/a?token=secret", "/media/a#fragment", "/accounts/a", "http://gateway:8081@evil.test/media/a"} {
		if _, e := mediaSource("http://gateway:8081", raw); e == nil {
			t.Fatalf("unsafe media URL accepted: %s", raw)
		}
	}
	for _, raw := range []string{"/media/tg-1_2", "http://gateway:8081/media/tg-1_2"} {
		if got, e := mediaSource("http://gateway:8081", raw); e != nil || got != "http://gateway:8081/media/tg-1_2" {
			t.Fatal(got, e)
		}
	}
}
