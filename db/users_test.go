package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestUsers_CRUD(t *testing.T) {
	st, _ := openTemp(t)

	u, err := st.CreateUser("111", "alice", "ava1")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 || u.Username != "alice" || u.IsAdmin || u.Disabled {
		t.Errorf("user = %+v", u)
	}

	got, err := st.GetUserByDiscordID("111")
	if err != nil || got == nil {
		t.Fatalf("GetUserByDiscordID: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("got id %d, want %d", got.ID, u.ID)
	}

	missing, err := st.GetUserByDiscordID("999")
	if err != nil || missing != nil {
		t.Errorf("missing user should be (nil,nil), got %v %v", missing, err)
	}

	if err := st.SetUserDisabled(u.ID, true, "test ban"); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}
	got, _ = st.GetUserByID(u.ID)
	if !got.Disabled {
		t.Error("user should be disabled")
	}
}

func TestEnsureAdminUser(t *testing.T) {
	st, _ := openTemp(t)

	a1, err := st.EnsureAdminUser("root")
	if err != nil {
		t.Fatalf("EnsureAdminUser: %v", err)
	}
	if !a1.IsAdmin || a1.DiscordID != AdminDiscordID || a1.Username != "root" {
		t.Errorf("admin = %+v", a1)
	}

	// Second call updates username, keeps the same row.
	a2, err := st.EnsureAdminUser("root2")
	if err != nil {
		t.Fatalf("EnsureAdminUser 2: %v", err)
	}
	if a2.ID != a1.ID || a2.Username != "root2" {
		t.Errorf("admin2 = %+v", a2)
	}
}

func TestListAndDeleteUsers(t *testing.T) {
	st, _ := openTemp(t)
	st.EnsureAdminUser("root")
	st.CreateUser("1", "u1", "")
	st.CreateUser("2", "u2", "")
	u2, _ := st.GetUserByDiscordID("2")

	users, err := st.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 { // admin excluded
		t.Errorf("ListUsers = %d, want 2 (admin excluded)", len(users))
	}

	if err := st.DeleteUser(u2.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	gone, _ := st.GetUserByID(u2.ID)
	if gone != nil {
		t.Error("user should be deleted")
	}
}

func TestSessions(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("1", "u1", "")

	token, expiresAt, err := st.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if token == "" || time.Until(expiresAt) < 6*24*time.Hour {
		t.Errorf("token/expiry wrong")
	}

	got, err := st.GetSessionUser(token)
	if err != nil || got == nil {
		t.Fatalf("GetSessionUser: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("session user id = %d, want %d", got.ID, u.ID)
	}

	unknown, err := st.GetSessionUser("nope")
	if err != nil || unknown != nil {
		t.Errorf("unknown token should be (nil,nil)")
	}

	if err := st.DeleteSession(token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got, _ := st.GetSessionUser(token); got != nil {
		t.Error("session should be deleted")
	}

	// DeleteUserSessions clears all tokens.
	t1, _, _ := st.CreateSession(u.ID)
	t2, _, _ := st.CreateSession(u.ID)
	st.DeleteUserSessions(u.ID)
	if got, _ := st.GetSessionUser(t1); got != nil {
		t.Error("t1 should be gone")
	}
	if got, _ := st.GetSessionUser(t2); got != nil {
		t.Error("t2 should be gone")
	}
}

func TestSettings(t *testing.T) {
	st, _ := openTemp(t)

	if v, _ := st.GetSetting(SettingGuildID); v != "" {
		t.Errorf("unset setting = %q, want \"\"", v)
	}
	if err := st.SetSetting(SettingGuildID, "g1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := st.SetSetting(SettingGuildID, "g2"); err != nil {
		t.Fatalf("SetSetting upsert: %v", err)
	}
	v, err := st.GetSetting(SettingGuildID)
	if err != nil || v != "g2" {
		t.Errorf("setting = %q, want g2 (err %v)", v, err)
	}
}

func TestDeleteUser_WithDonationApplication(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("999", "u_da", "")

	// Create a donation application for this user.
	deadline := time.Now().Add(48 * time.Hour).Unix()
	_, err := st.CreateDonationApplication(u.ID, "general", "claude-opus-4-6",
		"https://dify.example.com/v1", "app-secret-key", 100, deadline, 10, "test")
	if err != nil {
		t.Fatalf("CreateDonationApplication: %v", err)
	}

	// DeleteUser should succeed (no FK violation).
	if err := st.DeleteUser(u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// Confirm user is gone.
	gone, err := st.GetUserByID(u.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if gone != nil {
		t.Error("user should be deleted after DeleteUser")
	}
}

func TestDeleteUser_AnonymizesDonationSource(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("998", "u_src", "")

	// Create a donation with source_user_id pointing to this user.
	d := &Donation{
		Service:         "general",
		Model:           "claude-opus-4-6",
		DifyBaseURL:     "https://dify.example.com/v1",
		TotalCount:      100,
		RemainingCount:  100,
		Deadline:        time.Now().Add(48 * time.Hour).Unix(),
		RpmLimit:        10,
		SourceUserID:    sql.NullInt64{Int64: u.ID, Valid: true},
		SourceDiscordID: "998",
		SourceUsername:  "u_src",
		Status:          DonationActive,
	}
	don, err := st.CreateDonation(d, "app-secret-key")
	if err != nil {
		t.Fatalf("CreateDonation: %v", err)
	}

	if err := st.DeleteUser(u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// Verify donation source info is anonymized.
	don2, err := st.GetDonation(don.ID)
	if err != nil {
		t.Fatalf("GetDonation: %v", err)
	}
	if don2.SourceUserID.Valid {
		t.Error("SourceUserID should be NULL after DeleteUser")
	}
	if don2.SourceDiscordID != "" {
		t.Errorf("SourceDiscordID = %q, want empty", don2.SourceDiscordID)
	}
	if don2.SourceUsername != "" {
		t.Errorf("SourceUsername = %q, want empty", don2.SourceUsername)
	}
}

func TestOpenMigratesIsAdmin(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "m.db")
	keyPath := filepath.Join(dir, "m.key")

	st1, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Simulate a legacy row (no is_admin awareness) then reopen: the idempotent
	// ALTER must not fail.
	if _, err := st1.db.Exec(`INSERT INTO users (discord_id, username, created_at, updated_at) VALUES ('x','y',1,1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	st1.Close()

	st2, err := Open(dbPath, keyPath)
	if err != nil {
		t.Fatalf("reopen with migration: %v", err)
	}
	defer st2.Close()
	u, err := st2.GetUserByDiscordID("x")
	if err != nil || u == nil || u.IsAdmin {
		t.Errorf("migrated user = %+v, err %v", u, err)
	}
}
