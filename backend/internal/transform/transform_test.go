package transform

import (
	"testing"

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
