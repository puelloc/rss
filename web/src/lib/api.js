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

