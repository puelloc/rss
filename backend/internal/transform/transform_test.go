package transform

import (
	"testing"
	"time"

	"github.com/mmcdole/gofeed"

	"feedforge/internal/model"
)

func item(title, desc string) *gofeed.Item {
	return &gofeed.Item{Title: title, Description: desc, Link: "https://x.test/1"}
}

func TestTransformString(t *testing.T) {
	rule := func(op, find, replace string) model.TransformRule {
		return model.TransformRule{Op: op, Find: find, Replace: replace}
	}
	cases := []struct {
		name string
		s    string
		r    model.TransformRule
		want string
	}{
		{"replace all", "aa-bb-cc", rule("replace", "b", "B"), "aa-BB-cc"},
		{"regex replace", "order 12345", rule("regex_replace", `\d+`, "#42"), "order #42"},
		{"regex invalid unchanged", "abc", rule("regex_replace", "(", "x"), "abc"},
		{"prefix", "World", rule("prefix", "Hello ", ""), "Hello World"},
		{"suffix", "Hello", rule("suffix", "!", ""), "Hello!"},
		{"strip html", " <b>Bold</b> and <i>italic</i> ", rule("strip_html", "", ""), "Bold and italic"},
		{"trim", "  x  ", rule("trim", "", ""), "x"},
		{"lower", "ABC", rule("lower", "", ""), "abc"},
		{"upper", "abc", rule("upper", "", ""), "ABC"},
		{"unknown op unchanged", "abc", rule("nope", "", ""), "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := transformString(tc.s, tc.r); got != tc.want {
				t.Errorf("transformString(%q, %q) = %q, want %q", tc.s, tc.r.Op, got, tc.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello, World!", "hello-world"},
		{"  --Leading and trailing--  ", "leading-and-trailing"},
		{"a--b  c", "a-b-c"},
		{"", ""},
		{"already-slug 2024", "already-slug-2024"},
	}
	for _, tc := range cases {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEpisodeNumber(t *testing.T) {
	cases := []struct{ in, want string }{
		{"S03E12 The Big One", "12"},
		{"Episode 47: Chaos", "47"},
		{"Ep8 bonus", "8"},
		{"- 12 special v2", "12"},
		{"No number here", ""},
	}
	for _, tc := range cases {
		if got := episodeNumber(tc.in); got != tc.want {
			t.Errorf("episodeNumber(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderLink(t *testing.T) {
	it := &gofeed.Item{
		Title:   "Episode 42: Hello, World!",
		Link:    "https://orig.test/x",
		Content: "body",
	}

	t.Run("empty template keeps original link", func(t *testing.T) {
		got, err := renderLink("  ", it)
		if err != nil || got != "https://orig.test/x" {
			t.Errorf("got %q, %v", got, err)
		}
	})

	t.Run("template funcs", func(t *testing.T) {
		got, err := renderLink("https://x.test/{{ .Episode }}-{{ slug .Title }}", it)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		want := "https://x.test/42-episode-42-hello-world"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("bad template returns error and empty link", func(t *testing.T) {
		got, err := renderLink("{{ .Nope", it)
		if err == nil {
			t.Fatal("want error for bad template")
		}
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func applyNow() time.Time { return time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC) }

func dt(m time.Month, day int) *time.Time {
	t := time.Date(2026, m, day, 12, 0, 0, 0, time.UTC)
	return &t
}

func TestApply(t *testing.T) {
	feed := model.Feed{Name: "pod", Rules: model.Rules{
		Include:    model.FilterGroup{Mode: "any", Rules: []model.FilterRule{{Field: "title", Op: "contains", Value: "tech"}}},
		Transforms: []model.TransformRule{{Target: "link", Op: "replace", Find: "http://", Replace: "https://"}},
		MaxItems:   2,
		SortDesc:   true,
	}}
	src := &gofeed.Feed{Title: "src", Items: []*gofeed.Item{
		{Title: "Tech talk", Link: "http://a.example/1", PublishedParsed: dt(1, 2)},
		{Title: "Sports news", Link: "http://a.example/2"},
		{Title: "More tech", Link: "http://a.example/3", PublishedParsed: dt(1, 3)},
		{Title: "tech roundup", Link: "http://a.example/4", PublishedParsed: dt(1, 1)},
	}}

	out := Apply(&feed, src, "http://feed.example/rss", applyNow())

	if out.Title != "pod" {
		t.Errorf("title = %q, want pod", out.Title)
	}
	if out.Link != "http://feed.example/rss" {
		t.Errorf("link = %q", out.Link)
	}
	if out.SourceCount != 4 {
		t.Errorf("source_count = %d, want 4", out.SourceCount)
	}
	if out.Matched != 3 {
		t.Errorf("matched = %d, want 3 (Sports news filtered out)", out.Matched)
	}
	if len(out.Items) != 2 {
		t.Fatalf("got %d items, want 2 (max_items)", len(out.Items))
	}
	if out.Items[0].Title != "More tech" {
		t.Errorf("first item = %q, want Most tech (newest of matches)", out.Items[0].Title)
	}
	if out.Items[0].Link != "https://a.example/3" {
		t.Errorf("first link = %q, want https://a.example/3", out.Items[0].Link)
	}
}

func TestApplyMaxAge(t *testing.T) {
	feed := model.Feed{Rules: model.Rules{MaxAgeDays: 1, SortDesc: true}}
	src := &gofeed.Feed{Items: []*gofeed.Item{
		{Title: "recent", PublishedParsed: dt(1, 9)},
		{Title: "old", PublishedParsed: dt(1, 1)},
		{Title: "no date"},                               // no date: must survive max_age filter
		{Title: "updated date", UpdatedParsed: dt(1, 9)}, // UpdatedParsed fallback
	}}

	out := Apply(&feed, src, "", applyNow())
	got := map[string]bool{}
	for _, it := range out.Items {
		got[it.Title] = true
	}

	if !got["recent"] || !got["no date"] || !got["updated date"] {
		t.Errorf("unexpected survivors: %v (want recent, no date, updated date)", got)
	}
	if got["old"] {
		t.Errorf("old item kept despite max_age_days=1: %v", got)
	}

	// dated items first (newest first), undated last
	if out.Items[0].Title != "recent" {
		t.Errorf("first = %q, want recent", out.Items[0].Title)
	}
	if out.Items[len(out.Items)-1].Title != "no date" && out.Items[len(out.Items)-1].Title != "updated date" {
		t.Errorf("last should be an undated item, got %q", out.Items[len(out.Items)-1].Title)
	}
}

func TestApplyTransforms(t *testing.T) {
	feed := model.Feed{Rules: model.Rules{Transforms: []model.TransformRule{
		{Target: "title", Op: "upper"},
		{Target: "link", Op: "regex_replace", Find: `http://[^/]+`, Replace: "https://cdn"},
		{Target: "content", Op: "strip_html"},
	}}}
	src := &gofeed.Feed{Items: []*gofeed.Item{{
		Title: "lowercase", Description: "<p>hi</p>", Link: "http://old.example/x",
		Content: "<div>hello <b>world</b></div>",
	}}}

	out := Apply(&feed, src, "", applyNow())
	it := out.Items[0]
	if it.Title != "LOWERCASE" {
		t.Errorf("title = %q, want LOWERCASE", it.Title)
	}
	if it.Description != "<p>hi</p>" {
		t.Errorf("untouched description altered: %q", it.Description)
	}
	if it.Link != "https://cdn/x" {
		t.Errorf("link = %q, want https://cdn/x", it.Link)
	}
	if it.Content != "hello world" {
		t.Errorf("content = %q, want \"hello world\"", it.Content)
	}
}

func TestApplyDoesNotMutateSource(t *testing.T) {
	feed := model.Feed{Rules: model.Rules{Transforms: []model.TransformRule{{Target: "title", Op: "upper"}}}}
	src := &gofeed.Feed{Items: []*gofeed.Item{{Title: "before"}}}
	Apply(&feed, src, "", applyNow())
	if src.Items[0].Title != "before" {
		t.Errorf("source item mutated: %q", src.Items[0].Title)
	}
}

func TestApplyGUIDFallback(t *testing.T) {
	feed := model.Feed{}
	src := &gofeed.Feed{Items: []*gofeed.Item{
		{Title: "no guid", Link: "http://x.example/1", PublishedParsed: dt(1, 1)},
		{Title: "with guid", Link: "http://x.example/2", GUID: "custom-id"},
	}}

	out := Apply(&feed, src, "http://feed.example/rss", applyNow())
	if out.Items[0].GUID != "http://x.example/1" {
		t.Errorf("guid = %q, want link fallback", out.Items[0].GUID)
	}
	if out.Items[1].GUID != "custom-id" {
		t.Errorf("guid = %q, want custom-id", out.Items[1].GUID)
	}
	if out.Items[0].Link != "http://x.example/1" {
		t.Errorf("link = %q, want unchanged (empty template)", out.Items[0].Link)
	}
}

func TestMatchRuleOps(t *testing.T) {
	val := "The Quick Brown Fox"
	rule := func(op, value string, cs bool) model.FilterRule {
		return model.FilterRule{Field: "title", Op: op, Value: value, CaseSensitive: cs}
	}

	cases := []struct {
		name string
		r    model.FilterRule
		want bool
	}{
		{"contains hit", rule("contains", "brown", false), true},
		{"contains case-sensitive miss", rule("contains", "brown", true), false},
		{"contains miss", rule("contains", "dog", false), false},
		{"not_contains hit", rule("not_contains", "dog", false), true},
		{"not_contains miss", rule("not_contains", "fox", false), false},
		{"equals hit", rule("equals", "the quick brown fox", false), true},
		{"equals exact hit", rule("equals", "The Quick Brown Fox", true), true},
		{"starts_with hit", rule("starts_with", "the", false), true},
		{"starts_with miss", rule("starts_with", "quick", false), false},
		{"ends_with hit", rule("ends_with", "FOX", false), true},
		{"ends_with miss", rule("ends_with", "quick", false), false},
		{"regex hit", rule("regex", `brown \w+`, false), true},
		{"regex invalid returns false", rule("regex", "(", false), false},
		{"unknown op returns false", rule("nonsense", "the", false), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchRule(val, tc.r); got != tc.want {
				t.Errorf("matchRule(%q) = %v, want %v", tc.r.Op, got, tc.want)
			}
		})
	}
}
