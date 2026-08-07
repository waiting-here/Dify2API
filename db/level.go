package db

// LevelThresholds holds the three donation-credit thresholds that delimit
// the automatic levels 1-4 (level 5 is manual-only). Intervals are
// lower-closed: < t2 -> 1, [t2, t3) -> 2, [t3, t4) -> 3, >= t4 -> 4.
type LevelThresholds struct {
	T2 int
	T3 int
	T4 int
}

// LevelThresholdKeys lists the setting keys of the three thresholds in
// ascending order; used by the admin level-settings API.
var LevelThresholdKeys = []string{
	SettingLevelThreshold2,
	SettingLevelThreshold3,
	SettingLevelThreshold4,
}

// LevelNameKeys lists the setting keys of the five custom level names in
// level order; an empty name means "fall back to the numeric level".
var LevelNameKeys = []string{
	SettingLevelName1,
	SettingLevelName2,
	SettingLevelName3,
	SettingLevelName4,
	SettingLevelName5,
}

// LevelThresholds returns the currently configured thresholds with the
// built-in defaults when unset or invalid. Invalid stored values (negative
// or non-numeric) fall back per-key, never partially to a broken set.
func (s *Store) LevelThresholds() LevelThresholds {
	return LevelThresholds{
		T2: s.GetSettingIntAllowZero(SettingLevelThreshold2, DefaultLevelThreshold2),
		T3: s.GetSettingIntAllowZero(SettingLevelThreshold3, DefaultLevelThreshold3),
		T4: s.GetSettingIntAllowZero(SettingLevelThreshold4, DefaultLevelThreshold4),
	}
}

// EffectiveLevel computes the automatic level from the donation credit and
// the three thresholds (lower-closed intervals). It never returns 5: level 5
// is manual-only. A negative donation credit falls below t2 and yields 1.
func EffectiveLevel(donationCredit, t2, t3, t4 int) int {
	switch {
	case donationCredit < t2:
		return 1
	case donationCredit < t3:
		return 2
	case donationCredit < t4:
		return 3
	default:
		return 4
	}
}

// GetUserLevel resolves a user's effective level: a manual override
// (u.Level != nil, 1-5) wins; otherwise the automatic level is computed
// lazily from donation_credit + thresholds. The second return value reports
// whether the level is a manual override. nil users behave as automatic
// level 1.
func GetUserLevel(u *User, thresholds LevelThresholds) (level int, manual bool) {
	if u == nil {
		return 1, false
	}
	if u.Level != nil {
		return *u.Level, true
	}
	return EffectiveLevel(u.DonationCredit, thresholds.T2, thresholds.T3, thresholds.T4), false
}
