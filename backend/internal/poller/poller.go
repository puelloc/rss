package poller

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"

	"feedforge/internal/model"
	"feedforge/internal/store"
	"feedforge/internal/transform"
)

type entry struct {
	result    *transform.Result
	fetchedAt time.Time
	err       error
}

var (
	fetchClient = &http.Client{
		Transport: &http.Transport{
			DialContext:           guardedDialContext,
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
		},
	}
)

// guardedDialContext resolves the host and checks the destination against
// private/loopback/link-local ranges before any socket is opened. Because it
// runs inside the transport it fires on every dial, including redirects, so
// a malicious feed can't bounce through a public redirect to an internal IP.
func guardedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if !allowPrivate() {
		var ips []net.IP
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else {
			addrs, lerr := net.DefaultResolver.LookupIPAddr(ctx, host)
			if lerr == nil {
				for _, a := range addrs {
					ips = append(ips, a.IP)
				}
			}
		}
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				return nil, fmt.Errorf("blocked: unsafe destination %s", ip)
			}
		}
	}
	return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, addr)
}

func allowPrivate() bool {
	v := os.Getenv("FEEDFORGE_ALLOW_PRIVATE")
	return v == "1" || v == "true"
}

type Poller struct {
	store *store.Store
	fp    *gofeed.Parser

	mu    sync.RWMutex
	cache map[string]*entry

	locks sync.Map // feedID -> *sync.Mutex
}

func New(s *store.Store) *Poller {
	fp := gofeed.NewParser()
	fp.UserAgent = "FeedForge/1.0 (+https://github.com/yourname/feedforge)"

	return &Poller{
		store: s,
		fp:    fp,
		cache: map[string]*entry{},
	}
}

func (p *Poller) lockFor(id string) *sync.Mutex {
	v, _ := p.locks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Get returns a transformed result, using cache unless force is set.
func (p *Poller) Get(ctx context.Context, f *model.Feed, feedLink string, force bool) (*transform.Result, time.Time, error) {
	ttl := time.Duration(f.FetchInterval) * time.Minute
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	p.mu.RLock()
	e, ok := p.cache[f.ID]
	p.mu.RUnlock()
	if ok && !force && e.err == nil && time.Since(e.fetchedAt) < ttl {
		return e.result, e.fetchedAt, nil
	}

	lk := p.lockFor(f.ID)
	lk.Lock()
	defer lk.Unlock()

	// Double-check after acquiring the lock.
	p.mu.RLock()
	e, ok = p.cache[f.ID]
	p.mu.RUnlock()
	if ok && !force && e.err == nil && time.Since(e.fetchedAt) < ttl {
		return e.result, e.fetchedAt, nil
	}

	res, err := p.fetch(ctx, f, feedLink)
	now := time.Now().UTC()

	p.mu.Lock()
	if err != nil {
		// keep the previous good result around if we have one
		if ok && e.result != nil {
			e.err = err
			// remember the failure so the next call retries, but still hand the
			// caller the stale result so endpoints can keep serving
			p.mu.Unlock()
			return e.result, e.fetchedAt, err
		}
		p.cache[f.ID] = &entry{err: err, fetchedAt: now}
	} else {
		p.cache[f.ID] = &entry{result: res, fetchedAt: now}
	}
	p.mu.Unlock()

	if err != nil {
		return nil, now, err
	}
	return res, now, nil
}

func (p *Poller) Invalidate(id string) {
	p.mu.Lock()
	delete(p.cache, id)
	p.mu.Unlock()
}

func (p *Poller) fetch(ctx context.Context, f *model.Feed, feedLink string) (*transform.Result, error) {
	if err := guardURL(f.SourceURL); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.SourceURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.fp.UserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, */*")

	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}

	src, err := p.fp.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}

	return transform.Apply(f, src, feedLink, time.Now().UTC()), nil
}

// guardURL checks the initial URL's scheme before the HTTP client dials. The
// per-dial enforcement lives in guardedDialContext, which also covers any
// redirects this response chain produces.
func guardURL(raw string) error {
	if allowPrivate() {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	return nil
}

