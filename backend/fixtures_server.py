#!/usr/bin/env python3
"""Tiny multi-feed fixture server for FeedForge local dev / smoke tests.

Usage (from backend/):
    python3 fixtures_server.py            # http://127.0.0.1:8082
Ports:
    /feeds/feed1.xml  RSS 2.0, 4 items
    /feeds/feed2.xml  Atom, 3 items
"""
import http.server
import socketserver

PORT = 8082

FEED1 = b"""<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Feed One</title>
    <link>https://example.com/one</link>
    <description>RSS fixture</description>
    <item><title>Alpha</title><link>https://example.com/one/alpha</link><guid>https://example.com/one/alpha</guid><pubDate>Wed, 16 Sep 2026 10:00:00 +0000</pubDate></item>
    <item><title>Bravo</title><link>https://example.com/one/bravo</link><guid>https://example.com/one/bravo</guid><pubDate>Wed, 16 Sep 2026 09:00:00 +0000</pubDate></item>
    <item><title>Charlie</title><link>https://example.com/one/charlie</link><guid>https://example.com/one/charlie</guid><pubDate>Wed, 16 Sep 2026 08:00:00 +0000</pubDate></item>
    <item><title>Delta</title><link>https://example.com/one/delta</link><guid>https://example.com/one/delta</guid><pubDate>Wed, 16 Sep 2026 07:00:00 +0000</pubDate></item>
  </channel>
</rss>
"""

FEED2 = b"""<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Feed Two</title>
  <link href="https://example.com/two"/>
  <entry><title>Echo</title><link href="https://example.com/two/echo"/><id>urn:uuid:a1</id><updated>2026-09-16T10:00:00Z</updated></entry>
  <entry><title>FOX</title><link href="https://example.com/two/fox"/><id>urn:uuid:a2</id><updated>2026-09-16T09:00:00Z</updated></entry>
  <entry><title>Golf</title><link href="https://example.com/two/golf"/><id>urn:uuid:a3</id><updated>2026-09-16T08:00:00Z</updated></entry>
</feed>
"""


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = {"/feeds/feed1.xml": FEED1, "/feeds/feed2.xml": FEED2}.get(self.path)
        if body is None:
            self.send_error(404)
            return
        self.send_response(200)
        ctype = "application/atom+xml" if self.path.endswith("feed2.xml") else "application/rss+xml"
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


if __name__ == "__main__":
    with Server(("127.0.0.1", PORT), Handler) as httpd:
        print(f"fixture server on http://127.0.0.1:{PORT}")
        httpd.serve_forever()
