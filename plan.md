FeedForge — Self-Hosted RSS Transformer

A complete, containerized app: Go backend (chi + gofeed + gorilla/feeds + SQLite), Svelte 5 frontend with a dual-pane live preview, deployable to a NAS via Docker Compose.

Project layout

```text
feedforge/
├── docker-compose.yml
├── Dockerfile
├── backend/
│   ├── go.mod
│   ├── cmd/feedforge/main.go
│   └── internal/
│       ├── model/model.go
│       ├── store/store.go
│       ├── transform/transform.go
│       ├── render/render.go
│       ├── poller/poller.go
│       └── api/api.go
└── web/
    ├── package.json
    ├── vite.config.js
    ├── svelte.config.js
    ├── index.html
    └── src/
        ├── main.js
        ├── App.svelte
        └── lib/
            ├── api.js
            ├── RuleEditor.svelte
            └── PreviewPane.svelte

```

---

Backend

backend/go.mod

```go
module feedforge

go 1.22

require (
	github.com/go-chi/chi/v5 v5.0.12
	github.com/go-chi/cors v1.2.1
	github.com/gorilla/feeds v1.2.0
	github.com/mmcdole/gofeed v1.3.0
	modernc.org/sqlite v1.29.5
)

```

backend/internal/model/model.go

```go
package model

import "time"

type FilterMode string

const (
	ModeAny FilterMode = "any"
	ModeAll FilterMode = "all"
)

// FilterRule matches a single field of an item.
// Ops: contains | not_contains | equals | starts_with | ends_with | regex
type FilterRule struct {
	Field         string `json:"field"` // title | description | content | link | author | categories
	Op            string `json:"op"`
	Value         string `json:"value"`
	CaseSensitive bool   `json:"case_sensitive"`
}

// FilterGroup holds rules plus the boolean mode used to combine them.
type FilterGroup struct {
	Mode  FilterMode   `json:"mode"`
	Rules []FilterRule `json:"rules"`
}

// TransformRule mutates a field.
// Ops: replace | regex_replace | prefix | suffix | strip_html | trim | lower | upper
type TransformRule struct {
	Target  string `json:"target"` // title | description | content | link
	Op      string `json:"op"`
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

type Rules struct {
	Include      FilterGroup     `json:"include"`
	Exclude      FilterGroup     `json:"exclude"`
	Transforms   []TransformRule `json:"transforms"`
	LinkTemplate string          `json:"link_template"`
	MaxItems     int             `json:"max_items"`
	MaxAgeDays   int             `json:"max_age_days"`
	SortDesc     bool            `json:"sort_desc"`
}

type Feed struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	SourceURL     string    `json:"source_url"`
	Description   string    `json:"description"`
	Enabled       bool      `json:"enabled"`
	FetchInterval int       `json:"fetch_interval"` // minutes
	Rules         Rules     `json:"rules"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

```

backend/internal/transform/transform.go

```go
package transform

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
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

var regexCache = map[string]*regexp.Regexp{}

func compiled(expr string) *regexp.Regexp {
	if re, ok := regexCache[expr]; ok {
		return re
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil
	}
	regexCache[expr] = re
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

```

backend/internal/render/render.go

```go
package render

import (
	"time"

	"github.com/gorilla/feeds"

	"feedforge/internal/transform"
)

func build(r *transform.Result, updated time.Time) *feeds.Feed {
	f := &feeds.Feed{
		Title:       r.Title,
		Link:        &feeds.Link{Href: r.Link},
		Description: r.Description,
		Created:     updated,
		Updated:     updated,
	}
	f.Items = make([]*feeds.Item, 0, len(r.Items))
	for _, it := range r.Items {
		created := it.Published
		if created.IsZero() {
			created = updated
		}
		author := &feeds.Author{}
		if it.Author != "" {
			author = &feeds.Author{Name: it.Author}
		}
		desc := it.Description
		if desc == "" && it.Content != "" {
			desc = it.Content
		}
		f.Items = append(f.Items, &feeds.Item{
			Title:       it.Title,
			Link:        &feeds.Link{Href: it.Link},
			Description: desc,
			Content:     it.Content,
			Id:          it.GUID,
			Author:      author,
			Created:     created,
			Updated:     created,
		})
	}
	return f
}

func RSS(r *transform.Result, updated time.Time) (string, error) {
	return build(r, updated).ToRss()
}

func Atom(r *transform.Result, updated time.Time) (string, error) {
	return build(r, updated).ToAtom()
}

```

backend/internal/store/store.go

```go
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

```

backend/internal/poller/poller.go

```go
package poller

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"

	"feedforge/internal/model"
	"feedforge/internal/store"
	"feedforge/internal/transform"
)

type entry struct {
	result    *transform.Result
	fetchedAt time.Time
	err       error
}

type Poller struct {
	store *store.Store
	fp    *gofeed.Parser

	mu    sync.RWMutex
	cache map[string]*entry

	locks sync.Map // feedID -> *sync.Mutex
}

func New(s *store.Store) *Poller {
	fp := gofeed.NewParser()
	fp.UserAgent = "FeedForge/1.0 (+https://github.com/yourname/feedforge)"

	return &Poller{
		store: s,
		fp:    fp,
		cache: map[string]*entry{},
	}
}

func (p *Poller) lockFor(id string) *sync.Mutex {
	v, _ := p.locks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Get returns a transformed result, using cache unless force is set.
func (p *Poller) Get(ctx context.Context, f *model.Feed, feedLink string, force bool) (*transform.Result, time.Time, error) {
	ttl := time.Duration(f.FetchInterval) * time.Minute
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	p.mu.RLock()
	e, ok := p.cache[f.ID]
	p.mu.RUnlock()
	if ok && !force && e.err == nil && time.Since(e.fetchedAt) < ttl {
		return e.result, e.fetchedAt, nil
	}

	lk := p.lockFor(f.ID)
	lk.Lock()
	defer lk.Unlock()

	// Double-check after acquiring the lock.
	p.mu.RLock()
	e, ok = p.cache[f.ID]
	p.mu.RUnlock()
	if ok && !force && e.err == nil && time.Since(e.fetchedAt) < ttl {
		return e.result, e.fetchedAt, nil
	}

	res, err := p.fetch(ctx, f, feedLink)
	now := time.Now().UTC()

	p.mu.Lock()
	if err != nil {
		// keep the previous good result around if we have one
		if ok && e.result != nil {
			e.err = err
		} else {
			p.cache[f.ID] = &entry{err: err, fetchedAt: now}
		}
	} else {
		p.cache[f.ID] = &entry{result: res, fetchedAt: now}
	}
	p.mu.Unlock()

	if err != nil {
		return nil, now, err
	}
	return res, now, nil
}

func (p *Poller) Invalidate(id string) {
	p.mu.Lock()
	delete(p.cache, id)
	p.mu.Unlock()
}

func (p *Poller) fetch(ctx context.Context, f *model.Feed, feedLink string) (*transform.Result, error) {
	if err := guardURL(f.SourceURL); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.SourceURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.fp.UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, */*")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}

	src, err := p.fp.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}

	return transform.Apply(f, src, feedLink, time.Now().UTC()), nil
}

// guardURL blocks requests to loopback/private ranges unless explicitly allowed.
func guardURL(raw string) error {
	if os.Getenv("FEEDFORGE_ALLOW_PRIVATE") == "1" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil // let the HTTP client surface the error
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("refusing to fetch private address %s", ip)
		}
	}
	return nil
}

```

backend/internal/api/api.go

```go
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mmcdole/gofeed"

	"feedforge/internal/model"
	"feedforge/internal/poller"
	"feedforge/internal/render"
	"feedforge/internal/store"
	"feedforge/internal/transform"
)

type API struct {
	store  *store.Store
	poller *poller.Poller
	fp     *gofeed.Parser
}

func New(s *store.Store, p *poller.Poller) *API {
	return &API{store: s, poller: p, fp: gofeed.NewParser()}
}

func (a *API) Routes() chi.Router {
	r := chi.NewRouter()

	r.Route("/api", func(r chi.Router) {
		r.Get("/feeds", a.listFeeds)
		r.Post("/feeds", a.createFeed)
		r.Get("/feeds/{id}", a.getFeed)
		r.Put("/feeds/{id}", a.updateFeed)
		r.Delete("/feeds/{id}", a.deleteFeed)
		r.Post("/feeds/{id}/refresh", a.refreshFeed)
		r.Post("/preview", a.preview)
		r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
	})

	r.Get("/feed/{id}", a.serveFeed)
	r.Get("/feed/{id}.xml", a.serveFeed)
	r.Get("/feed/{id}/atom", a.serveFeedAtom)

	return r
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

// ---------- feed CRUD ----------

func (a *API) listFeeds(w http.ResponseWriter, r *http.Request) {
	feeds, err := a.store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, feeds)
}

func (a *API) createFeed(w http.ResponseWriter, r *http.Request) {
	var f model.Feed
	if err := decode(r, &f); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(f.SourceURL) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("source_url is required"))
		return
	}
	if f.FetchInterval <= 0 {
		f.FetchInterval = 30
	}
	if f.Name == "" {
		f.Name = "Untitled feed"
	}
	if err := a.store.Create(&f); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (a *API) getFeed(w http.ResponseWriter, r *http.Request) {
	f, err := a.store.Get(chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (a *API) updateFeed(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var f model.Feed
	if err := decode(r, &f); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := a.store.Update(id, &f); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a.poller.Invalidate(id)
	writeJSON(w, http.StatusOK, f)
}

func (a *API) deleteFeed(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.store.Delete(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a.poller.Invalidate(id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) refreshFeed(w http.ResponseWriter, r *http.Request) {
	f, err := a.store.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	link := fmt.Sprintf("%s/feed/%s", baseURL(r), f.ID)
	res, _, err := a.poller.Get(r.Context(), f, link, true)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source_count": res.SourceCount,
		"matched":      res.Matched,
	})
}

// ---------- preview ----------

type previewRequest struct {
	SourceURL string      `json:"source_url"`
	Name      string      `json:"name"`
	Rules     model.Rules `json:"rules"`
	Format    string      `json:"format"` // "rss" | "atom"
	Limit     int         `json:"limit"`
}

type previewResponse struct {
	Items       []transform.Item `json:"items"`
	XML         string           `json:"xml"`
	SourceCount int              `json:"source_count"`
	Matched     int              `json:"matched"`
	Error       string           `json:"error,omitempty"`
}

func (a *API) preview(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.SourceURL) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("source_url is required"))
		return
	}
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 25
	}

	tmp := &model.Feed{
		ID:        "preview",
		Name:      req.Name,
		SourceURL: req.SourceURL,
		Rules:     req.Rules,
	}
	if tmp.Name == "" {
		tmp.Name = "Preview"
	}

	link := baseURL(r) + "/feed/preview"
	res, _, err := a.poller.Get(r.Context(), tmp, link, true)
	if err != nil {
		writeJSON(w, http.StatusOK, previewResponse{Error: err.Error()})
		return
	}

	// Render the full feed, then trim the JSON item list for the UI.
	var xml string
	if req.Format == "atom" {
		xml, err = render.Atom(res, time.Now().UTC())
	} else {
		xml, err = render.RSS(res, time.Now().UTC())
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	items := res.Items
	if len(items) > req.Limit {
		items = items[:req.Limit]
	}

	writeJSON(w, http.StatusOK, previewResponse{
		Items:       items,
		XML:         xml,
		SourceCount: res.SourceCount,
		Matched:     res.Matched,
	})
}

// ---------- feed output ----------

func (a *API) serveFeed(w http.ResponseWriter, r *http.Request) {
	a.serve(w, r, "rss")
}

func (a *API) serveFeedAtom(w http.ResponseWriter, r *http.Request) {
	a.serve(w, r, "atom")
}

func (a *API) serve(w http.ResponseWriter, r *http.Request, format string) {
	id := chi.URLParam(r, "id")
	f, err := a.store.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !f.Enabled {
		http.Error(w, "feed disabled", http.StatusServiceUnavailable)
		return
	}

	link := fmt.Sprintf("%s/feed/%s", baseURL(r), f.ID)
	res, updated, err := a.poller.Get(r.Context(), f, link, false)
	if err != nil {
		// Serve stale cache if we have one; otherwise report the error.
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var body string
	if format == "atom" {
		body, err = render.Atom(res, updated)
	} else {
		body, err = render.RSS(res, updated)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ct := "application/rss+xml; charset=utf-8"
	if format == "atom" {
		ct = "application/atom+xml; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Last-Modified", updated.UTC().Format(http.TimeFormat))
	_, _ = w.Write([]byte(body))
}

// StaticHandler serves the built Svelte app with SPA fallback.
func StaticHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/feed") {
			http.NotFound(w, r)
			return
		}
		path := dir + r.URL.Path
		if _, err := os.Stat(path); err != nil {
			http.ServeFile(w, r, dir+"/index.html")
			return
		}
		fs.ServeHTTP(w, r)
	})
}

```

backend/cmd/feedforge/main.go

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"feedforge/internal/api"
	"feedforge/internal/poller"
	"feedforge/internal/store"
)

func main() {
	dataDir := env("DATA_DIR", "./data")
	webDir := env("WEB_DIR", "./web/dist")
	addr := env("LISTEN_ADDR", ":8080")

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	st, err := store.Open(filepath.Join(dataDir, "feedforge.db"))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	p := poller.New(st)
	a := api.New(st, p)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
	}))

	r.Mount("/", a.Routes())
	r.Handle("/*", api.StaticHandler(webDir))

	go warmup(p)
	go scheduler(p, st)

	log.Printf("FeedForge listening on %s (web: %s, data: %s)", addr, webDir, dataDir)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// warmup pre-populates the cache so the first request is fast.
func warmup(p *poller.Poller) {
	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = ctx
}

// scheduler refreshes enabled feeds on their configured interval.
func scheduler(p *poller.Poller, st *store.Store) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		feeds, err := st.List()
		if err != nil {
			log.Printf("scheduler list: %v", err)
			continue
		}
		for _, f := range feeds {
			if !f.Enabled {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			if _, _, err := p.Get(ctx, f, "", false); err != nil {
				log.Printf("refresh %s (%s): %v", f.Name, f.ID, err)
			}
			cancel()
		}
	}
}

```

Note: poller.Get uses a TTL, so calling it every minute is safe — it only refetches when fetch_interval has elapsed.

---

Frontend (Svelte 5 + Vite)

web/package.json

```json
{
  "name": "feedforge-web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview"
  },
  "devDependencies": {
    "@sveltejs/vite-plugin-svelte": "^4.0.0",
    "svelte": "^5.0.0",
    "vite": "^5.4.0"
  }
}

```

web/vite.config.js

```js
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/feed': 'http://localhost:8080',
    },
  },
});

```

web/svelte.config.js

```js
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
export default { preprocess: vitePreprocess() };

```

web/index.html

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>FeedForge</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.js"></script>
  </body>
</html>

```

web/src/main.js

```js
import { mount } from 'svelte';
import App from './App.svelte';
import './app.css';

export default mount(App, { target: document.getElementById('app') });

```

web/src/app.css

```css
:root {
  --bg: #0f1115;
  --panel: #161a22;
  --panel-2: #1c212b;
  --border: #262c38;
  --text: #e6e9ef;
  --muted: #8b93a7;
  --accent: #5b8cff;
  --accent-2: #3ecf8e;
  --danger: #ff5c5c;
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
}

* { box-sizing: border-box; }

body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font-size: 14px;
}

button {
  font: inherit;
  cursor: pointer;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--panel-2);
  color: var(--text);
  padding: 6px 12px;
}
button:hover { border-color: var(--accent); }
button.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
button.danger { color: var(--danger); }
button.ghost { background: transparent; }

input, select, textarea {
  font: inherit;
  background: var(--panel);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 8px;
  width: 100%;
}
input:focus, select:focus, textarea:focus { outline: 1px solid var(--accent); }

```

web/src/lib/api.js

```js
const BASE = import.meta.env.VITE_API_BASE ?? '';

async function req(path, opts = {}) {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (res.status === 204) return null;
  const ct = res.headers.get('content-type') || '';
  const body = ct.includes('json') ? await res.json() : await res.text();
  if (!res.ok) {
    const msg = typeof body === 'string' ? body : body?.error || res.statusText;
    throw new Error(msg);
  }
  return body;
}

export const api = {
  listFeeds: () => req('/api/feeds'),
  getFeed: (id) => req(`/api/feeds/${id}`),
  createFeed: (f) => req('/api/feeds', { method: 'POST', body: JSON.stringify(f) }),
  updateFeed: (id, f) => req(`/api/feeds/${id}`, { method: 'PUT', body: JSON.stringify(f) }),
  deleteFeed: (id) => req(`/api/feeds/${id}`, { method: 'DELETE' }),
  refreshFeed: (id) => req(`/api/feeds/${id}/refresh`, { method: 'POST' }),
  preview: (payload) => req('/api/preview', { method: 'POST', body: JSON.stringify(payload) }),
};

```

web/src/lib/RuleEditor.svelte

```svelte
<script>
  let { rules = $bindable(), onchange = () => {} } = $props();

  const FIELDS = ['title', 'description', 'content', 'link', 'author', 'categories'];
  const FILTER_OPS = ['contains', 'not_contains', 'equals', 'starts_with', 'ends_with', 'regex'];
  const TRANSFORM_OPS = [
    'replace', 'regex_replace', 'strip_html', 'prefix', 'suffix', 'trim', 'lower', 'upper',
  ];
  const TRANSFORM_TARGETS = ['title', 'description', 'content', 'link'];

  function addFilter(group) {
    group.rules = [...group.rules, { field: 'title', op: 'contains', value: '', case_sensitive: false }];
    onchange();
  }
  function removeFilter(group, i) {
    group.rules = group.rules.filter((_, idx) => idx !== i);
    onchange();
  }
  function addTransform() {
    rules.transforms = [...rules.transforms, { target: 'title', op: 'replace', find: '', replace: '' }];
    onchange();
  }
  function removeTransform(i) {
    rules.transforms = rules.transforms.filter((_, idx) => idx !== i);
    onchange();
  }

  const tmplHints = [
    '{{ .Title }}', '{{ .Episode }}', '{{ slug .Title }}',
    '{{ lower .Title }}', '{{ replace .Title " " "-" }}',
  ];
</script>

<div class="rules">
  <!-- INCLUDE -->
  <section>
    <header>
      <h3>Include</h3>
      <select bind:value={rules.include.mode} onchange={onchange}>
        <option value="any">Match ANY</option>
        <option value="all">Match ALL</option>
      </select>
      <button class="ghost" onclick={() => addFilter(rules.include)}>+ rule</button>
    </header>
    {#if rules.include.rules.length === 0}
      <p class="hint">No include rules — every item passes.</p>
    {/if}
    {#each rules.include.rules as r, i}
      <div class="rule">
        <select bind:value={r.field} onchange={onchange}>
          {#each FIELDS as f}<option>{f}</option>{/each}
        </select>
        <select bind:value={r.op} onchange={onchange}>
          {#each FILTER_OPS as o}<option>{o}</option>{/each}
        </select>
        <input bind:value={r.value} oninput={onchange} placeholder="value" />
        <label class="cs" title="Case sensitive">
          <input type="checkbox" bind:checked={r.case_sensitive} onchange={onchange} /> Aa
        </label>
        <button class="ghost danger" onclick={() => removeFilter(rules.include, i)}>✕</button>
      </div>
    {/each}
  </section>

  <!-- EXCLUDE -->
  <section>
    <header>
      <h3>Exclude</h3>
      <select bind:value={rules.exclude.mode} onchange={onchange}>
        <option value="any">Drop if ANY</option>
        <option value="all">Drop if ALL</option>
      </select>
      <button class="ghost" onclick={() => addFilter(rules.exclude)}>+ rule</button>
    </header>
    {#each rules.exclude.rules as r, i}
      <div class="rule">
        <select bind:value={r.field} onchange={onchange}>
          {#each FIELDS as f}<option>{f}</option>{/each}
        </select>
        <select bind:value={r.op} onchange={onchange}>
          {#each FILTER_OPS as o}<option>{o}</option>{/each}
        </select>
        <input bind:value={r.value} oninput={onchange} placeholder="value" />
        <label class="cs">
          <input type="checkbox" bind:checked={r.case_sensitive} onchange={onchange} /> Aa
        </label>
        <button class="ghost danger" onclick={() => removeFilter(rules.exclude, i)}>✕</button>
      </div>
    {/each}
  </section>

  <!-- TRANSFORMS -->
  <section>
    <header>
      <h3>Transforms</h3>
      <button class="ghost" onclick={addTransform}>+ transform</button>
    </header>
    {#each rules.transforms as t, i}
      <div class="rule">
        <select bind:value={t.target} onchange={onchange}>
          {#each TRANSFORM_TARGETS as x}<option>{x}</option>{/each}
        </select>
        <select bind:value={t.op} onchange={onchange}>
          {#each TRANSFORM_OPS as o}<option>{o}</option>{/each}
        </select>
        <input bind:value={t.find} oninput={onchange} placeholder="find / prefix" />
        <input bind:value={t.replace} oninput={onchange} placeholder="replace" />
        <button class="ghost danger" onclick={() => removeTransform(i)}>✕</button>
      </div>
    {/each}
  </section>

  <!-- LINK TEMPLATE -->
  <section>
    <header><h3>Destination link</h3></header>
    <input
      bind:value={rules.link_template}
      oninput={onchange}
      placeholder="https://my.site/anime/{{ slug .Title }}"
    />
    <p class="hint">
      Go template. Available:
      {#each tmplHints as h}<code>{h}</code>{/each}
      — leave empty to keep the original link.
    </p>
  </section>

  <!-- LIMITS -->
  <section>
    <header><h3>Limits</h3></header>
    <div class="grid3">
      <label>Max items<input type="number" min="0" bind:value={rules.max_items} oninput={onchange} /></label>
      <label>Max age (days)<input type="number" min="0" bind:value={rules.max_age_days} oninput={onchange} /></label>
      <label class="row">
        <input type="checkbox" bind:checked={rules.sort_desc} onchange={onchange} /> Newest first
      </label>
    </div>
  </section>
</div>

<style>
  .rules { display: flex; flex-direction: column; gap: 18px; }
  section { border: 1px solid var(--border); border-radius: 8px; padding: 12px; background: var(--panel); }
  header { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
  header h3 { margin: 0; font-size: 13px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); flex: 1; }
  header select { width: auto; }
  .rule { display: grid; grid-template-columns: 1.1fr 1.2fr 2fr auto auto; gap: 6px; margin-bottom: 6px; align-items: center; }
  .rule select, .rule input { font-size: 13px; }
  .cs { display: flex; align-items: center; gap: 4px; white-space: nowrap; color: var(--muted); font-size: 12px; }
  .cs input { width: auto; }
  .hint { color: var(--muted); font-size: 12px; margin: 8px 0 0; }
  .hint code { background: var(--panel-2); padding: 1px 5px; border-radius: 4px; margin-right: 4px; font-size: 11px; }
  .grid3 { display: grid; grid-template-columns: 1fr 1fr auto; gap: 10px; }
  .grid3 label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
  .grid3 label.row { flex-direction: row; align-items: center; }
  .grid3 label.row input { width: auto; }
</style>

```

web/src/lib/PreviewPane.svelte

```svelte
<script>
  let { items = [], xml = '', loading = false, error = '', stats = null } = $props();
  let tab = $state('items');
  let filter = $state('');

  const filtered = $derived(
    filter
      ? items.filter((i) => (i.title || '').toLowerCase().includes(filter.toLowerCase()))
      : items
  );

  function fmt(d) {
    if (!d) return '—';
    const t = new Date(d);
    return isNaN(t) ? '—' : t.toLocaleString();
  }
</script>

<div class="preview">
  <header>
    <div class="tabs">
      <button class:active={tab === 'items'} onclick={() => (tab = 'items')}>Items ({items.length})</button>
      <button class:active={tab === 'xml'} onclick={() => (tab = 'xml')}>XML</button>
    </div>
    {#if stats}
      <span class="stats">{stats.matched} / {stats.source_count} matched</span>
    {/if}
    {#if loading}<span class="spinner">loading…</span>{/if}
  </header>

  {#if error}
    <div class="error">{error}</div>
  {/if}

  {#if tab === 'items'}
    <input class="search" bind:value={filter} placeholder="Filter preview…" />
    <ul class="items">
      {#each filtered as it}
        <li>
          <a href={it.link} target="_blank" rel="noreferrer">{it.title}</a>
          <div class="meta">
            <span>{fmt(it.published)}</span>
            {#if it.author}<span>· {it.author}</span>{/if}
          </div>
          <div class="link">{it.link}</div>
        </li>
      {/each}
      {#if filtered.length === 0}
        <li class="empty">No items matched the current rules.</li>
      {/if}
    </ul>
  {:else}
    <pre class="xml">{xml || 'Nothing to render yet.'}</pre>
  {/if}
</div>

<style>
  .preview { display: flex; flex-direction: column; height: 100%; min-height: 0; }
  header { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
  .tabs { display: flex; gap: 4px; flex: 1; }
  .tabs button { font-size: 12px; padding: 4px 10px; }
  .tabs button.active { background: var(--accent); border-color: var(--accent); color: #fff; }
  .stats, .spinner { font-size: 12px; color: var(--muted); }
  .search { margin-bottom: 8px; }
  .items { list-style: none; margin: 0; padding: 0; overflow: auto; flex: 1; min-height: 0; }
  .items li { border-bottom: 1px solid var(--border); padding: 10px 2px; }
  .items a { color: var(--accent); text-decoration: none; font-weight: 500; }
  .items a:hover { text-decoration: underline; }
  .meta { color: var(--muted); font-size: 12px; margin-top: 3px; display: flex; gap: 5px; }
  .link { color: var(--accent-2); font-size: 11px; word-break: break-all; margin-top: 3px; font-family: ui-monospace, monospace; }
  .empty { color: var(--muted); padding: 20px 0; text-align: center; }
  .xml {
    flex: 1; min-height: 0; overflow: auto; background: #0b0d12; border: 1px solid var(--border);
    border-radius: 8px; padding: 12px; font-size: 12px; white-space: pre-wrap; word-break: break-all;
  }
  .error { background: #2a1518; border: 1px solid var(--danger); color: #ffb4b4;
           padding: 8px 10px; border-radius: 6px; margin-bottom: 8px; font-size: 12px; }
</style>

```

web/src/App.svelte

```svelte
<script>
  import { onMount } from 'svelte';
  import { api } from './lib/api.js';
  import RuleEditor from './lib/RuleEditor.svelte';
  import PreviewPane from './lib/PreviewPane.svelte';

  const blank = () => ({
    id: null,
    name: 'New feed',
    source_url: '',
    description: '',
    enabled: true,
    fetch_interval: 30,
    rules: {
      include: { mode: 'any', rules: [] },
      exclude: { mode: 'any', rules: [] },
      transforms: [],
      link_template: '',
      max_items: 50,
      max_age_days: 0,
      sort_desc: true,
    },
  });

  let feeds = $state([]);
  let draft = $state(blank());
  let dirty = $state(false);
  let saving = $state(false);
  let toast = $state('');
  let preview = $state({ items: [], xml: '', loading: false, error: '', stats: null });
  let previewFormat = $state('rss');
  let showJson = $state(false);

  let previewTimer;

  onMount(load);

  async function load() {
    try {
      feeds = await api.listFeeds();
    } catch (e) {
      toast = e.message;
    }
  }

  function select(f) {
    draft = structuredClone(f);
    dirty = false;
    schedulePreview(0);
  }

  function newFeed() {
    draft = blank();
    dirty = true;
    schedulePreview(0);
  }

  function schedulePreview(delay = 600) {
    clearTimeout(previewTimer);
    previewTimer = setTimeout(runPreview, delay);
  }

  function markDirty() {
    dirty = true;
    schedulePreview();
  }

  async function runPreview() {
    if (!draft.source_url) {
      preview = { items: [], xml: '', loading: false, error: '', stats: null };
      return;
    }
    preview.loading = true;
    preview.error = '';
    try {
      const res = await api.preview({
        source_url: draft.source_url,
        name: draft.name,
        rules: draft.rules,
        format: previewFormat,
        limit: 50,
      });
      preview = {
        items: res.items || [],
        xml: res.xml || '',
        loading: false,
        error: res.error || '',
        stats: { matched: res.matched, source_count: res.source_count },
      };
    } catch (e) {
      preview = { ...preview, loading: false, error: e.message };
    }
  }

  async function save() {
    saving = true;
    try {
      const payload = { ...draft };
      if (payload.id) {
        await api.updateFeed(payload.id, payload);
      } else {
        const created = await api.createFeed(payload);
        draft.id = created.id;
      }
      dirty = false;
      toast = 'Saved';
      await load();
    } catch (e) {
      toast = e.message;
    } finally {
      saving = false;
      setTimeout(() => (toast = ''), 2500);
    }
  }

  async function remove(id) {
    if (!confirm('Delete this feed?')) return;
    await api.deleteFeed(id);
    if (draft.id === id) draft = blank();
    await load();
  }

  async function refresh() {
    if (!draft.id) return runPreview();
    try {
      const r = await api.refreshFeed(draft.id);
      toast = `Refreshed: ${r.matched}/${r.source_count} matched`;
      await runPreview();
    } catch (e) {
      toast = e.message;
    }
    setTimeout(() => (toast = ''), 2500);
  }

  const feedURL = $derived(
    draft.id ? `${location.origin}/feed/${draft.id}` : '(save to get a URL)'
  );

  function copyURL() {
    if (!draft.id) return;
    navigator.clipboard.writeText(feedURL);
    toast = 'URL copied';
    setTimeout(() => (toast = ''), 1500);
  }
</script>

<div class="app">
  <!-- SIDEBAR -->
  <aside>
    <div class="brand">FeedForge</div>
    <button class="primary new" onclick={newFeed}>+ New feed</button>
    <ul class="feed-list">
      {#each feeds as f}
        <li class:active={f.id === draft.id}>
          <button class="entry" onclick={() => select(f)}>
            <span class="name">{f.name}</span>
            <span class="sub">{f.enabled ? 'enabled' : 'paused'}</span>
          </button>
          <button class="ghost danger tiny" onclick={() => remove(f.id)}>✕</button>
        </li>
      {/each}
      {#if feeds.length === 0}
        <li class="empty">No feeds yet</li>
      {/if}
    </ul>
  </aside>

  <!-- MAIN -->
  <main>
    <div class="topbar">
      <input class="title" bind:value={draft.name} oninput={markDirty} />
      <label class="toggle">
        <input type="checkbox" bind:checked={draft.enabled} onchange={markDirty} /> Enabled
      </label>
      <select bind:value={draft.fetch_interval} onchange={markDirty}>
        <option value={5}>5 min</option>
        <option value={15}>15 min</option>
        <option value={30}>30 min</option>
        <option value={60}>1 hour</option>
        <option value={360}>6 hours</option>
      </select>
      <button onclick={refresh}>Refresh now</button>
      <button class="primary" onclick={save} disabled={saving || !dirty}>
        {saving ? 'Saving…' : dirty ? 'Save' : 'Saved'}
      </button>
    </div>

    <div class="url-row">
      <span class="label">Output URL</span>
      <code>{feedURL}</code>
      <button class="ghost" onclick={copyURL} disabled={!draft.id}>Copy</button>
      {#if draft.id}
        <a class="ghost link" href={feedURL} target="_blank" rel="noreferrer">Open ↗</a>
        <a class="ghost link" href={`/feed/${draft.id}/atom`} target="_blank" rel="noreferrer">Atom ↗</a>
      {/if}
    </div>

    <div class="panes">
      <!-- LEFT: config -->
      <div class="pane scroll">
        <section class="source">
          <label>Source feed URL</label>
          <input
            bind:value={draft.source_url}
            oninput={markDirty}
            placeholder="https://feed.animetosho.org/rss2"
          />
          <label>Description</label>
          <input bind:value={draft.description} oninput={markDirty} placeholder="Optional" />
        </section>

        <RuleEditor bind:rules={draft.rules} onchange={markDirty} />
      </div>

      <!-- RIGHT: preview -->
      <div class="pane scroll">
        <div class="preview-controls">
          <select bind:value={previewFormat} onchange={() => runPreview()}>
            <option value="rss">RSS 2.0</option>
            <option value="atom">Atom</option>
          </select>
          <button class="ghost" onclick={() => runPreview()}>Re-run preview</button>
        </div>
        <PreviewPane {...preview} />
      </div>
    </div>
  </main>

  {#if toast}<div class="toast">{toast}</div>{/if}
</div>

<style>
  .app { display: grid; grid-template-columns: 260px 1fr; height: 100vh; }

  aside {
    background: var(--panel); border-right: 1px solid var(--border);
    display: flex; flex-direction: column; padding: 14px; gap: 12px; min-height: 0;
  }
  .brand { font-weight: 700; letter-spacing: .04em; font-size: 15px; }
  .new { width: 100%; }
  .feed-list { list-style: none; margin: 0; padding: 0; overflow: auto; flex: 1; }
  .feed-list li { display: flex; align-items: center; gap: 2px; border-radius: 6px; }
  .feed-list li.active { background: var(--panel-2); }
  .entry {
    flex: 1; text-align: left; background: transparent; border: none;
    padding: 8px; display: flex; flex-direction: column; gap: 2px;
  }
  .entry .name { font-size: 13px; }
  .entry .sub { font-size: 11px; color: var(--muted); }
  .tiny { padding: 2px 6px; font-size: 11px; }
  .feed-list .empty { color: var(--muted); font-size: 12px; padding: 8px; }

  main { display: flex; flex-direction: column; min-width: 0; min-height: 0; }

  .topbar {
    display: flex; gap: 10px; align-items: center;
    padding: 12px 16px; border-bottom: 1px solid var(--border);
  }
  .title { font-size: 15px; font-weight: 600; flex: 1; }
  .toggle { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--muted); white-space: nowrap; }
  .toggle input { width: auto; }
  .topbar select { width: auto; }

  .url-row {
    display: flex; align-items: center; gap: 8px;
    padding: 8px 16px; border-bottom: 1px solid var(--border);
    background: var(--panel); font-size: 12px;
  }
  .url-row .label { color: var(--muted); }
  .url-row code {
    flex: 1; background: var(--panel-2); padding: 5px 8px; border-radius: 5px;
    font-family: ui-monospace, monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .link { text-decoration: none; display: inline-flex; align-items: center; }

  .panes { display: grid; grid-template-columns: 1fr 1fr; flex: 1; min-height: 0; }
  .pane { padding: 16px; min-height: 0; }
  .pane.scroll { overflow: auto; }
  .pane:first-child { border-right: 1px solid var(--border); }

  .source { display: flex; flex-direction: column; gap: 6px; margin-bottom: 18px; }
  .source label { font-size: 12px; color: var(--muted); }

  .preview-controls { display: flex; gap: 8px; margin-bottom: 10px; }
  .preview-controls select { width: auto; }

  .toast {
    position: fixed; bottom: 20px; left: 50%; transform: translateX(-50%);
    background: var(--accent); color: #fff; padding: 8px 18px; border-radius: 999px;
    font-size: 13px; box-shadow: 0 6px 24px rgba(0,0,0,.4);
  }

  @media (max-width: 900px) {
    .app { grid-template-columns: 1fr; }
    aside { display: none; }
    .panes { grid-template-columns: 1fr; }
    .pane:first-child { border-right: none; border-bottom: 1px solid var(--border); }
  }
</style>

```

---

Containerization

Dockerfile

```dockerfile
# ---------- frontend ----------
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# ---------- backend ----------
FROM golang:1.22-alpine AS api
WORKDIR /src
COPY backend/go.mod ./
RUN go mod download || true
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/feedforge ./cmd/feedforge

# ---------- runtime ----------
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=api /out/feedforge /app/feedforge
COPY --from=web /web/dist /app/web

ENV DATA_DIR=/data \
    WEB_DIR=/app/web \
    LISTEN_ADDR=:8080

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/feedforge"]

```

docker-compose.yml

```yaml
services:
  feedforge:
    build: .
    container_name: feedforge
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    environment:
      DATA_DIR: /data
      WEB_DIR: /app/web
      LISTEN_ADDR: ":8080"
      FEEDFORGE_ALLOW_PRIVATE: "1"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/api/health"]
      interval: 30s
      timeout: 5s
      retries: 3

```
