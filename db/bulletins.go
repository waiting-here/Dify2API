package db

import (
	"database/sql"
	"fmt"
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
	ID          int64
	Title       string
	Content     string // Raw body (HTML or Markdown depending on ContentType).
	ContentType string // "html" (default) or "markdown"
	Type        string // info / warning / important
	SortOrder   int
	Closable    bool
	CreatedAt   int64
	ExpiresAt   sql.NullInt64 // NULL = never expires
	IsSystem    bool
	SystemKey   sql.NullString // non-nil for system-generated bulletins
	Lang        string         // "zh" (default) or "en"
}

// BulletinDeleteError identifies an expected failed_id batch validation.
type BulletinDeleteError struct {
	ID       int64
	IsSystem bool
}

func (e *BulletinDeleteError) Error() string {
	if e.IsSystem {
		return fmt.Sprintf("系统公告 %d 不可删除", e.ID)
	}
	return fmt.Sprintf("公告 %d 不存在", e.ID)
}

func scanBulletin(row interface{ Scan(...interface{}) error }) (*Bulletin, error) {
	var b Bulletin
	if err := row.Scan(
		&b.ID, &b.Title, &b.Content, &b.ContentType, &b.Type, &b.SortOrder,
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
		`INSERT INTO bulletins (title, content, content_type, type, sort_order, closable,
		 created_at, expires_at, is_system, system_key, lang)
		 VALUES (?,?,?,?,?,?,?,?,0,NULL,?)`,
		b.Title, b.Content, b.ContentType, b.Type, b.SortOrder, boolToInt(b.Closable),
		now, b.ExpiresAt, b.Lang,
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
		`UPDATE bulletins SET title=?, content=?, content_type=?, type=?, sort_order=?,
		 closable=?, expires_at=?, lang=?
		 WHERE id=? AND is_system=0`,
		b.Title, b.Content, b.ContentType, b.Type, b.SortOrder, boolToInt(b.Closable),
		b.ExpiresAt, b.Lang, id,
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

// DeleteBulletins validates and deletes an entire selection in one
// transaction. SQL failures cannot leave a partially deleted batch.
func (s *Store) DeleteBulletins(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var isSystem bool
		if err := tx.QueryRow(`SELECT is_system FROM bulletins WHERE id=?`, id).Scan(&isSystem); err != nil {
			if err == sql.ErrNoRows {
				return &BulletinDeleteError{ID: id}
			}
			return err
		}
		if isSystem {
			return &BulletinDeleteError{ID: id, IsSystem: true}
		}
	}
	for _, id := range ids {
		if _, err := tx.Exec(`DELETE FROM bulletins WHERE id=? AND is_system=0`, id); err != nil {
			return fmt.Errorf("delete bulletin %d: %w", id, err)
		}
	}
	return tx.Commit()
}

// GetBulletin fetches a bulletin by primary key. Returns (nil, nil) when absent.
func (s *Store) GetBulletin(id int64) (*Bulletin, error) {
	b, err := scanBulletin(s.db.QueryRow(
		`SELECT id, title, content, content_type, type, sort_order,
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
		`SELECT id, title, content, content_type, type, sort_order,
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
		`SELECT id, title, content, content_type, type, sort_order,
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
