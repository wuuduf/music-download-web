// Self-removing replacement for the old Workbox service worker.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => {
	event.waitUntil(
		Promise.all([
			self.registration.unregister(),
			caches.keys().then((keys) => Promise.all(keys.map((key) => caches.delete(key)))),
			self.clients.claim(),
		]),
	);
});
