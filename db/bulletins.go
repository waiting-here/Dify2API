package db

import (
	"database/sql"
	"time"
)

// Bulletin type constants.
const (
	BulletinTypeInfo      = "info"
	BulletinTypeWarning   = "warning"
	BulletinTypeImportant = "important"
)

// Bulletin represents a public announcement entry.
type Bulletin struct {
	ID        int64
	Title     string
	Content   string // HTML body, no escaping.
	Type      string // info / warning / important
	SortOrder int
	Closable  bool
	CreatedAt int64
	ExpiresAt sql.NullInt64 // NULL = never expires
	IsSystem  bool
	SystemKey sql.NullString // non-nil for system-generated bulletins
	Lang      string         // default "zh", reserved for beta i18n
}

func scanBulletin(row interface{ Scan(...interface{}) error }) (*Bulletin, error) {
	var b Bulletin
	if err := row.Scan(
		&b.ID, &b.Title, &b.Content, &b.Type, &b.SortOrder,
		&b.Closable, &b.CreatedAt, &b.ExpiresAt,
		&b.IsSystem, &b.SystemKey, &b.Lang,
	); err != nil {
		return nil, err
	}
	return &b, nil
}

// CreateBulletin inserts a new bulletin (admin-created, non-system).
func (s *Store) CreateBulletin(b *Bulletin) (*Bulletin, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO bulletins (title, content, type, sort_order, closable,
		 created_at, expires_at, is_system, system_key, lang)
		 VALUES (?,?,?,?,?,?,?,0,NULL,'zh')`,
		b.Title, b.Content, b.Type, b.SortOrder, boolToInt(b.Closable),
		now, b.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetBulletin(id)
}

// UpdateBulletin updates a non-system bulletin. Returns (nil, nil) when absent.
func (s *Store) UpdateBulletin(id int64, b *Bulletin) (*Bulletin, error) {
	// Reject updating system bulletins.
	existing, err := s.GetBulletin(id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}
	if existing.IsSystem {
		return nil, nil // callers should check before calling
	}

	_, err = s.db.Exec(
		`UPDATE bulletins SET title=?, content=?, type=?, sort_order=?,
		 closable=?, expires_at=?
		 WHERE id=? AND is_system=0`,
		b.Title, b.Content, b.Type, b.SortOrder, boolToInt(b.Closable),
		b.ExpiresAt, id,
	)
	if err != nil {
		return nil, err
	}
	return s.GetBulletin(id)
}

// DeleteBulletin removes a non-system bulletin by id.
func (s *Store) DeleteBulletin(id int64) error {
	_, err := s.db.Exec(`DELETE FROM bulletins WHERE id=? AND is_system=0`, id)
	return err
}

// GetBulletin fetches a bulletin by primary key. Returns (nil, nil) when absent.
func (s *Store) GetBulletin(id int64) (*Bulletin, error) {
	b, err := scanBulletin(s.db.QueryRow(
		`SELECT id, title, content, type, sort_order,
		 closable, created_at, expires_at,
		 is_system, system_key, lang
		 FROM bulletins WHERE id=?`, id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

// ListBulletins returns all bulletins ordered by sort_order DESC, id DESC.
func (s *Store) ListBulletins() ([]*Bulletin, error) {
	rows, err := s.db.Query(
		`SELECT id, title, content, type, sort_order,
		 closable, created_at, expires_at,
		 is_system, system_key, lang
		 FROM bulletins ORDER BY sort_order DESC, id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Bulletin
	for rows.Next() {
		b, err := scanBulletin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListActiveBulletins returns non-system, non-expired bulletins
// ordered by sort_order DESC, id DESC.
func (s *Store) ListActiveBulletins(now int64) ([]*Bulletin, error) {
	rows, err := s.db.Query(
		`SELECT id, title, content, type, sort_order,
		 closable, created_at, expires_at,
		 is_system, system_key, lang
		 FROM bulletins
		 WHERE is_system = 0 AND (expires_at IS NULL OR expires_at > ?)
		 ORDER BY sort_order DESC, id DESC`,
		now,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Bulletin
	for rows.Next() {
		b, err := scanBulletin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
