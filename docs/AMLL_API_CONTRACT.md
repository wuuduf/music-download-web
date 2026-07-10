# MusicWeb AMLL API Contract (v1)

This contract freezes the boundary between the Go backend, the public React
site, and the AMLL TTML Studio fork. Existing unversioned endpoints remain as
compatibility aliases while all new clients use `/api/v1`.

## Track identity

```json
{
  "platform": "netease",
  "track_id": "123",
  "title": "Song",
  "artists": ["Artist"],
  "album": "Album",
  "duration_ms": 210000,
  "isrc": "USXXX0000001",
  "external_ids": {"netease": ["123"]}
}
```

## Playback

- `POST /api/v1/playback/sessions`
- `GET /api/v1/playback/sessions/{session_id}`
- `GET /api/v1/playback/sessions/{session_id}/audio`
- `GET /api/v1/playback/sessions/{session_id}/lyrics?format=ttml`

Audio is same-origin, supports HTTP Range requests, and is served inline. A
session initially mirrors the underlying download job state.

## Lyrics

- `GET /api/v1/lyrics/{platform}/{track_id}?format=ttml`
- `GET /api/v1/lyrics/file?platform={platform}&track_id={track_id}&format=lrc`
- `POST /api/v1/admin/amlldb/sync` (admin)
- `GET /api/v1/admin/amlldb/status` (admin)

Every lyric response reports `source`, `match_type`, `confidence`, `author`,
`word_synced`, and platform IDs. Resolution order is AMLL TTML DB, then the
native platform provider.

## Studio

- `POST /api/v1/studio/projects` (admin)
- `GET /api/v1/studio/projects/{project_id}/bootstrap` (admin)
- `GET /api/v1/studio/projects/{project_id}/export` (admin, current TTML)
- `POST /api/v1/studio/projects/{project_id}/revisions` (admin)
- `GET /api/v1/studio/projects/{project_id}/revisions` (admin)
- `POST /api/v1/studio/projects/{project_id}/restore` (admin)
- `GET /api/v1/studio/metadata/search` (admin)

Revision writes include `expected_revision`. A mismatch returns HTTP 409 and
never overwrites a newer revision.

## Compatibility policy

The fields above are additive within v1. Fields are never silently renamed or
removed. Breaking changes require `/api/v2`.
