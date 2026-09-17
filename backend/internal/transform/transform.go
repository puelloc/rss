package transform

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode"

	"github.com/mmcdole/gofeed"

	"feedforge/internal/model"
)

type Item struct {
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Author      string    `json:"author"`
	Published   time.Time `json:"published"`
	GUID        string    `json:"guid"`
	Categories  []string  `json:"categories"`
}

type Result struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	Items       []Item `json:"items"`
	SourceCount int    `json:"source_count"`
	Matched     int    `json:"matched"`
}

// ---------- Field extraction ----------

func fieldValue(it *gofeed.Item, field string) string {
	switch field {
	case "title":
		return it.Title
	case "description":
		return it.Description
	case "content":
		if it.Content != "" {
			return it.Content
		}
		return it.Description
	case "link":
		return it.Link
	case "author":
		if it.Author != nil {
			return it.Author.Name
		}
		return ""
	case "categories":
		return strings.Join(it.Categories, ",")
	}
	return ""
}

// ---------- Filtering ----------

var (
	regexCacheMu sync.RWMutex
	regexCache   = map[string]*regexp.Regexp{}
)

func compiled(expr string) *regexp.Regexp {
	regexCacheMu.RLock()
	re, ok := regexCache[expr]
	regexCacheMu.RUnlock()
	if ok {
		return re
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil
	}
	regexCacheMu.Lock()
	if len(regexCache) > 1000 { // crude cap: arbitrary user regexes shouldn't grow this forever
		regexCache = map[string]*regexp.Regexp{}
	}
	regexCache[expr] = re
	regexCacheMu.Unlock()
	return re
}

func matchRule(val string, r model.FilterRule) bool {
	switch r.Op {
	case "regex":
		expr := r.Value
		if !r.CaseSensitive {
			expr = "(?i)" + expr
		}
		re := compiled(expr)
		return re != nil && re.MatchString(val)
	}

	v := r.Value
	if !r.CaseSensitive {
		val = strings.ToLower(val)
		v = strings.ToLower(v)
	}
	switch r.Op {
	case "contains":
		return strings.Contains(val, v)
	case "not_contains":
		return !strings.Contains(val, v)
	case "equals":
		return val == v
	case "starts_with":
		return strings.HasPrefix(val, v)
	case "ends_with":
		return strings.HasSuffix(val, v)
	}
	return false
}

func matchGroup(it *gofeed.Item, g model.FilterGroup) bool {
	if len(g.Rules) == 0 {
		return true
	}
	any := g.Mode == model.ModeAny || g.Mode == ""
	if any {
		for _, r := range g.Rules {
			if matchRule(fieldValue(it, r.Field), r) {
				return true
			}
		}
		return false
	}
	// "all" mode
	for _, r := range g.Rules {
		if !matchRule(fieldValue(it, r.Field), r) {
			return false
		}
	}
	return true
}

// ---------- Transformation ----------

var (
	tagRe = regexp.MustCompile(`<[^>]*>`)
	wsRe  = regexp.MustCompile(`\s+`)
)

func stripHTML(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func transformString(s string, tr model.TransformRule) string {
	switch tr.Op {
	case "replace":
		return strings.ReplaceAll(s, tr.Find, tr.Replace)
	case "regex_replace":
		re := compiled(tr.Find)
		if re == nil {
			return s
		}
		return re.ReplaceAllString(s, tr.Replace)
	case "prefix":
		return tr.Find + s
	case "suffix":
		return s + tr.Find
	case "strip_html":
		return stripHTML(s)
	case "trim":
		return strings.TrimSpace(s)
	case "lower":
		return strings.ToLower(s)
	case "upper":
		return strings.ToUpper(s)
	}
	return s
}

func applyTransforms(it *gofeed.Item, rules []model.TransformRule) {
	for _, tr := range rules {
		switch tr.Target {
		case "title":
			it.Title = transformString(it.Title, tr)
		case "description":
			it.Description = transformString(it.Description, tr)
		case "content":
			it.Content = transformString(it.Content, tr)
		case "link":
			it.Link = transformString(it.Link, tr)
		}
	}
}

// ---------- Link templating ----------

func slugify(s string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func titleCase(s string) string {
	var b strings.Builder
	prev := ' '
	for _, r := range s {
		if unicode.IsSpace(prev) || prev == '-' || prev == '_' || prev == '.' {
			b.WriteRune(unicode.ToTitle(r))
		} else {
			b.WriteRune(r)
		}
		prev = r
	}
	return b.String()
}

var epRe = regexp.MustCompile(`(?i)(?:s\d+e|ep?|episode\s*|-\s*)(\d{1,4})(?:v\d)?`)

func episodeNumber(title string) string {
	m := epRe.FindStringSubmatch(title)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

var tmplFuncs = template.FuncMap{
	"slug":    slugify,
	"lower":   strings.ToLower,
	"upper":   strings.ToUpper,
	"trim":    strings.TrimSpace,
	"title":   titleCase,
	"replace": strings.ReplaceAll,
	"episode": episodeNumber,
}

func renderLink(tmplStr string, it *gofeed.Item) (string, error) {
	if strings.TrimSpace(tmplStr) == "" {
		return it.Link, nil
	}
	t, err := template.New("link").Funcs(tmplFuncs).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("link template: %w", err)
	}
	author := ""
	if it.Author != nil {
		author = it.Author.Name
	}
	data := map[string]any{
		"Title":       it.Title,
		"Link":        it.Link,
		"Description": it.Description,
		"Content":     it.Content,
		"Author":      author,
		"GUID":        it.GUID,
		"Episode":     episodeNumber(it.Title),
		"Categories":  it.Categories,
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("link template: %w", err)
	}
	return strings.TrimSpace(b.String()), nil
}

// ---------- Main entry point ----------

func Apply(f *model.Feed, src *gofeed.Feed, feedLink string, now time.Time) *Result {
	res := &Result{
		Title:       f.Name,
		Link:        feedLink,
		Description: f.Description,
		SourceCount: len(src.Items),
	}

	var cutoff time.Time
	if f.Rules.MaxAgeDays > 0 {
		cutoff = now.AddDate(0, 0, -f.Rules.MaxAgeDays)
	}

	for _, src2 := range src.Items {
		if len(f.Rules.Include.Rules) > 0 && !matchGroup(src2, f.Rules.Include) {
			continue
		}
		if len(f.Rules.Exclude.Rules) > 0 && matchGroup(src2, f.Rules.Exclude) {
			continue
		}

		pub := time.Time{}
		if src2.PublishedParsed != nil {
			pub = *src2.PublishedParsed
		} else if src2.UpdatedParsed != nil {
			pub = *src2.UpdatedParsed
		}
		if !cutoff.IsZero() && !pub.IsZero() && pub.Before(cutoff) {
			continue
		}

		// Copy so we never mutate the cached source item.
		c := *src2
		applyTransforms(&c, f.Rules.Transforms)

		link, err := renderLink(f.Rules.LinkTemplate, &c)
		if err != nil || link == "" {
			link = c.Link
		}

		author := ""
		if c.Author != nil {
			author = c.Author.Name
		}

		guid := c.GUID
		if guid == "" {
			guid = src2.Link
		}

		res.Items = append(res.Items, Item{
			Title:       c.Title,
			Link:        link,
			Description: c.Description,
			Content:     c.Content,
			Author:      author,
			Published:   pub,
			GUID:        guid,
			Categories:  c.Categories,
		})
	}

	res.Matched = len(res.Items)

	// Sort newest-first (or oldest-first), keeping undated items at the end.
	sort.SliceStable(res.Items, func(i, j int) bool {
		a, b := res.Items[i].Published, res.Items[j].Published
		if a.IsZero() != b.IsZero() {
			return b.IsZero()
		}
		if a.Equal(b) {
			return false
		}
		if f.Rules.SortDesc {
			return a.After(b)
		}
		return a.Before(b)
	})

	if f.Rules.MaxItems > 0 && len(res.Items) > f.Rules.MaxItems {
		res.Items = res.Items[:f.Rules.MaxItems]
	}

	return res
}

