# FeedForge — web (Svelte 5 SPA)

Single-page editor for FeedForge feeds. Pick or create a feed, edit its source and
rules, watch a **live RSS/Atom preview** update as you type, then share the published
URL. Talks to the Go backend over same-origin `/api/*` (no CORS headaches).

Built with Svelte 5 (runes: `$state`/`$derived`), Vite 5, and no framework
dependencies — just the three components below.

## Commands (run here, in `web/`)

```bash
npm install
npm run dev        # Vite dev server on http://localhost:5173  (hot reload)
npm run build      # production bundle → web/dist/
npm run preview    # serve the production build locally
```

The dev server **proxies** `/api` and `/feed` to `http://localhost:8080`
(see `vite.config.js`), so you just need the backend running:

```bash
# Terminal 1
cd ../backend && go run ./cmd/feedforge
# Terminal 2  (here)
cd web && npm run dev
```

For a cross-origin setup (not the proxy), set `VITE_API_BASE`
(e.g. `VITE_API_BASE=https://feed.example.com` at build time).

## Layout

```
web/
├── index.html                     # SPA shell (mounts #app)
├── main.js                        # createRoot(App) + app.css
├── vite.config.js                 # Svelte plugin; dev port 5173; /api + /feed proxy
├── svelte.config.js
├── package.json
└── src/
    ├── App.svelte                 # root: sidebar, top bar, dual-pane editor, toast
    ├── app.css                    # design tokens (CSS variables) + base styles
    └── lib/
        ├── api.js                 # fetch client for /api/*
        ├── RuleEditor.svelte      # include/exclude + transform + link-template form
        └── PreviewPane.svelte     # item card + renderable XML preview
```

## What the UI does

- **Sidebar** — list of saved feeds (`enabled`/`paused` state) and `+ New feed`.
- **Top bar** — feed name, `Enabled` toggle, `fetch interval` (5→360 min),
  `Refresh now`, and `Save`.
- **Output URL bar** — shows `http://host:8080/feed/{id}` (RSS) with **Copy**,
  **Open ↗**, and an **Atom ↗** link (`/feed/{id}/atom`). Appears once saved.
- **Left pane** — source URL, description, and the rule editor:
  - **Include / Exclude** groups with `any`/`all` mode, per-field rules
    (`title, description, content, link, author, categories`), operators
    (`contains, not_contains, equals, starts_with, ends_with, regex`).
  - **Transforms** (`replace, regex_replace, prefix, suffix, strip_html, trim, lower, upper`).
  - **Link template**, **max items**, **max age**, **newest first**.
- **Right pane** — live preview (RSS 2.0 / Atom toggle), the matched item cards, and
  the raw XML. The preview runs ~600 ms after each input change (debounced) via
  `POST /api/preview`.

## Data flow

```
App.svelte ⇄ lib/api.js ⇄ backend /api/*
   │
   ├─ on mount:          GET  /api/feeds            → sidebar list
   ├─ new/edit/save:     POST/PUT /api/feeds[{id}]
   ├─ delete:            DELETE /api/feeds/{id}
   ├─ refresh now:       POST /api/feeds/{id}/refresh
   └─ live preview:      POST /api/preview          → item cards + XML
```

The client never caches feed state beyond the in-memory `draft`; the backend is the
source of truth and the single writer.

## Styling

CSS variables in `src/app.css` (e.g. `--panel`, `--border`, `--accent`). Layout is
CSS grid; responsive breakpoint collapses the sidebar + dual panes on narrow screens.

## Notes

- This is a single-view app: there is no client-side router, so the backend serves
  `index.html` as the SPA fallback for non-API/feed paths.
- `web/dist/` is the build output the Go server serves via `WEB_DIR`; it's
  git-ignored (produce it with `npm run build`, or let the Docker build make it).
