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

func (a *API) Routes(r chi.Router) {
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

	saved, err := a.store.Get(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
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
		xml, err = render.Atom(res, link, time.Now().UTC())
	} else {
		xml, err = render.RSS(res, link, time.Now().UTC())
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
	if err != nil && res == nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err != nil {
		// upstream failed this refresh; serving the last good copy
		w.Header().Set("Warning", `110 feedforge "Response is Stale"`)
	}

	var body string
	if format == "atom" {
		body, err = render.Atom(res, link, updated)
	} else {
		body, err = render.RSS(res, link, updated)
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
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/feed/") {
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

