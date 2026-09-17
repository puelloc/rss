# FeedForge — Progress

## Status (2026-09-16 — verified against code on disk)

All work complete. `go build ./...` + `go vet ./...` clean. Smoke test passed end-to-end
(details at the bottom). Code in `backend/`, `web/`, `Dockerfile`, `docker-compose.yml`.

### What is actually implemented (checked by reading the files)

- **main.go** (`backend/cmd/feedforge/main.go`): single flat chi router —
  `a.Routes(r)` registers all API/feed routes directly on it, then
  `r.NotFound(spa.ServeHTTP)` is the SPA fallback. `r.Mount`/`r.Handle("/*")` are gone
  (that combo was a real bug: both patterns were `/*` and `Handle` shadowed the mounted
  API router, 404-ing every `/api` route — fixed 2026-09-16, see test log below).
  **Warmup is real** (Fix 3): waits 2s, lists feeds from the store, `p.Get` per enabled
  feed in goroutines with a 45s timeout. Middleware: Logger, Recoverer, Timeout(60s), CORS.
- **Routes** (flat, no nesting): `/api/feeds*` (GET/POST/PUT/DELETE/{id}/refresh),
  `/api/preview`, `/api/health`, plus feed output `/feed/{id}`, `/feed/{id}.xml`,
  `/feed/{id}/atom` — **no `?format=` switch; output picked by route**.
- **api.go**: preview body is `{ items: gofeed.Item[], rules }` — local testing, no
  upstream URL. `serve()` renders via explicit route, sets `Warning: 110 feedforge
  "Response is Stale"` when the poller serves a prior good copy.
  `updateFeed()` re-reads the persisted feed after `Update` before returning it (Fix 6).
  `StaticHandler(dir)` = `http.FileServer` + SPA fallback to `index.html`, with 404 for
  `/api/*` and `/feed/*` paths (matters because it's also the NotFound handler).
- **poller.go**: `http.Client` with 15s timeout + custom `Transport.DialContext`
  `guardedDialContext` — SSRF check on the **dial**, so every hop (incl. redirects;
  client has no `CheckRedirect`, so the default 10-redirect cap applies) is covered.
  `guardURL()` only enforces scheme http/https. `allowPrivate()` reads
  `FEEDFORGE_ALLOW_PRIVATE` (accepts `1` or `true`). `Get(ctx, f, link, force=false)`:
  cache hit unless TTL expired or `force`; fetch; transform; cache; on refresh error with
  a prior good copy → return stale copy + err (Fix 4). `Refresh` = `Get(ctx, f, link, true)`.
- **transform.go**: regex cache with `sync.RWMutex` + 1000-entry cap + `compileLimit`
  (Fix 1).
- **render.go**: RSS/Atom renderers take explicit `selfLink` and emit it as the
  channel-level `<link>` (Fix 2 / self-ref fix).
- **docker-compose.yml**: `FEEDFORGE_ALLOW_PRIVATE` is **not** in the env block (removed
  2026-09-16). Default is safe: private/loopback feed URLs rejected unless the operator
  sets the variable explicitly — matches plan.md.
- **Repo hygiene**: `backend/cmd/ffdbg/` and `backend/go_test_dbg.go` (both stray debug
  mains that broke `go build ./...`) are deleted; `cmd/` contains only `feedforge`.

### Environment notes (unchanged)
- `http://` source URLs blocked by default (SSRF guard) — needed for the loopback smoke
  test set `FEEDFORGE_ALLOW_PRIVATE=1` at process start.
- `go1.26.8`, `Node v22.23.2`. Test fixture at `/tmp/fftest/feed.xml` (serve:
  `cd /tmp/fftest && python3 -m http.server 9753`); test web stub at `/tmp/fftest/web`;
  test data dir `/tmp/fftest/data`.
- `web/dist` is **not committed** (the SPA is `web/` source; build it with npm before
  production runs, or ship it another way — the Dockerfile handles container builds).

## Test log — 2026-09-16 (all green)

1. `go build ./... && go vet ./...` → clean (after removing the two stray debug mains).
2. **Routing bug found & fixed**: old `r.Mount("/", a.Routes())` +
   `r.Handle("/*", StaticHandler)` registered the same `/*` pattern twice; the API router
   was shadowed → `GET /api/health` returned the static handler's 404. After switching to
   `a.Routes(r)` + `r.NotFound(spa.ServeHTTP)`:
   - `GET /api/health` → 200 `{"status":"ok"}`
   - `GET /` and `GET /some/client/route` → 200 `text/html` (SPA fallback)
   - `GET /api/nope` → 404 (static handler correctly refuses API/feed paths)
3. **Rendered feed self-reference**: `GET /feed/476af7d4604ff735` → 200, RSS contains
   `<link>http://127.0.0.1:8080/feed/476af7d4604ff735</link>` ✓
4. **Stale serving**: killed the upstream fixture, re-requested after TTL expiry →
   `HTTP/1.1 200` with `Warning: 110 feedforge "Response is Stale"`, body still the
   last good items ✓

## Notes

- `plan.md` is the **original design spec** — several details are now stale vs. code
  (warmup stub, pre-dial-only SSRF guard, `preview` with `url` field, per-request
  transform, `?format=` param). Treat code as source of truth.
- Feed created via API defaults to `enabled:false`; `PUT` with the full payload to enable
  (PUT replaces the whole record — it will zero fields you omit, e.g. `name`).
- `go.sum` has extra `github.com/jackc` entries (from older sqlc variant). Harmless;
  `go mod tidy` will drop them if desired.
