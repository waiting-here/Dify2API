package handler

import (
	"net/http"
	"strings"
	"testing"
)

func TestBodyLimit(t *testing.T) {
	gw, key, _ := setupRoutedUser(t, "http://127.0.0.1:1", "[general]x")
	limit := int64(gw.Config.MaxRequestBodyMB) << 20
	big := strings.Repeat("a", int(limit)+1024)
	body := `{"model":"[general]x","messages":[{"role":"user","content":"` + big + `"}]}`

	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body: %.200s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request_too_large") {
		t.Errorf("error should carry request_too_large: %s", rec.Body.String())
	}
}

func TestChatInFlightCap(t *testing.T) {
	var captured map[string]interface{}
	srv := mockDifyApp(t, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[general]x")

	// Saturate the semaphore manually (cap is 1 for the test).
	gw.chatSem = make(chan struct{}, 1)
	gw.chatSem <- struct{}{}

	body := `{"model":"[general]x","messages":[{"role":"user","content":"hi"}]}`
	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "server_busy") {
		t.Errorf("error should carry server_busy: %s", rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 should include Retry-After header")
	}
	if captured != nil {
		t.Error("saturated gateway must not forward to Dify")
	}
}
