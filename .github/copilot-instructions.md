# AirWiggler – Copilot Instructions

## Project Summary
Self-hosted music library & player in a single Docker container, targeting Unraid users. Minimal setup, no frontend framework, pure Go backend.

## Stack
- **Backend**: Go — scans `/music`, serves REST API + static files + audio
- **Frontend**: Vanilla HTML/CSS/JS (no framework), single-page app
- **Distribution**: Single Docker container

## Music Folder Layout
```
/music/<Album>/
  cover.jpg|folder.jpg|cover.png   # optional art (checked in this order)
  high/    # FLAC or lossless
  medium/  # MP3 or lossy
```
- Each direct subfolder of `/music` is one album
- At least one of `high/` or `medium/` must exist
- Tracks sorted by filename; titles from embedded tags (Vorbis/ID3), fallback to filename

## API
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/library` | Full library JSON |
| POST | `/api/rescan` | Trigger rescan |

Audio and static files served directly from the container.

## Library JSON Shape
```json
{
  "albums": [{
    "id": "slugified-name",
    "title": "Album Name",
    "art": "/music/Album%20Name/cover.jpg",
    "qualities": {
      "high":   { "tracks": [{ "title": "...", "url": "...", "duration": 123 }] },
      "medium": { "tracks": [] }
    }
  }]
}
```

## Frontend Behavior
- Two views: **Library** (album grid with art + quality badges) and **Player** (track list + controls)
- Controls: play/pause, stop, prev/next, seek bar, volume, quality selector, autoplay toggle
- Quality switch preserves track index, playback position, and play/pause state
- No autoplay on page load — user initiates playback
- Single `<audio>` element controlled via JS

## Cover Art Resolution Order
1. `cover.jpg` 2. `folder.jpg` 3. `cover.png` (all in album root)
4. `high/cover.*` 5. `medium/cover.*`
6. Embedded art in first audio track via `dhowden/tag` `Picture()`

## Docker
| Volume | Purpose |
|--------|---------|
| `/music` | Read-only music library |

| Env Var | Default | Purpose |
|---------|---------|--------|
| `APP_PORT` | `8080` | HTTP port |
| `SITE_TITLE` | `My Music Library` | UI title |
| `DEFAULT_QUALITY` | `medium` | Initial quality |
| `RESCAN_ON_START` | `true` | Scan at boot |
| `RESCAN_INTERVAL_MINUTES` | `15` | Periodic rescan |
| `ACCESS_TOKEN` | `` (unset) | Shared token for internet-facing deployments |

## Access Control
- Optional shared-token middleware; no-op when `ACCESS_TOKEN` is unset
- On first visit user hits `/?token=<value>`; middleware sets an `httpOnly`, `SameSite=Lax` cookie and redirects to `/`
- All subsequent requests are validated against the cookie — no login screen, bookmarks work
- All routes protected when token is set (`/`, `/api/*`, `/music/*`)
- Requires HTTPS at reverse proxy (Nginx Proxy Manager) for internet exposure

## Go Metadata Libraries
- **Title/cover art**: `dhowden/tag` (Vorbis comments + ID3; `Picture()` for embedded art)
- **FLAC duration**: `mewkiz/flac` — `totalSamples / sampleRate` from `STREAMINFO`
- **MP3 duration**: `tcolgate/mp3` — frame scanning
- Both duration libs are pure Go (no CGo)

## Rescan Concurrency
- `sync.Mutex` guards scan; `POST /api/rescan` returns `{"status": "started"}` or `{"status": "already_running"}`

## Quality Switch Behavior
- Preserves track index; resets position to 0; resumes play if was playing
- Implementation: swap `src`, `load()`, then `play()` if was playing

## FLAC Browser Compatibility
- Detected via `audio.canPlayType('audio/flac')` on page load
- Shows non-blocking warning banner if unsupported (e.g., Safari)

## Key Constraints
- No frontend JS framework
- No external database — in-memory library model only
- Minimal dependencies; keep the image small
- Single HTTP port exposed (default 8080)
- No `/config` volume — not needed until there is a concrete use case
