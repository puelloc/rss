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
      placeholder={'https://my.site/anime/{{ slug .Title }}'}
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

