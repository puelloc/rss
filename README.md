# FeedForge

**Self-hosted RSS transformer.** FeedForge reads existing RSS/Atom feeds, applies
per-feed filter + transform rules you configure in a live web UI, and republishes the
result as a clean, fresh RSS 2.0 **or** Atom feed at its own URL — perfect for
trimming, tidying, or re-linking feeds you don't control (a NAS, a friend's blog, an
anime site, etc.).

- **Backend:** Go (chi + gofeed + gorilla/feeds) with a pure-Go SQLite store — no CGO, easy to cross-compile.
- **Frontend:** Svelte 5 single-page app with a dual-pane editor + live preview.
- **Deploy:** single multi-stage Docker image, `docker compose up`.

```
            ┌────────────┐   rules    ┌─────────────────────┐
 upstream ─▶│  poller    │───────────▶│   transform + render │──▶  /feed/{id}       (RSS)
  RSS/Atom  │ (fetch/    │            │   (filter, reshape,  │──▶  /feed/{id}/atom (Atom)
            │  cache)    │            │   re-link, sort/trim)│
            └─────┬──────┘            └─────────────────────┘
                  │
                  ▼
            ┌────────────┐        ┌──────────────────────────────┐
            │  SQLite    │        │  Go  :8080  /api  +  /feed    │
            │  feedforge.db│       │  Svelte SPA  (editor/preview) │
            └────────────┘        └──────────────────────────────┘
```

---

## Quickstart

### Docker (recommended)

```bash
docker compose up -d --build
```

- App: **http://localhost:8080**
- Data persists in `./data/feedforge.db` (a named volume in `docker-compose.yml`).
- Health: `GET /api/health`.

FeedForge builds absolute item + feed links from the request host, so to expose them
at your LAN/NAS address put the instance behind a reverse proxy that forwards
`X-Forwarded-Host` and `X-Forwarded-Proto` (e.g. Caddy/Traefik/nginx). See
`backend/README.md` for the exact mechanism.

### Manual build (no Docker)

```bash
# 1. Build the frontend
cd web && npm install && npm run build          # → web/dist

# 2. Build the backend
cd ../backend
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o feedforge ./cmd/feedforge

# 3. Run from the repo root (so relative defaults resolve)
DATA_DIR=./data WEB_DIR=web/dist LISTEN_ADDR=:8080 ./backend/feedforge
# → http://localhost:8080
```

### Local development (hot reload)

```bash
# Terminal 1 — API + feed engine (:8080)
cd backend && go run ./cmd/feedforge

# Terminal 2 — Svelte dev server (:5173) with hot reload,
#             proxies /api and /feed to :8080
cd web && npm install && npm run dev
```

Open **http://localhost:5173**.

> Optional offline test source: `python3 backend/fixtures_server.py` serves two fake
> feeds (RSS 2.0 + Atom) on **http://127.0.0.1:8082** — use
> `http://127.0.0.1:8082/feeds/feed1.xml` as a `source_url` so you don't need the
> network. Note: because that IP is loopback, you must start the backend with
> `FEEDFORGE_ALLOW_PRIVATE=1` to allow it (see below).

---

## Using it

1. **`+ New feed`** in the left pane, or pick an existing feed.
2. **Source feed URL** — paste the upstream RSS/Atom URL (e.g.
   `https://feed.animetosho.org/rss2` or your fixture URL).
3. **Rules** (left pane) — pick which items to keep and how to reshape them:
   - **Include / Exclude** — match on `title`, `description`, `content`, `link`,
     `author`, or `categories`, combined `any`/`all`. An item survives if it matches
     Include (or no Include rules) **and** does not match Exclude.
   - **Transforms** — `replace`, `regex_replace`, `prefix`, `suffix`, `strip_html`,
     `trim`, `lower`, `upper` applied to a field.
   - **Link template** — rewrite each item's URL (useful for "watch on this site").
   - **Max items / Max age (days) / Newest first** — cap and ordering.
4. **Live preview** (right pane) — updates ~600 ms after each change in either
   RSS 2.0 or Atom; the raw XML is shown below the item card.
5. **Save**, then share the **Output URL**
   (`http://host:8080/feed/{id}` — RSS) or the **Atom** link
   (`/feed/{id}/atom`). Import that URL into any RSS reader.

Every saved feed is re-fetched automatically in the background on its **fetch
interval** (default 30 min). `Refresh now` forces an immediate upstream poll.

---

## Configuration (environment)

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP listen address. |
| `DATA_DIR` | `./data` | Where `feedforge.db` (SQLite) is created. |
| `WEB_DIR` | `./web/dist` | Directory of the built SPA to serve (`""` disables static hosting, API-only). |
| `FEEDFORGE_ALLOW_PRIVATE` | `0` | `1`/`true` allows upstream URLs resolving to loopback/private IPs (off by default — blocks SSRF-style requests to your internal devices). |

There is no `BASE_URL` variable: absolute links in published feeds are derived per
request from `X-Forwarded-Proto`/`X-Forwarded-Host` when present, otherwise the
request `Host`. Forward those two headers from your reverse proxy to get stable,
publicly-reachable links.

---

## Repository layout

```
.
├── Dockerfile            # multi-stage: web → backend → slim alpine runtime
├── docker-compose.yml    # container, port 8080, ./data volume, healthcheck
├── .dockerignore
├── .gitignore
├── plan.md               # design notes / spec
├── PROGRESS.md           # implementation + verification log
├── backend/              # Go service (see backend/README.md)
│   ├── cmd/feedforge/main.go
│   ├── internal/{model,store,transform,render,poller,api}/
│   ├── fixtures_server.py
│   └── go.mod
└── web/                  # Svelte 5 SPA (see web/README.md)
    ├── src/{App.svelte, main.js, lib/{api.js, RuleEditor.svelte, PreviewPane.svelte}}
    ├── vite.config.js    # dev server on 5173, proxies /api + /feed → :8080
    └── package.json
```

---

## Status

Implemented and verified end-to-end (builds green, browser smoke-test passed).
See [`PROGRESS.md`](PROGRESS.md) for the detailed log and what's implemented vs.
planned.

## License

No license file is included yet; add a `LICENSE` (e.g. MIT) when you're ready to ship.
The third-party libraries it builds on (gofeed, gorilla/feeds, chi, Svelte, Vite)
retain their own licenses.
