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

