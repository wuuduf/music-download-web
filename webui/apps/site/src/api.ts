export interface PlatformInfo {
  name: string;
  display_name: string;
  emoji?: string;
  capabilities: { search: boolean; download: boolean; lyrics: boolean };
}

export interface QualityOption { value: string; label: string }

export interface TrackResult {
  track_id: string;
  platform: string;
  title: string;
  artists: string[];
  album?: string;
  duration_ms: number;
  cover_url?: string;
  isrc?: string;
  qualities: QualityOption[];
}

export interface DownloadJob {
  job_id: string;
  status: string;
  progress: number;
  quality: string;
  title?: string;
  artists?: string[];
  error?: string;
}

export interface PlaybackSession {
  session_id: string;
  job_id: string;
  status: string;
  progress: number;
  platform: string;
  track_id: string;
  quality: string;
  title: string;
  artists: string[];
  album?: string;
  cover_url?: string;
  duration_ms?: number;
  audio_url: string;
  lyric_url: string;
  error?: string;
}

export interface LyricInfo {
  source: string;
  format: string;
  match_type: string;
  confidence: number;
  author?: string;
  word_synced: boolean;
  content: string;
}

export async function api<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || response.statusText);
  return data as T;
}

export function createPlayback(track: TrackResult, quality: string) {
  return api<PlaybackSession>("/api/v1/playback/sessions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      platform: track.platform,
      track_id: track.track_id,
      quality,
      title: track.title,
      artists: track.artists,
      album: track.album || "",
      cover_url: track.cover_url || "",
      duration_ms: track.duration_ms || 0,
      isrc: track.isrc || ""
    })
  });
}

export function coverURL(value?: string): string {
  if (!value) return "";
  return `/api/v1/media/image?url=${encodeURIComponent(value.replace(/^http:\/\//, "https://"))}`;
}
