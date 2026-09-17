package store

import (
	"path/filepath"
	"testing"

	"feedforge/internal/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.db.Close() })
	return s
}

func TestCRUD(t *testing.T) {
	s := openTestStore(t)

	f := &model.Feed{
		Name:          "My Feed",
		SourceURL:     "https://example.com/feed",
		Description:   "desc",
		Enabled:       true,
		FetchInterval: 15,
		Rules: model.Rules{
			MaxItems: 10,
			SortDesc: true,
			Exclude: model.FilterGroup{
				Mode:  model.ModeAny,
				Rules: []model.FilterRule{{Field: "title", Op: "contains", Value: "spam"}},
			},
		},
	}
	if err := s.Create(f); err != nil {
		t.Fatalf("create: %v", err)
	}
	if f.ID == "" {
		t.Fatal("create should populate ID")
	}
	if f.CreatedAt.IsZero() || f.UpdatedAt.IsZero() {
		t.Fatal("create should set timestamps")
	}

	got, err := s.Get(f.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != f.Name || got.SourceURL != f.SourceURL || got.Description != f.Description {
		t.Errorf("get mismatch: %+v", got)
	}
	if got.Rules.MaxItems != 10 || !got.Rules.SortDesc {
		t.Errorf("rules not roundtripped: %+v", got.Rules)
	}
	exc := got.Rules.Exclude
	if exc.Mode != model.ModeAny || len(exc.Rules) != 1 || exc.Rules[0].Value != "spam" {
		t.Errorf("exclude rules not roundtripped: %+v", exc)
	}

	// Update
	got.Name = "Renamed"
	got.Rules.MaxItems = 5
	if err := s.Update(f.ID, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, err := s.Get(f.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if again.Name != "Renamed" || again.Rules.MaxItems != 5 {
		t.Errorf("update not persisted: %+v", again)
	}
	// CreatedAt must not change on update.
	if !again.CreatedAt.Equal(f.CreatedAt) {
		t.Errorf("created_at changed on update: %v -> %v", f.CreatedAt, again.CreatedAt)
	}

	// Delete
	if err := s.Delete(f.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(f.ID); err != ErrNotFound {
		t.Errorf("get after delete: want ErrNotFound, got %v", err)
	}
}
