package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"feedforge/internal/model"
)

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // sqlite: serialise writers
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS feeds (
		id             TEXT PRIMARY KEY,
		name           TEXT NOT NULL,
		source_url     TEXT NOT NULL,
		description    TEXT NOT NULL DEFAULT '',
		enabled        INTEGER NOT NULL DEFAULT 1,
		fetch_interval INTEGER NOT NULL DEFAULT 30,
		rules          TEXT NOT NULL DEFAULT '{}',
		created_at     DATETIME NOT NULL,
		updated_at     DATETIME NOT NULL
	);`)
	return err
}

func (s *Store) List() ([]*model.Feed, error) {
	rows, err := s.db.Query(`SELECT id,name,source_url,description,enabled,fetch_interval,rules,created_at,updated_at
	                         FROM feeds ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*model.Feed{}
	for rows.Next() {
		f, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) Get(id string) (*model.Feed, error) {
	row := s.db.QueryRow(`SELECT id,name,source_url,description,enabled,fetch_interval,rules,created_at,updated_at
	                      FROM feeds WHERE id = ?`, id)
	f, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return f, err
}

func (s *Store) Create(f *model.Feed) error {
	if f.ID == "" {
		f.ID = NewID()
	}
	now := time.Now().UTC()
	f.CreatedAt, f.UpdatedAt = now, now

	rules, err := json.Marshal(f.Rules)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO feeds
		(id,name,source_url,description,enabled,fetch_interval,rules,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		f.ID, f.Name, f.SourceURL, f.Description, f.Enabled, f.FetchInterval,
		string(rules), f.CreatedAt, f.UpdatedAt)
	return err
}

func (s *Store) Update(id string, f *model.Feed) error {
	f.ID = id
	f.UpdatedAt = time.Now().UTC()
	rules, err := json.Marshal(f.Rules)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE feeds SET name=?,source_url=?,description=?,enabled=?,
		fetch_interval=?,rules=?,updated_at=? WHERE id=?`,
		f.Name, f.SourceURL, f.Description, f.Enabled, f.FetchInterval,
		string(rules), f.UpdatedAt, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM feeds WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(r rowScanner) (*model.Feed, error) {
	var (
		f         model.Feed
		rulesJSON string
	)
	if err := r.Scan(&f.ID, &f.Name, &f.SourceURL, &f.Description, &f.Enabled,
		&f.FetchInterval, &rulesJSON, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(rulesJSON), &f.Rules); err != nil {
		return nil, fmt.Errorf("decode rules: %w", err)
	}
	return &f, nil
}

