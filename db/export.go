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
type ExportBundle struct {
	ExportedAt time.Time       `json:"exported_at"`
	User       ExportUser      `json:"user"`
	Configs    []ExportConfig  `json:"app_configs"`
	CallerKey  string          `json:"caller_key"`
	Logs       []ExportLog     `json:"request_logs"`
}

// ExportUser mirrors the users row without internal sentinel fields.
type ExportUser struct {
	ID          int64  `json:"id"`
	DiscordID   string `json:"discord_id"`
	Username    string `json:"username"`
	Avatar      string `json:"avatar"`
	IsAdmin     bool   `json:"is_admin"`
	Disabled    bool   `json:"disabled"`
	BannedUntil int64  `json:"banned_until"`
	AutoBanned  bool   `json:"auto_banned"`
	BanReason   string `json:"ban_reason"`
	RPMLimit    *int64 `json:"rpm_limit"`    // null when using global default
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
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

// ExportLog mirrors one request_log entry.
type ExportLog struct {
	Model     string `json:"model"`
	Service   string `json:"service"`
	StartedAt int64  `json:"started_at"`
	EndedAt   int64  `json:"ended_at"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code"`
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

	bundle := &ExportBundle{
		ExportedAt: time.Now(),
		User: ExportUser{
			ID:          u.ID,
			DiscordID:   u.DiscordID,
			Username:    u.Username,
			Avatar:      u.Avatar,
			IsAdmin:     u.IsAdmin,
			Disabled:    u.Disabled,
			BannedUntil: u.BannedUntil,
			AutoBanned:  u.AutoBanned,
			BanReason:   u.BanReason,
			RPMLimit:    rpmPtr(u.RPMLimit),
			CreatedAt:   u.CreatedAt,
			UpdatedAt:   u.UpdatedAt,
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

	// Request logs (up to 500, newest first — same as the dashboard).
	logs, err := s.ListRequestLogs(userID, 500)
	if err != nil {
		return nil, err
	}
	bundle.Logs = make([]ExportLog, 0, len(logs))
	for _, l := range logs {
		bundle.Logs = append(bundle.Logs, ExportLog{
			Model:     l.Model,
			Service:   l.Service,
			StartedAt: l.StartedAt,
			EndedAt:   l.EndedAt,
			Status:    l.Status,
			ErrorCode: l.ErrorCode,
		})
	}

	return bundle, nil
}

// rpmPtr converts sql.NullInt64 to *int64 for JSON serialisation (null → null).
func rpmPtr(v sql.NullInt64) *int64 {
	if v.Valid {
		return &v.Int64
	}
	return nil
}
