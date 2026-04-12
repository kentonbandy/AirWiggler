# AirWiggler

Do you love it when air wiggles your ear bones? Are you sick of free hosting solutions that bombard your friends and family with ads? Have you spent way too much time and money on a home server? I may not be able to provide the therapy you need, but I can provide this app!

AirWiggler is a self-hosted music library and player in a single Docker container. Point it at a folder of music, open a browser, and play. Supports hosting multiple levels of quality so users can select the best option for their connection. Minimal configuration required.

Designed for home servers running [Unraid](https://unraid.net/), but works with any container runtime that can pull from Docker Hub.

Configurable to have no security, an access token (only those with the link can access), or to respect OIDC auth headers.

---

## Music Folder Layout

Every direct subfolder of `/music` is treated as one album or collection. Beyond that, the structure is flexible - the folder structure is the configuration:

```
/music/
  Album Name/
    cover.jpg          ← optional cover art
    track01.flac       ← music files at this level are fine if you only have one quality
    track02.flac
  Another Album/
    cover.jpg
    flac/              ← subfolder name becomes the quality button label in the UI
      track01.flac
    mp3/
      track01.mp3
  Mixed Album/
    cover.jpg
    track01.mp3        ← top-level files appear under a "root" button
    high/
      track01.flac     ← subfolder files appear under their own button
```

**Quality subfolders** — if you want to offer multiple versions of the same album, put each version in its own subfolder. The folder name becomes the label on the quality-selector button in the UI. Any name works: `high`/`medium`/`low`, `flac`/`mp3`/`wav`, `lossless`/`lossy`, etc.

**Single quality** — if you only have one version, you can skip subfolders entirely and place music files directly in the album folder.

**Mixed** — if music files exist at the album root alongside subfolders, the root-level files are grouped under a `root` button.

**Supported audio formats** — .flac, .mp3, .wav, .m4a, .aac, .ogg, .opus, .aif, .aiff

**Cover art** — `cover.jpg` (or `folder.jpg` / `cover.png`) in the album folder is used as the album art. Additional images in the folder are also viewable. Embedded art in the first audio track is used as a fallback.

Tracks are sorted by filename; titles come from embedded tags (Vorbis/ID3) and fall back to the filename.

---

## Setup on Unraid

### 1. Add the container

1. Go to the **Docker** tab and click **Add Container**.
2. Set the **Repository** field to:
   ```
   kentonbandy/airwiggler:latest
   ```
3. Give the container a name, e.g. `AirWiggler`.

### 2. Map the music path

Add a **Path** mapping:

| Field | Value |
|-------|-------|
| Container Path | `/music` |
| Host Path | Path to your music share, e.g. `/mnt/user/music` |
| Access Mode | Read Only |

### 3. Map the port

Add a **Port** mapping:

| Field | Value |
|-------|-------|
| Container Port | `8080` |
| Host Port | `8080` (or any available port) |

### 4. Set environment variables

Add these under **Variables**:

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_PORT` | `8080` | Port the server listens on inside the container. Must match what you mapped above. |
| `SITE_TITLE` | `My Music Library` | Title shown in the browser tab and UI. |
| `DEFAULT_QUALITY` | `medium` | Which quality tier loads by default (will attempt to match a quality folder name, otherwise defaults to the first found quality). |
| `RESCAN_ON_START` | `true` | Scan for new/changed albums when the container starts. |
| `RESCAN_INTERVAL_MINUTES` | `15` | How often to re-scan the music folder automatically. |
| `COOKIE_SECURE` | `true` | See note below. |
| `ACCESS_TOKEN` | *(unset)* | Optional, but highly recommended to control access to your library. See [Access Control](#access-control). |
| `AUTH_PROXY_HEADER` | *(unset)* | Header-based auth for use behind an OIDC/SSO reverse proxy. See [Proxy / OIDC Auth](#proxy--oidc-auth). |

#### COOKIE_SECURE

This flag controls whether session cookies are marked `Secure`, meaning the browser will only send them over HTTPS.

- **`true`** (default) — required when the app is accessible over the internet via HTTPS. Use this in production.
- **`false`** — set this when running locally over plain HTTP (e.g. `http://192.168.1.x:8080`). Without it, the browser will silently drop the cookie and you will be unable to access the app.

If you are only accessing AirWiggler on your home network and have not set up HTTPS, set `COOKIE_SECURE=false`.

### 5. Apply and open

Click **Apply**. Unraid will pull the image and start the container. Open a browser and navigate to `http://<your-server-ip>:8080`.

---

## Setup with Docker Compose

If you're not on Unraid, here's a minimal `compose.yml`:

```yaml
services:
  airwiggler:
    image: kentonbandy/airwiggler
    ports:
      - "8080:8080"
    volumes:
      - /path/to/your/music:/music:ro
    environment:
      SITE_TITLE: My Music Library
      DEFAULT_QUALITY: medium
      COOKIE_SECURE: "false"   # set to true if behind an HTTPS reverse proxy
```

---

## Proxy / OIDC Auth

If you already run an SSO solution in front of your services — such as [Authelia](https://www.authelia.com/), [Authentik](https://goauthentik.io/), or Traefik Forward Auth — you can delegate authentication entirely to the proxy instead of using `ACCESS_TOKEN`.

These tools authenticate the user (via OIDC, LDAP, etc.) and then stamp a header on every forwarded request to signal that it is authenticated. Set `AUTH_PROXY_HEADER` to the name of that header and AirWiggler will allow the request through if the header is present and non-empty.

Common header names:

| Proxy | Header |
|-------|--------|
| Authelia | `Remote-User` |
| Authentik | `X-authentik-username` |
| Traefik Forward Auth | `X-Forwarded-User` |

Example:

```yaml
environment:
  AUTH_PROXY_HEADER: Remote-User
```

> **Security requirement:** `AUTH_PROXY_HEADER` trusts the named header unconditionally. The container port must **only** be reachable from the reverse proxy — not directly from the internet — otherwise the header can be spoofed.

`AUTH_PROXY_HEADER` and `ACCESS_TOKEN` are mutually exclusive. If both are set, `AUTH_PROXY_HEADER` takes priority and a warning is logged at startup.

---

## Access Control

By default, AirWiggler is open to anyone who can reach the port. If you plan to expose it outside your home network, set an `ACCESS_TOKEN`:

1. Generate a long random string (32+ characters), for example:
   ```sh
   # Linux / macOS
   openssl rand -hex 32

   # PowerShell
   -join ((1..32) | ForEach-Object { '{0:x}' -f (Get-Random -Max 16) })
   ```
2. Set `ACCESS_TOKEN` to that value in your container config.
3. Share the app URL with your token appended:
   ```
   https://yourdomain.com/?token=<your-token>
   ```
   On first visit, a session cookie is set. After that, bookmarks and direct links work without the token in the URL.

To revoke all active sessions and issue new credentials, see [docs/revoking-access.md](docs/revoking-access.md).

---

## Push Notifications (Optional)

AirWiggler can alert you via [ntfy](https://ntfy.sh) if suspicious activity is detected — for example, repeated failed token attempts. This requires no account and is entirely optional.

See [docs/notifications.md](docs/notifications.md) for setup instructions.

---

## Rescanning

Albums are scanned automatically on startup and on the interval you configure. To trigger an immediate re-scan manually, send a request directly:

```sh
curl -X POST http://localhost:8080/api/rescan
```

---

## FLAC Compatibility

FLAC playback requires browser support. If your browser does not support FLAC (notably Safari), a warning banner will appear and you can switch to the `medium` quality tier instead.
