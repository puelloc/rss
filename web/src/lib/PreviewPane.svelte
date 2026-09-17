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

