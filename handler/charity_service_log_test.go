package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func newServiceLogCaller(t *testing.T) (*Gateway, string, int64) {
	t.Helper()
	gw, store := setupAuthGateway(t, "x")
	u, err := store.CreateUser("service-log", "service-log", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	key, err := store.SetCallerKey(u.ID)
	if err != nil {
		t.Fatalf("SetCallerKey: %v", err)
	}
	return gw, key, u.ID
}

func assertLatestLogService(t *testing.T, gw *Gateway, userID int64, errorCode, wantService string) {
	t.Helper()
	logs, err := gw.Store.ListRequestLogs(userID, 1)
	if err != nil {
		t.Fatalf("ListRequestLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("request logs=%d, want 1", len(logs))
	}
	if logs[0].ErrorCode != errorCode {
		t.Errorf("error_code=%q, want %q", logs[0].ErrorCode, errorCode)
	}
	if logs[0].Service != wantService {
		t.Errorf("service=%q, want %q", logs[0].Service, wantService)
	}
}

func TestCharityRequestLogsUseActualService(t *testing.T) {
	const charityModel = "[公益][general]test"
	charityBody := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"short"}]}`, charityModel)

	t.Run("rpm exceeded", func(t *testing.T) {
		gw, key, userID := newServiceLogCaller(t)
		setRPMSettings(t, gw, 100, 100, 1)
		gw.limiter.record(rpmClassC, userID, time.Now())

		rec := chatRequest(gw, key, charityBody)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want 403; body=%s", rec.Code, rec.Body.String())
		}
		assertLatestLogService(t, gw, userID, "rpm_exceeded", "general")
	})

	t.Run("charity disabled", func(t *testing.T) {
		gw, key, userID := newServiceLogCaller(t)

		rec := chatRequest(gw, key, charityBody)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status=%d, want 403; body=%s", rec.Code, rec.Body.String())
		}
		assertLatestLogService(t, gw, userID, "charity_disabled", "general")
	})

	t.Run("content too short", func(t *testing.T) {
		gw, key, userID := newServiceLogCaller(t)
		if _, err := gw.Store.UpsertAntiAbuseConfig("general", 1, 20, 0, 0, 1); err != nil {
			t.Fatalf("UpsertAntiAbuseConfig: %v", err)
		}
		gw.refreshAntiAbuseCache()

		rec := chatRequest(gw, key, charityBody)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		assertLatestLogService(t, gw, userID, "content_too_short", "general")
	})

	t.Run("ordinary model unchanged", func(t *testing.T) {
		gw, key, userID := newServiceLogCaller(t)
		body := `{"model":"[general]missing","messages":[{"role":"user","content":"long enough for this control request"}]}`

		rec := chatRequest(gw, key, body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
		assertLatestLogService(t, gw, userID, "model_not_found", "general")
	})
}
