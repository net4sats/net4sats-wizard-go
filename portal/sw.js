const CACHE_NAME = 'net4sats-v1';
const SHELL_ASSETS = [
  '/net4sats/',
  '/net4sats/index.html',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then((cache) => cache.addAll(SHELL_ASSETS))
  );
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((names) =>
      Promise.all(
        names
          .filter((n) => n !== CACHE_NAME)
          .map((n) => caches.delete(n))
      )
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // Network-first for /ubus calls (always fresh API data)
  if (url.pathname.includes('/ubus')) {
    event.respondWith(
      fetch(event.request)
        .catch(() =>
          caches.match(event.request).then((r) => r || new Response('Network error', { status: 503 }))
        )
    );
    return;
  }

  // Cache-first for app shell and static assets
  event.respondWith(
    caches.match(event.request).then((cached) => {
      if (cached) return cached;
      return fetch(event.request).then((response) => {
        // Cache successful GETs for JS/CSS/HTML
        if (response.ok && event.request.method === 'GET') {
          const ct = response.headers.get('content-type') || '';
          if (ct.includes('javascript') || ct.includes('css') || ct.includes('html')) {
            const clone = response.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
          }
        }
        return response;
      });
    })
  );
});
