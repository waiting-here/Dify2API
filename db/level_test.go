package db

import "testing"

// TestEffectiveLevel_Boundaries verifies the frozen lower-closed threshold
// semantics with the default thresholds (1/100/500): <1 -> 1, 1..99 -> 2,
// 100..499 -> 3, >=500 -> 4. Level 5 is never returned.
func TestEffectiveLevel_Boundaries(t *testing.T) {
	cases := []struct {
		credit int
		want   int
	}{
		{-10, 1}, {0, 1}, {1, 2}, {2, 2}, {99, 2},
		{100, 3}, {101, 3}, {499, 3}, {500, 4}, {501, 4}, {100000, 4},
	}
	for _, c := range cases {
		if got := EffectiveLevel(c.credit, DefaultLevelThreshold2, DefaultLevelThreshold3, DefaultLevelThreshold4); got != c.want {
			t.Errorf("EffectiveLevel(%d, 1, 100, 500) = %d, want %d", c.credit, got, c.want)
		}
	}
	// Never returns 5.
	if got := EffectiveLevel(1<<62, 1, 100, 500); got != 4 {
		t.Errorf("huge credit: level %d, want 4 (5 is manual-only)", got)
	}
}

// TestEffectiveLevel_ZeroT2 verifies that t2 may be 0: every non-negative
// credit is then level 2+; only negative credits stay at level 1.
func TestEffectiveLevel_ZeroT2(t *testing.T) {
	if got := EffectiveLevel(0, 0, 100, 500); got != 2 {
		t.Errorf("EffectiveLevel(0, 0, 100, 500) = %d, want 2", got)
	}
	if got := EffectiveLevel(99, 0, 100, 500); got != 2 {
		t.Errorf("EffectiveLevel(99, 0, 100, 500) = %d, want 2", got)
	}
	if got := EffectiveLevel(-1, 0, 100, 500); got != 1 {
		t.Errorf("EffectiveLevel(-1, 0, 100, 500) = %d, want 1", got)
	}
}

// TestGetUserLevel_ManualVsAuto verifies the manual override wins and that
// automatic levels track donation_credit immediately (lazy computation).
func TestGetUserLevel_ManualVsAuto(t *testing.T) {
	th := LevelThresholds{T2: 1, T3: 100, T4: 500}

	// Automatic: donation_credit drives the level.
	u := &User{DonationCredit: 250}
	if level, manual := GetUserLevel(u, th); level != 3 || manual {
		t.Errorf("auto level = %d manual=%v, want 3/false", level, manual)
	}
	u.DonationCredit = 500
	if level, _ := GetUserLevel(u, th); level != 4 {
		t.Errorf("auto level after credit change = %d, want 4 (immediate)", level)
	}

	// Manual override: wins regardless of donation_credit.
	five := 5
	u2 := &User{DonationCredit: 0, Level: &five}
	if level, manual := GetUserLevel(u2, th); level != 5 || !manual {
		t.Errorf("manual level = %d manual=%v, want 5/true", level, manual)
	}
	// Manual 1 with huge credit stays manual.
	one := 1
	u3 := &User{DonationCredit: 9000, Level: &one}
	if level, manual := GetUserLevel(u3, th); level != 1 || !manual {
		t.Errorf("manual low level = %d manual=%v, want 1/true", level, manual)
	}

	// nil user behaves as automatic level 1.
	if level, manual := GetUserLevel(nil, th); level != 1 || manual {
		t.Errorf("nil user level = %d manual=%v, want 1/false", level, manual)
	}
}

// TestLevelThresholds_StoreDefaults verifies settings round-tripping and the
// per-key fallback for invalid stored values.
func TestLevelThresholds_StoreDefaults(t *testing.T) {
	st, _ := openTemp(t)

	th := st.LevelThresholds()
	if th.T2 != DefaultLevelThreshold2 || th.T3 != DefaultLevelThreshold3 || th.T4 != DefaultLevelThreshold4 {
		t.Fatalf("default thresholds = %+v, want %d/%d/%d", th, DefaultLevelThreshold2, DefaultLevelThreshold3, DefaultLevelThreshold4)
	}

	// Stored values win.
	st.SetSetting(SettingLevelThreshold2, "5")
	st.SetSetting(SettingLevelThreshold3, "200")
	st.SetSetting(SettingLevelThreshold4, "1000")
	th = st.LevelThresholds()
	if th.T2 != 5 || th.T3 != 200 || th.T4 != 1000 {
		t.Fatalf("stored thresholds = %+v, want 5/200/1000", th)
	}

	// t2 may legitimately be 0 (all non-negative credits are level 2+).
	st.SetSetting(SettingLevelThreshold2, "0")
	if th := st.LevelThresholds(); th.T2 != 0 {
		t.Errorf("threshold_2 = %d, want 0", th.T2)
	}

	// Invalid values fall back per-key without poisoning the rest.
	st.SetSetting(SettingLevelThreshold3, "not-a-number")
	th = st.LevelThresholds()
	if th.T2 != 0 || th.T3 != DefaultLevelThreshold3 || th.T4 != 1000 {
		t.Errorf("fallback thresholds = %+v, want 0/%d/1000", th, DefaultLevelThreshold3)
	}
	st.SetSetting(SettingLevelThreshold4, "-5")
	if th := st.LevelThresholds(); th.T4 != DefaultLevelThreshold4 {
		t.Errorf("negative threshold_4 = %d, want fallback %d", th.T4, DefaultLevelThreshold4)
	}
}

// TestUserLevel_PersistAndClear verifies SetUserLevel round-trips through
// GetUserByID/ListUsers and that nil restores the NULL (automatic) state.
func TestUserLevel_PersistAndClear(t *testing.T) {
	st, _ := openTemp(t)
	u, _ := st.CreateUser("7001", "level_user", "")

	// Fresh users have no manual level.
	got, _ := st.GetUserByID(u.ID)
	if got.Level != nil {
		t.Fatalf("fresh user Level = %v, want nil", *got.Level)
	}

	three := 3
	if err := st.SetUserLevel(u.ID, &three); err != nil {
		t.Fatalf("SetUserLevel(3): %v", err)
	}
	got, _ = st.GetUserByID(u.ID)
	if got.Level == nil || *got.Level != 3 {
		t.Fatalf("Level after set = %v, want 3", got.Level)
	}
	list, err := st.ListUsers()
	if err != nil || len(list) != 1 || list[0].Level == nil || *list[0].Level != 3 {
		t.Fatalf("ListUsers level = %v / %v", list, err)
	}

	// Clearing restores automatic (NULL).
	if err := st.SetUserLevel(u.ID, nil); err != nil {
		t.Fatalf("SetUserLevel(nil): %v", err)
	}
	got, _ = st.GetUserByID(u.ID)
	if got.Level != nil {
		t.Fatalf("Level after clear = %v, want nil", *got.Level)
	}
}

// TestSetUserLevel_ExcludesAdmin verifies the admin row is never part of the
// level system.
func TestSetUserLevel_ExcludesAdmin(t *testing.T) {
	st, _ := openTemp(t)
	admin, err := st.EnsureAdminUser("root")
	if err != nil {
		t.Fatalf("EnsureAdminUser: %v", err)
	}
	five := 5
	if err := st.SetUserLevel(admin.ID, &five); err != nil {
		t.Fatalf("SetUserLevel(admin): %v", err)
	}
	got, _ := st.GetUserByID(admin.ID)
	if got.Level != nil {
		t.Errorf("admin Level = %v, want nil", *got.Level)
	}
}

// TestSetSettings_LevelBatchFailureRollsBack verifies the nine level-settings
// keys are written atomically: when any statement inside the batch fails, the
// whole batch rolls back and no half-written threshold set survives.
func TestSetSettings_LevelBatchFailureRollsBack(t *testing.T) {
	st, _ := openTemp(t)

	// Establish a known good state first.
	if err := st.SetSettings([]SettingUpdate{
		{Key: SettingLevelThreshold2, Value: "5"},
		{Key: SettingLevelThreshold3, Value: "50"},
		{Key: SettingLevelThreshold4, Value: "500"},
		{Key: SettingLevelName1, Value: "old"},
		{Key: SettingLevelBannerText, Value: "old banner"},
	}); err != nil {
		t.Fatalf("seed level settings: %v", err)
	}

	// Inject a failure on the third threshold write.
	if _, err := st.db.Exec(`CREATE TRIGGER fail_level_threshold3
		BEFORE INSERT ON settings WHEN NEW.key='level_threshold_3'
		BEGIN SELECT RAISE(ABORT, 'forced level-settings failure'); END`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	err := st.SetSettings([]SettingUpdate{
		{Key: SettingLevelThreshold2, Value: "10"},
		{Key: SettingLevelThreshold3, Value: "20"},
		{Key: SettingLevelThreshold4, Value: "30"},
		{Key: SettingLevelName1, Value: "new1"},
		{Key: SettingLevelName2, Value: "new2"},
		{Key: SettingLevelName3, Value: "new3"},
		{Key: SettingLevelName4, Value: "new4"},
		{Key: SettingLevelName5, Value: "new5"},
		{Key: SettingLevelBannerText, Value: "new banner"},
	})
	if err == nil {
		t.Fatal("SetSettings succeeded despite injected failure")
	}

	// The whole batch rolled back: no partial threshold set, no partial names.
	th := st.LevelThresholds()
	if th.T2 != 5 || th.T3 != 50 || th.T4 != 500 {
		t.Errorf("thresholds after failed batch = %+v, want 5/50/500 (all-or-nothing)", th)
	}
	if v := st.GetSettingString(SettingLevelName1, ""); v != "old" {
		t.Errorf("name_1 after failed batch = %q, want old", v)
	}
	if v := st.GetSettingString(SettingLevelName2, ""); v != "" {
		t.Errorf("name_2 after failed batch = %q, want empty (never written)", v)
	}
	if v := st.GetSettingString(SettingLevelBannerText, ""); v != "old banner" {
		t.Errorf("banner after failed batch = %q, want old banner", v)
	}
}
