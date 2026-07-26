/**
 * Shared fetch helpers for the MusicWeb studio integration panels.
 *
 * These wrap `fetch` with the admin-authenticated behavior the studio panels
 * rely on: a 401 redirects to the admin login, and a non-2xx response throws
 * the server's error payload. This is intentionally distinct from the public
 * player's `apps/site/src/api.ts` helper (which has no 401 redirect).
 */

/**
 * Fetch with the shared 401/error guard, returning the raw Response.
 * On 401 it redirects to the admin login; on any other non-ok status it throws
 * the server's `error` payload (or an `HTTP <status>` fallback).
 */
export async function checkedFetch(
	url: string,
	init?: RequestInit,
): Promise<Response> {
	const response = await fetch(url, init);
	if (response.status === 401) {
		location.href = `/admin/login?next=${encodeURIComponent(location.pathname)}`;
		throw new Error("需要管理员登录");
	}
	if (!response.ok) {
		const payload = await response.json().catch(() => ({}));
		throw new Error(payload.error || `HTTP ${response.status}`);
	}
	return response;
}

/** Fetch through the shared guard and parse the response body as JSON. */
export async function api<T>(url: string, init?: RequestInit): Promise<T> {
	const response = await checkedFetch(url, init);
	return response.json() as Promise<T>;
}

/** Resolve after the given number of milliseconds. */
export const wait = (milliseconds: number) =>
	new Promise((resolve) => window.setTimeout(resolve, milliseconds));

/**
 * Poll an audio URL until it is ready, retrying while the server responds with
 * 409 (still preparing) and failing on any other non-ok status.
 */
export async function fetchAudio(url: string): Promise<Response> {
	for (let attempt = 0; attempt < 180; attempt += 1) {
		const response = await fetch(url);
		if (response.ok) return response;
		if (response.status !== 409)
			throw new Error(`音频加载失败：HTTP ${response.status}`);
		await new Promise((resolve) => setTimeout(resolve, 1000));
	}
	throw new Error("音频准备超时");
}
