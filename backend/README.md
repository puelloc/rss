# FeedForge — backend (Go)

The FeedForge service: fetches upstream RSS/Atom feeds, applies per-feed
filter/transform rules, and republishes the result as a clean RSS 2.0 or Atom feed.
Pure Go, no CGO required (uses `modernc.org/sqlite`), so it cross-compiles cleanly.

## Stack

- **HTTP:** `chi` router (+ CORS).
- **Feed parse:** `mmcdole/gofeed` (upstream), `gorilla/feeds` (output render).
- **Storage:** SQLite via `modernc.org/sqlite` (WAL, single writer, pure Go).
- **Go:** 1.22.

## Layout

```
backend/
├── cmd/feedforge/main.go        # wiring: store, poller, api, router, scheduler
├── internal/
│   ├── model/model.go           # Feed + Rules types (JSON shapes used by the API/SPA)
│   ├── store/store.go           # SQLite CRUD (table: feeds)
│   ├── poller/poller.go         # HTTP fetch of upstream feeds, in-mem cache, SSRF dial guard
│   ├── transform/transform.go   # filter + transform + link-template + sort/trim engine
│   ├── render/render.go         # build RSS 2.0 / Atom XML from a transform result
│   └── api/api.go               # /api/* JSON endpoints, /feed/* output, SPA static handler
├── fixtures_server.py           # optional offline RSS/Atom fixture server for dev/tests
└── go.mod
```

## Build & run

```bash
cd backend
go build ./...                                  # type-check
go vet ./...
go run ./cmd/feedforge                          # dev server on :8080 (API + SPA if WEB_DIR set)

# production build (static binary)
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o feedforge ./cmd/feedforge
```

Environment (see `main.go`):

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | Listen address. |
| `DATA_DIR` | `./data` | Directory for `feedforge.db`. |
| `WEB_DIR` | `./web/dist` | Directory of the built SPA. Set to `""` for API-only. |
| `FEEDFORGE_ALLOW_PRIVATE` | `0` | `1`/`true` to permit upstream hosts on loopback/private IPs (default: blocked). |

Because defaults are relative (`./data`, `./web/dist`), when launching the raw binary
it's simplest to run it from the **repository root** with `WEB_DIR=web/dist`.

## Behaviour notes

- **Cache / TTL:** upstream fetch results are cached in memory per feed; a `GET`
  reuses the cache until `fetch_interval` (minutes, default 30) elapses. The
  background `scheduler` (a 1-minute ticker) re-polls each enabled feed when stale.
  `POST /api/feeds/{id}/refresh` forces an immediate poll.
- **SSRF guard:** the poller resolves and dials through a custom dialer that rejects
  loopback / private / unspecified / link-local destinations unless
  `FEEDFORGE_ALLOW_PRIVATE=1`. This is why the `127.0.0.1:8082` fixture needs that
  flag to be reachable.
- **Link origin:** absolute feed/item links are built from the request —
  `X-Forwarded-Proto`/`X-Forwarded-Host` when present, else the request `Host`.
  Send those from your reverse proxy for stable public links. There is no separate
  `BASE_URL` env var.
- **Staleness:** if an upstream refresh fails but a prior good copy exists, the
  previous result is served with a `Warning: 110 … "Response is Stale"` header.

## REST API

All bodies are JSON. `id` is the feed's UUID string.

### Discovery / meta
- `GET /api/health` → `200 {"status":"ok"}`

### Feed CRUD
- `GET    /api/feeds` → list all feeds
- `POST   /api/feeds` → create (returns `201` + feed)
- `GET    /api/feeds/{id}`
- `PUT    /api/feeds/{id}` → **replace** the feed (full body)
- `DELETE /api/feeds/{id}` → `204`
- `POST   /api/feeds/{id}/refresh` → force poll; returns `{"source_count":N,"matched":M}`
- `POST   /api/preview` → transform an unsaved feed (see below)

### Published feed output
- `GET /feed/{id}` → RSS 2.0 (`application/rss+xml`)
- `GET /feed/{id}.xml` → RSS 2.0 (alias)
- `GET /feed/{id}/atom` → Atom (`application/atom+xml`)

Disabling a feed (`enabled=false`) makes the `/feed/...` routes answer `503
"feed disabled"`.

### `Feed` object

```jsonc
{
  "id": "2c91…",                    // server-generated
  "name": "My feed",
  "source_url": "https://upstream/rss.xml",   // required
  "description": "optional",
  "enabled": true,
  "fetch_interval": 30,             // minutes (default 30)
  "rules": { /* Rules, below */ },
  "created_at": "2026-09-17T00:00:00Z",
  "updated_at": "2026-09-17T00:00:00Z"
}
```

### `Rules` object

```jsonc
{
  "include": { "mode": "any",           // "any" | "all"
    "rules": [ { "field": "title",              // title|description|content|link|author|categories
                  "op": "contains",             // contains|not_contains|equals|starts_with|ends_with|regex
                  "value": "episode",
                  "case_sensitive": false } ] },
  "exclude": { "mode": "all", "rules": [ ] },
  "transforms": [ { "target": "title",          // title|description|content|link
                    "op": "replace",            // replace|regex_replace|prefix|suffix|strip_html|trim|lower|upper
                    "find": "Old", "replace": "New" } ],
  "link_template": "https://site/show/{{ .slug }}" , // Go text/template over item fields
  "max_items": 50,
  "max_age_days": 0,                 // 0 = no age filter
  "sort_desc": true                  // newest first
}
```

Filter/transform semantics live in `internal/transform/transform.go`. Link templates
support helpers: `slug`, `lower`, `upper`, `trim`, `title`, `replace`, `episode`, and
the item fields `Title`, `Link`, `Description`, `Content`, `Author`, `GUID`,
`Categories`, `Episode`.

### `POST /api/preview`

Runs the full pipeline against an **unsaved** feed without persisting it.

```jsonc
// request
{
  "source_url": "https://upstream/rss.xml",   // required
  "name": "Preview",
  "rules": { /* Rules, may be empty */ },
  "format": "rss",                            // "rss" | "atom"
  "limit": 25                                 // 1..200 (items returned in `items`)
}
// response
{
  "items": [ { "title": "", "link": "", "description": "", "content": "",
               "author": "", "published": "", "guid": "", "categories": [] } ],
  "xml": "<rss…</rss>",
  "source_count": 40,
  "matched": 3,
  "error": ""                                  // present when the upstream fetch/parse failed
}
```

## Testing / fixtures

```bash
cd backend
python3 fixtures_server.py    # http://127.0.0.1:8082/feeds/{feed1.xml (RSS), feed2.xml (Atom)}
# then start the backend with:
FEEDFORGE_ALLOW_PRIVATE=1 go run ./cmd/feedforge
```

The two fixtures give you a deterministic, offline upstream to build & preview feeds
against. See `PROGRESS.md` for the end-to-end smoke-test log.

## Notes / limitations (intentional for v1)

- Single-process in-memory cache: restarting the server clears the upstream cache
  (it is re-warmed at boot). Feed *configurations* persist in SQLite.
- No auth on the API/SPA — assume a trusted network or put a reverse proxy in front.
- `POST /api/feeds` body is decoded with `DisallowUnknownFields` (unknown keys are a
  client bug); `PUT` uses the same strict decoding.
