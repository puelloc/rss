# FeedForge — Progress

## Status (2026-09-17 — re-verified; full log at the bottom)

All work complete. `go build ./...` + `go vet ./...` clean. `npm run build` green.
Browser smoke test (real Chromium via Playwright) passed end-to-end. Repo is now git
(`main` pushed to github.com/puelloc/rss) — see 2026-09-17 test log.
Code in `backend/`, `web/`, `Dockerfile`, `docker-compose.yml`.

### Fixed on 2026-09-17 (details in test log)

- **`web/src/lib/RuleEditor.svelte:119`** — `{{ ... }}` literal inside a quoted
  attribute string broke the Svelte parser (build failure). Now a JS string expression.
- **`backend/internal/api/api.go` (StaticHandler)** — the API/feed 404 guard used
  `HasPrefix(path, "/feed")`, which shadowed SPA client routes like `/feeds` and 404'd
  them. Now `/feed/` (trailing slash); `/feeds`, `/search` serve the SPA.
- **`backend/internal/api/api.go` (preview)** — `/api/preview` takes the SPA's actual
  payload (`source_url` + rules); fetches upstream, applies rules, returns items/xml.
- **Repo hygiene** — `go mod tidy` (no stray `jackc` entries left in go.mod/go.sum);
  `.dockerignore` added (keeps `web/node_modules`, `backend/feedforge` binary out of
  the build context); `.gitignore` added; `backend/fixtures_server.py` committed as the
  canonical smoke-test fixture (replaces the old `/tmp/fftest` one-off).

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
- **api.go**: preview body is `{ source_url, rules, format, limit }` — the backend
  fetches the upstream URL itself (same SSRF-guarded client as the poller), applies the
  rules, and returns `{ items, xml, matched, source_count, error? }`; this is exactly
  what the SPA `api.preview()` sends (verified in-browser 2026-09-17). `serve()` renders
  via explicit route, sets `Warning: 110 feedforge "Response is Stale"` when the poller
  serves a prior good copy.
  `updateFeed()` re-reads the persisted feed after `Update` before returning it (Fix 6).
  `StaticHandler(dir)` = `http.FileServer` + SPA fallback to `index.html`, with 404 for
  `/api/*` and `/feed/*` paths (matters because it's also the NotFound handler).
  **JSON is strict**: unknown fields are rejected (e.g. `format` on `POST /api/feeds`
  → 400 `json: unknown field`) — intentional.
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
- `web/dist` is **not committed** (`.gitignore`); SPA lives in `web/` source — the
  Dockerfile builds it in-image; for dev builds run `npm run build` under `web/` and
  point `WEB_DIR` at it.

## Test log — 2026-09-17 (all green)

Environment: same box; Chrome via Playwright (`agent-browser` CLI); no Docker daemon
access on this box (see item 6).

1. `cd backend && go mod tidy` → go.mod/go.sum clean (no stray `jackc` entries);
   `go build ./...` + `go vet ./...` → clean.
2. `cd web && npm install && npm run build` → **failed**: `RuleEditor.svelte:119` had
   `{{ ... }}` inside a quoted attribute string (parsed as Svelte expression, build
   broken). Fixed as a JS string expression → build green (a11y warnings only).
3. **SPA-404 regression found & fixed**: with the rebuilt dist served by the backend,
   `GET /feeds` → 404 (StaticHandler guard matched prefix `"/feed"` without the slash,
   swallowing client routes). Fixed to `"/feed/"` → `/feeds`, `/search` → 200 SPA;
   `GET /api/nope` still 404; `GET /feed/{id}` still API.
4. Browser smoke test (Chromium vs `http://127.0.0.1:8080`, real dist, fixture
   server `backend/fixtures_server.py` on :8082):
   - App boots, clean console; editor renders.
   - Fill source URL → `Re-run preview` → **4/4 fixture items** rendered.
   - `Save` → toast "Saved"; Output URL populated + Copy/Open links appear.
5. API round-trip with a fresh feed:
   - `POST /api/feeds` strict JSON: `format` field rejected (400) — intentional;
     minimal payload → id.
   - `PUT` to enable (feeds default `enabled:false`) + `POST /{id}/refresh` → 200.
   - `GET /feed/{id}` → 200 RSS, 3 items, correct self `<link>`;
     `GET /feed/{id}/atom` → 200, 3 entries.
   - `GET /api/health` → 200; `/api/preview` with SPA payload → items returned.
6. **Docker**: `docker compose config` → parses cleanly (env = `DATA_DIR`,
   `LISTEN_ADDR`, `WEB_DIR`; **no** `FEEDFORGE_ALLOW_PRIVATE` — as intended).
   `docker build` **not runnable** on this box (socket: permission denied; no
   sudo/podman). Static review of Dockerfile: stages sound; healthcheck `wget`
   is BusyBox built-in on alpine (fine); added `.dockerignore` so `web/node_modules`
   and the built Go binary no longer enter the build context.
7. **Git**: repo initialized (`main`), initial commit pushed to
   `github.com/puelloc/rss`; this update as a follow-up branch + PR. Nothing lost.
8. **Docs**: added root `README.md` (how to use: compose / manual / dev modes, UI
   walkthrough, env vars, repo layout) plus `web/README.md` and `backend/README.md`
   (architecture, API surface, configuration). Every claim verified against the
   code — real CSS tokens, rule fields/operators, env var names, ports, pure-Go
   `modernc.org/sqlite` driver, per-feed poller locking.

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
- `go.sum`/`go.mod` tidied 2026-09-17 (stray `jackc` entries are gone).
- **Docker build unverified in-container** (no daemon access on the dev box as of
  2026-09-17) — everything in the image path is verified natively: same Go build,
  same npm build, same binary+dist, healthcheck URL returns 200. Where a daemon
  exists: `docker build -t feedforge .` closes the loop.
- Working copy is under git now (github.com/puelloc/rss): branch for changes, push,
  open PRs against `main`.
