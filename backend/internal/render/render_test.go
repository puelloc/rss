package render

import (
	"strings"
	"testing"
	"time"

	"feedforge/internal/transform"
)

func sampleResult() *transform.Result {
	updated := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	return &transform.Result{
		Title:       "Test Feed",
		Link:        "https://ex.com/self",
		Description: "desc",
		Items: []transform.Item{
			{Title: "First", Link: "https://ex.com/a", GUID: "g-1", Author: "Alice", Published: updated},
			{Title: "Second", Link: "https://ex.com/b", GUID: "g-2", Description: "short", Content: "<p>long</p>"},
		},
		SourceCount: 2,
		Matched:     2,
	}
}

func containsAll(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("output missing %q", sub)
		}
	}
}

func TestRSSContainsBasicStructure(t *testing.T) {
	out, err := RSS(sampleResult(), "https://ex.com/self", time.Time{})
	if err != nil {
		t.Fatalf("RSS(): %v", err)
	}
	containsAll(t, out, "<?xml", "<rss", "<channel", "<item>")
	containsAll(t, out, "Test Feed", "First", "Second", "https://ex.com/a")
}

func TestAtomContainsBasicStructure(t *testing.T) {
	out, err := Atom(sampleResult(), "https://ex.com/self", time.Time{})
	if err != nil {
		t.Fatalf("Atom(): %v", err)
	}
	containsAll(t, out, "<?xml", "<feed", "<entry>")
	containsAll(t, out, "Test Feed", "First", "https://ex.com/a")
}

func TestRSSItemFields(t *testing.T) {
	out, err := RSS(sampleResult(), "https://ex.com/self", time.Time{})
	if err != nil {
		t.Fatalf("RSS(): %v", err)
	}
	containsAll(t, out, "Alice", "g-1")
}
