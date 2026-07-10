# MusicWeb × AMLL implementation

This branch implements the AMLL integration as one deployable Go service with
two independently built React applications.

## Runtime layout

```text
Browser
  ├─ /                         React search/download site
  ├─ /player/:session          AMLL online player
  └─ /studio/:project          pinned AMLL TTML Tool fork (admin only)
           │
           ▼
Go /api/v1
  ├─ musicservice              search, parse, cached downloads
  ├─ webapp/playback           session + same-origin Range audio
  ├─ webapp/lyrics             AMLL DB resolver + platform fallback
  └─ webapp/studio             bootstrap, metadata, revisions, export
           │
           ├─ cache/web        shared audio cache
           ├─ cache/amll-db    JSONL indexes and fetched lyric files
           └─ data.db          studio_projects / studio_revisions
```

## Stage completion map

1. **Frozen baseline** — integration branch, exact npm versions, upstream fork
   commits and `/api/v1` contract are recorded.
2. **React foundation** — `webui/apps/site` is Vite + React + TypeScript + pnpm;
   legacy `/api` remains compatible while the new site uses `/api/v1`.
3. **Online playback** — a playback session reuses `musicservice` cache and
   exposes audio through `http.ServeContent`, including byte ranges and seeking.
4. **AMLL player** — pinned AMLL React player, RAF audio sync, lyric seeking,
   translation/romanization switches, blurred cover and low-power mode.
5. **AMLL DB first** — on-demand JSONL index sync, exact platform ID and ISRC
   matching, local lyric cache, platform fallback and source/confidence fields.
6. **Unified lyric export** — `musicservice.CreateLyrics` delegates to the
   resolver; TTML/LRC/YRC/QRC/LYS/ELRC/ASS/SRT/AMJSON/TXT share one flow.
7. **Studio bootstrap** — pinned TTML Tool fork builds at `/studio/`; its bridge
   imports audio, seed TTML and metadata without a local file picker.
8. **Projects and autosave** — upstream IndexedDB autosave remains enabled and a
   debounced server snapshot uses optimistic revisions, conflict detection,
   history, restore and TTML export.
9. **Metadata match** — exact AMLL DB IDs and ISRC are preferred. Missing IDs
   are searched concurrently on NetEase, QQ Music, Spotify and Apple Music;
   exact title, artist, album and duration contribute to a score. A unique
   score of at least 85 (or an exact ISRC) is auto-applied, while ambiguous
   candidates remain `requires_confirmation=true`. Multiple regional titles,
   albums and ISRCs are retained because AMLL metadata permits repeated values.
10. **Release/operations** — GitHub Actions, hashed static assets, Nginx
    configuration, systemd units, SQLite backup timer, AMLL DB admin status,
    source/cache/failure counters and per-IP playback/Studio rate limits.

## Build

```bash
cd webui
corepack pnpm install --frozen-lockfile
corepack pnpm build:all
cd ..
go test ./web/server ./webapp/... ./bot/db ./bot/musicservice
GOMAXPROCS=1 GOMEMLIMIT=700MiB go build -trimpath -ldflags="-s -w" -o musicweb ./cmd/musicweb
```

The Vite outputs are `webui/dist/site` and `webui/dist/studio`. They are build
artifacts and are intentionally not committed.

## Production rollout

1. Build the two frontends before the Go binary.
2. Copy `deploy/musicweb.service` and the backup units to `/etc/systemd/system`.
3. Replace the example domain in `deploy/nginx.musicweb.conf`, then enable it.
4. Run `systemctl daemon-reload && systemctl enable --now musicweb`.
5. Enable backups with
   `systemctl enable --now musicweb-backup.timer`.
6. Sign in at `/admin`; use the AMLL DB panel to verify/sync the indexes.

For the 1 GiB server, keep `GOMAXPROCS=1`, `GOMEMLIMIT=700MiB`, build assets in
CI when possible, and do not clone the full AMLL lyrics repository. Only the
four JSONL indexes and requested lyric files are cached.

## Verification checklist

- Search and link parsing use `/api/v1` and each quality is an independent job.
- NetEase covers load through the allow-listed same-origin image proxy.
- Audio responses return `Accept-Ranges: bytes`; seeking works after readiness.
- Player response shows `source=amlldb` when an exact AMLL DB entry exists.
- Lyric download reports source, author, sync granularity and confidence.
- `/studio/:project` redirects unauthenticated users and has COOP/COEP headers.
- Saving the same expected revision twice returns HTTP 409 on the second write.
- Admin AMLL status exposes index size, cache hits/misses, sources and failures.
