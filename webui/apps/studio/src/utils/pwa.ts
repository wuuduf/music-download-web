// The upstream PWA pre-caches more than 6 MiB and can keep serving an old
// React/vendor split after a deployment. Studio favors correctness over
// offline support, so remove legacy registrations and Workbox caches.
export async function removeLegacyStudioCaches() {
	if (import.meta.env.TAURI_ENV_PLATFORM || typeof window === "undefined")
		return;
	if ("serviceWorker" in navigator) {
		const registrations = await navigator.serviceWorker.getRegistrations();
		await Promise.all(
			registrations
				.filter((registration) => registration.scope.includes("/studio/"))
				.map((registration) => registration.unregister()),
		);
	}
	if ("caches" in window) {
		const keys = await caches.keys();
		await Promise.all(
			keys
				.filter((key) => /workbox|precache|studio/i.test(key))
				.map((key) => caches.delete(key)),
		);
	}
}
