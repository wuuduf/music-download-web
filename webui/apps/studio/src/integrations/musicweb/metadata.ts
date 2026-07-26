import type { TTMLLyric, TTMLMetadata } from "$/types/ttml";

export interface MusicWebMetadataMatch {
	track_id?: string;
	score: number;
	match_type?: string;
	reasons?: string[];
	requires_confirmation: boolean;
	error?: string;
}

export interface MusicWebProjectMetadata {
	music_name: string;
	music_names?: string[];
	artists: string[];
	album?: string;
	albums?: string[];
	isrc?: string;
	isrcs?: string[];
	external_ids?: Record<string, string[]>;
	matches?: Record<string, MusicWebMetadataMatch>;
	unresolved_platforms?: string[];
}

export const platformMetadataKeys: Record<string, string> = {
	netease: "ncmMusicId",
	qqmusic: "qqMusicId",
	spotify: "spotifyId",
	applemusic: "appleMusicId",
};

export const platformDisplayNames: Record<string, string> = {
	netease: "网易云音乐",
	qqmusic: "QQ音乐",
	spotify: "Spotify",
	applemusic: "Apple Music",
};

export function musicWebProjectID() {
	const match = location.pathname.match(/^\/studio\/([^/]+)/);
	return match ? decodeURIComponent(match[1]) : "";
}

export function mergeMusicWebMetadata(
	lyrics: TTMLLyric,
	metadata?: MusicWebProjectMetadata,
): TTMLLyric {
	if (!metadata) return lyrics;
	const next = lyrics.metadata.map((entry) => ({
		...entry,
		value: [...entry.value],
	}));
	const values = new Map<string, string[]>();
	values.set("musicName", [
		metadata.music_name,
		...(metadata.music_names ?? []),
	]);
	values.set("artists", metadata.artists ?? []);
	values.set("album", [metadata.album ?? "", ...(metadata.albums ?? [])]);
	values.set("isrc", [metadata.isrc ?? "", ...(metadata.isrcs ?? [])]);
	for (const [platform, ids] of Object.entries(metadata.external_ids ?? {})) {
		const key = platformMetadataKeys[platform];
		if (key) values.set(key, ids);
	}
	for (const [key, incoming] of values)
		mergeMetadataValues(next, key, incoming);
	return { ...lyrics, metadata: next };
}

function mergeMetadataValues(
	metadata: TTMLMetadata[],
	key: string,
	values: string[],
) {
	const incoming = values.map((value) => value.trim()).filter(Boolean);
	if (incoming.length === 0) return;
	let entry = metadata.find((item) => item.key === key);
	if (!entry) {
		entry = { key, value: [] };
		metadata.push(entry);
	}
	for (const value of incoming) {
		if (!entry.value.includes(value)) entry.value.push(value);
	}
}

/**
 * Replaces the TTML metadata entry for one platform with exactly the given ID
 * (or clears it). Used by manual confirmation, where the corrected ID must not
 * coexist with an earlier wrong guess.
 */
export function setPlatformMetadataID(
	lyrics: TTMLLyric,
	platform: string,
	id: string,
): TTMLLyric {
	const key = platformMetadataKeys[platform];
	if (!key) return lyrics;
	const next = lyrics.metadata.map((entry) => ({
		...entry,
		value: [...entry.value],
	}));
	let entry = next.find((item) => item.key === key);
	if (!entry) {
		if (!id) return lyrics;
		entry = { key, value: [] };
		next.push(entry);
	}
	entry.value = id ? [id] : [];
	return { ...lyrics, metadata: next };
}

export function metadataResolutionSummary(metadata?: MusicWebProjectMetadata) {
	const matched = Object.keys(metadata?.external_ids ?? {}).filter(
		(platform) => (metadata?.external_ids?.[platform]?.length ?? 0) > 0,
	).length;
	const isrcs = new Set(
		[metadata?.isrc ?? "", ...(metadata?.isrcs ?? [])].filter(Boolean),
	).size;
	return {
		matched,
		total: 4,
		isrcs,
		unresolved: metadata?.unresolved_platforms ?? [],
	};
}
