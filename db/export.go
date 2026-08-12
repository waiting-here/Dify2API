package db

import (
	"database/sql"
	"time"
)

// ExportBundle contains all data associated with one user, assembled for
// self-service data export (GDPR "right of access" / "data portability").
//
// Dify API keys are decrypted so the user can retrieve them; caller keys are
// also decrypted. Sessions are intentionally NOT included — they are ephemeral
// authentication tokens, not personal data worth porting.
//
// Donations (donations table) are NOT included — donations are registered by
// the administrator and belong to the public-resource pool, not to any
// individual user's personal data.
type ExportBundle struct {
	ExportedAt           time.Time                  `json:"exported_at"`
	User                 ExportUser                 `json:"user"`
	Configs              []ExportConfig             `json:"app_configs"`
	CallerKey            string                     `json:"caller_key"`
	Logs                 []ExportLog                `json:"request_logs"`
	Activity             []UserActivityDaily        `json:"user_activity_daily"`
	DonationApplications []ExportDonationApp        `json:"donation_applications"`
	CharityReservations  []ExportCharityReservation `json:"charity_reservations"`
	Games                ExportGamesData            `json:"games"`
	TemplateDownloads    []ExportServiceGeneration  `json:"template_downloads"`
}

// ExportServiceGeneration mirrors one service_generations row (mapping
// included so the user can recover their imported App's variable meaning).
type ExportServiceGeneration struct {
	ID            int64  `json:"id"`
	Service       string `json:"service"`
	ModelKey      string `json:"model_key"`
	Purpose       string `json:"purpose"`
	Seed          string `json:"seed"`
	MappingJSON   string `json:"mapping_json"`
	DummyJSON     string `json:"dummy_json"`
	DummyCount    int    `json:"dummy_count"`
	DownloadCount int    `json:"download_count"`
	CreatedAt     int64  `json:"created_at"`
}

// ExportGamesData is the mini-game portion of an export: the user's game
// rounds within the rolling retention window, their per-game best catches,
// and the leaderboard anonymity switch.
type ExportGamesData struct {
	LeaderboardAnon bool              `json:"leaderboard_anon"`
	Rounds          []ExportGameRound `json:"rounds"`
	Best            []GameBestRow     `json:"best"`
}

// ExportGameRound mirrors one game_rounds row.
type ExportGameRound struct {
	ID         string `json:"id"`
	GameID     string `json:"game_id"`
	BaitTier   string `json:"bait_tier"`
	Price      int    `json:"price"`
	Status     string `json:"status"`
	SpeciesKey string `json:"species_key"`
	SizeCM     int    `json:"size_cm"`
	IsJunk     bool   `json:"is_junk"`
	IsTreasure bool   `json:"is_treasure"`
	CreditsWon int    `json:"credits_won"`
	CreatedAt  int64  `json:"created_at"`
	SettledAt  int64  `json:"settled_at"`
}

// ExportUser mirrors the users row without internal sentinel fields.
type ExportUser struct {
	ID             int64  `json:"id"`
	DiscordID      string `json:"discord_id"`
	Username       string `json:"username"`
	Avatar         string `json:"avatar"`
	IsAdmin        bool   `json:"is_admin"`
	Disabled       bool   `json:"disabled"`
	BannedUntil    int64  `json:"banned_until"`
	AutoBanned     bool   `json:"auto_banned"`
	BanReason      string `json:"ban_reason"`
	Credits        int    `json:"credits"`
	LastCheckinDay string `json:"last_checkin_day"`
	// RPMLimitA/B/C are per-user three-class RPM overrides; null when using global defaults.
	RPMLimitA      *int64 `json:"rpm_limit_a"`
	RPMLimitB      *int64 `json:"rpm_limit_b"`
	RPMLimitC      *int64 `json:"rpm_limit_c"`
	DonationCredit int    `json:"donation_credit"`
	CharityEnabled bool   `json:"charity_enabled"`
	Lang           string `json:"lang"`
	// LeaderboardAnon hides the username on game leaderboards.
	LeaderboardAnon bool `json:"leaderboard_anon"`
	// Level is the manual level override (null = automatic). EffectiveLevel
	// is the lazily computed effective level 1-5; LevelManual reports whether
	// it comes from a manual override rather than automatic computation.
	Level          *int  `json:"level"`
	EffectiveLevel int   `json:"effective_level"`
	LevelManual    bool  `json:"level_manual"`
	CreatedAt      int64 `json:"created_at"`
	UpdatedAt      int64 `json:"updated_at"`
}

// ExportConfig mirrors one app_configs row with the Dify API key decrypted.
type ExportConfig struct {
	Model       string `json:"model"`
	DifyBaseURL string `json:"dify_base_url"`
	DifyAPIKey  string `json:"dify_api_key"` // decrypted
	Note        string `json:"note"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// ExportDonationApp mirrors one donation_applications row with the Dify API key decrypted.
type ExportDonationApp struct {
	ID          int64  `json:"id"`
	Service     string `json:"service"`
	Model       string `json:"model"`
	DifyBaseURL string `json:"dify_base_url"`
	DifyAPIKey  string `json:"dify_api_key"` // decrypted
	TotalCount  int    `json:"total_count"`
	Deadline    int64  `json:"deadline"`
	RpmLimit    int    `json:"rpm_limit"`
	Note        string `json:"note"`
	Status      string `json:"status"`
	ReviewerID  *int64 `json:"reviewer_id"`
	ReviewNote  string `json:"review_note"`
	DonationID  *int64 `json:"donation_id"`
	CreatedAt   int64  `json:"created_at"`
	// MappingJSON is the template variable snapshot (B' services only).
	MappingJSON string `json:"mapping_json"`
}

// ExportCharityReservation exposes the user's role in a recent accounting
// reservation without disclosing the other party's internal user id.
type ExportCharityReservation struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	DonationID int64  `json:"donation_id"`
	Price      int    `json:"price"`
	Reward     int    `json:"reward"`
	Status     string `json:"status"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// ExportLog mirrors one request_log entry.
type ExportLog struct {
	Model           string `json:"model"`
	Service         string `json:"service"`
	StartedAt       int64  `json:"started_at"`
	EndedAt         int64  `json:"ended_at"`
	Status          string `json:"status"`
	ErrorCode       string `json:"error_code"`
	HttpStatus      int    `json:"http_status"`
	ErrorDetail     string `json:"error_detail"`
	CreditsConsumed int    `json:"credits_consumed"`
	AntiAbuseInfo   string `json:"anti_abuse_info"`
}

// ExportUserData assembles the full export bundle for a single user.
// Returns (nil, nil) when the user does not exist.
func (s *Store) ExportUserData(userID int64) (*ExportBundle, error) {
	u, err := s.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}

	level, levelManual := GetUserLevel(u, s.LevelThresholds())

	bundle := &ExportBundle{
		ExportedAt: time.Now(),
		User: ExportUser{
			ID:             u.ID,
			DiscordID:      u.DiscordID,
			Username:       u.Username,
			Avatar:         u.Avatar,
			IsAdmin:        u.IsAdmin,
			Disabled:       u.Disabled,
			BannedUntil:    u.BannedUntil,
			AutoBanned:     u.AutoBanned,
			BanReason:      u.BanReason,
			Credits:        u.Credits,
			LastCheckinDay: u.LastCheckinDay,
			RPMLimitA:      nullableIntPtr(u.RPMLimitA),
			RPMLimitB:      nullableIntPtr(u.RPMLimitB),
			RPMLimitC:      nullableIntPtr(u.RPMLimitC),
			DonationCredit: u.DonationCredit,
			CharityEnabled: u.CharityEnabled,
			Lang:           u.Lang,
			Level:          u.Level,
			EffectiveLevel: level,
			LevelManual:    levelManual,
			CreatedAt:      u.CreatedAt,
			UpdatedAt:      u.UpdatedAt,
		},
	}

	// App configs with decrypted API keys.
	configs, err := s.ListAppConfigs(userID)
	if err != nil {
		return nil, err
	}
	bundle.Configs = make([]ExportConfig, 0, len(configs))
	for _, c := range configs {
		key, decErr := s.Decrypt(c.DifyAPIKeyEnc)
		if decErr != nil {
			key = "(decrypt failed: " + decErr.Error() + ")"
		}
		bundle.Configs = append(bundle.Configs, ExportConfig{
			Model:       c.Model,
			DifyBaseURL: c.DifyBaseURL,
			DifyAPIKey:  key,
			Note:        c.Note,
			Enabled:     c.Enabled,
			CreatedAt:   c.CreatedAt,
			UpdatedAt:   c.UpdatedAt,
		})
	}

	// Caller key (decrypted; may be empty if never provisioned).
	if key, err := s.GetCallerKeyPlain(userID); err == nil && key != "" {
		bundle.CallerKey = key
	}

	// Request logs (complete, without limit — exports are infrequent).
	logs, err := s.ExportRequestLogs(userID)
	if err != nil {
		return nil, err
	}
	bundle.Logs = make([]ExportLog, 0, len(logs))
	for _, l := range logs {
		bundle.Logs = append(bundle.Logs, ExportLog{
			Model:           l.Model,
			Service:         l.Service,
			StartedAt:       l.StartedAt,
			EndedAt:         l.EndedAt,
			Status:          l.Status,
			ErrorCode:       l.ErrorCode,
			HttpStatus:      l.HTTPStatus,
			ErrorDetail:     l.ErrorDetail,
			CreditsConsumed: l.CreditsConsumed,
			AntiAbuseInfo:   l.AntiAbuseInfo,
		})
	}
	activity, err := s.ListUserActivity(userID)
	if err != nil {
		return nil, err
	}
	bundle.Activity = activity

	// Mini-game data: rounds within the rolling retention window, best
	// catches, and the leaderboard anonymity switch.
	rounds, err := s.ListGameRounds(userID, 0, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	bundle.Games.LeaderboardAnon = false
	if anon, err := s.LeaderboardAnon(userID); err == nil {
		bundle.Games.LeaderboardAnon = anon
	}
	bundle.Games.Rounds = make([]ExportGameRound, 0, len(rounds))
	for _, r := range rounds {
		bundle.Games.Rounds = append(bundle.Games.Rounds, ExportGameRound{
			ID: r.ID, GameID: r.GameID, BaitTier: r.BaitTier, Price: r.Price,
			Status: r.Status, SpeciesKey: r.SpeciesKey, SizeCM: r.SizeCM,
			IsJunk: r.IsJunk, IsTreasure: r.IsTreasure, CreditsWon: r.CreditsWon,
			CreatedAt: r.CreatedAt, SettledAt: r.SettledAt,
		})
	}
	if best, err := s.ListGameBest(userID); err == nil {
		bundle.Games.Best = best
	}

	// Template download records (mappings included for portability).
	gens, err := s.ListServiceGenerations(userID)
	if err != nil {
		return nil, err
	}
	bundle.TemplateDownloads = make([]ExportServiceGeneration, 0, len(gens))
	for _, g := range gens {
		bundle.TemplateDownloads = append(bundle.TemplateDownloads, ExportServiceGeneration{
			ID: g.ID, Service: g.Service, ModelKey: g.ModelKey, Purpose: g.Purpose, Seed: g.Seed,
			MappingJSON: g.MappingJSON, DummyJSON: g.DummyJSON, DummyCount: g.DummyCount,
			DownloadCount: g.DownloadCount, CreatedAt: g.CreatedAt,
		})
	}

	// Donation applications with decrypted API keys.
	apps, err := s.ListApplicationsByUser(userID)
	if err != nil {
		return nil, err
	}
	bundle.DonationApplications = make([]ExportDonationApp, 0, len(apps))
	for _, a := range apps {
		key, decErr := s.Decrypt(a.DifyAPIKeyEnc)
		if decErr != nil {
			key = "(decrypt failed: " + decErr.Error() + ")"
		}
		expApp := ExportDonationApp{
			ID:          a.ID,
			Service:     a.Service,
			Model:       a.Model,
			DifyBaseURL: a.DifyBaseURL,
			DifyAPIKey:  key,
			TotalCount:  a.TotalCount,
			Deadline:    a.Deadline,
			RpmLimit:    a.RpmLimit,
			Note:        a.Note,
			Status:      a.Status,
			ReviewNote:  a.ReviewNote,
			CreatedAt:   a.CreatedAt,
			MappingJSON: a.MappingJSON,
		}
		if a.ReviewerID.Valid {
			expApp.ReviewerID = &a.ReviewerID.Int64
		}
		if a.DonationID.Valid {
			expApp.DonationID = &a.DonationID.Int64
		}
		bundle.DonationApplications = append(bundle.DonationApplications, expApp)
	}

	reservations, err := s.ListUserCharityReservations(userID)
	if err != nil {
		return nil, err
	}
	bundle.CharityReservations = make([]ExportCharityReservation, 0, len(reservations))
	for _, r := range reservations {
		role := "consumer"
		if r.DonorUserID.Valid && r.DonorUserID.Int64 == userID {
			if r.UserID == userID {
				role = "consumer_and_donor"
			} else {
				role = "donor"
			}
		}
		bundle.CharityReservations = append(bundle.CharityReservations, ExportCharityReservation{
			ID: r.ID, Role: role, DonationID: r.DonationID, Price: r.Price,
			Reward: r.Reward, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
	}

	return bundle, nil
}

// nullableIntPtr converts sql.NullInt64 to *int64 for JSON serialisation.
func nullableIntPtr(v sql.NullInt64) *int64 {
	if v.Valid {
		return &v.Int64
	}
	return nil
}
