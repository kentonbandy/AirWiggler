/* global state */
const audio = document.getElementById('audio');

let library = null;
let currentAlbum = null;
let currentQuality = 'medium';
let currentTrackIndex = 0;

// ── Init ──────────────────────────────────────────────────────────────────

(async function init() {
  // Detect FLAC support
  if (audio.canPlayType('audio/flac') === '') {
    document.getElementById('flac-warning').classList.remove('hidden');
  }

  // Load server config (site title, default quality)
  try {
    const cfg = await fetch('/api/config').then(r => r.json());
    document.getElementById('site-title').textContent = cfg.siteTitle;
    document.title = cfg.siteTitle;
    currentQuality = cfg.defaultQuality || 'medium';
  } catch (e) {
    console.warn('Could not load config:', e);
  }

  await fetchLibrary();
})();

// ── Library ───────────────────────────────────────────────────────────────

async function fetchLibrary() {
  try {
    const data = await fetch('/api/library').then(r => r.json());
    library = data;
    renderLibrary(data.albums || []);
  } catch (e) {
    console.error('Failed to load library:', e);
  }
}

function renderLibrary(albums) {
  const grid = document.getElementById('album-grid');
  grid.innerHTML = '';

  if (albums.length === 0) {
    const msg = document.createElement('p');
    msg.style.color = 'var(--text-muted)';
    msg.style.gridColumn = '1 / -1';
    msg.textContent = 'No albums found. Make sure your /music volume is mapped and contains albums.';
    grid.appendChild(msg);
    return;
  }

  for (const album of albums) {
    const card = document.createElement('div');
    card.className = 'album-card';
    card.onclick = () => openAlbum(album);

    if (album.art) {
      const img = document.createElement('img');
      img.src = album.art;
      img.alt = album.title;
      img.loading = 'lazy';
      card.appendChild(img);
    } else {
      const placeholder = document.createElement('div');
      placeholder.className = 'album-card-no-art';
      placeholder.textContent = '\u266a';
      card.appendChild(placeholder);
    }

    const info = document.createElement('div');
    info.className = 'album-card-info';

    const title = document.createElement('h3');
    title.textContent = album.title;
    info.appendChild(title);

    const badges = document.createElement('div');
    badges.className = 'badges';
    const albumQualities = availableQualities(album);
    const showBadges = !(albumQualities.length === 1 && albumQualities[0] === 'root');
    if (showBadges) {
      for (const q of albumQualities) {
        const b = document.createElement('span');
        b.className = 'badge';
        b.textContent = q;
        badges.appendChild(b);
      }
    }
    info.appendChild(badges);
    card.appendChild(info);
    grid.appendChild(card);
  }
}

// ── Views ─────────────────────────────────────────────────────────────────

function showLibrary(pushState = true) {
  audio.pause();
  audio.src = '';
  currentAlbum = null;
  currentTrackIndex = 0;
  document.getElementById('library-view').classList.remove('hidden');
  document.getElementById('player-view').classList.add('hidden');
  document.getElementById('back-btn').classList.add('hidden');
  if (pushState) history.pushState(null, '', '#');
}

function openAlbum(album, pushState = true) {
  currentAlbum = album;

  // Pick best available quality; fall back to first available if preferred isn't present
  const available = availableQualities(album);
  if (!available.includes(currentQuality)) {
    currentQuality = available[0];
  }
  currentTrackIndex = 0;

  document.getElementById('library-view').classList.add('hidden');
  document.getElementById('player-view').classList.remove('hidden');
  document.getElementById('back-btn').classList.remove('hidden');

  const artEl = document.getElementById('player-art');
  if (album.art) {
    artEl.src = album.art;
    artEl.classList.remove('hidden');
  } else {
    artEl.src = '';
    artEl.classList.add('hidden');
  }
  document.getElementById('player-title').textContent = album.title;

  renderQualitySelector();
  renderTrackList();
  const firstTrack = album.qualities[currentQuality]?.tracks?.[0];
  document.getElementById('track-title-display').textContent = firstTrack?.title ?? '';
  document.getElementById('seek-bar').value = 0;
  document.getElementById('time-current').textContent = '0:00';
  document.getElementById('time-total').textContent = firstTrack ? formatDuration(firstTrack.duration) : '0:00';
  updatePlayButton();
  if (pushState) history.pushState({ albumId: album.id }, '', '#album/' + album.id);
}

// Handle browser back/forward.
window.addEventListener('popstate', () => {
  const hash = location.hash;
  if (!hash || hash === '#') {
    showLibrary(false);
  } else if (hash.startsWith('#album/') && library) {
    const id = hash.slice('#album/'.length);
    const album = library.albums.find(a => a.id === id);
    if (album) openAlbum(album, false);
    else showLibrary(false);
  }
});

// ── Player ────────────────────────────────────────────────────────────────

function availableQualities(album) {
  return (album.qualityOrder ?? []).filter(q => album.qualities[q]?.tracks?.length > 0);
}

function renderQualitySelector() {
  const container = document.getElementById('quality-selector');
  container.innerHTML = '';

  const available = availableQualities(currentAlbum);
  if (available.length <= 1) return;

  for (const q of available) {
    const btn = document.createElement('button');
    btn.textContent = q.charAt(0).toUpperCase() + q.slice(1);
    if (q === currentQuality) btn.classList.add('active');
    btn.addEventListener('click', () => switchQuality(q));
    container.appendChild(btn);
  }
}

function renderTrackList() {
  const list = document.getElementById('track-list');
  list.innerHTML = '';
  const tracks = currentAlbum.qualities[currentQuality]?.tracks || [];

  for (let i = 0; i < tracks.length; i++) {
    const t = tracks[i];
    const row = document.createElement('div');
    row.className = 'track-row' + (i === currentTrackIndex ? ' active' : '');
    row.dataset.index = i;
    row.onclick = () => playTrack(i);

    const num = document.createElement('span');
    num.className = 'track-num';
    num.textContent = i + 1;

    const name = document.createElement('span');
    name.className = 'track-name';
    name.textContent = t.title;

    const dur = document.createElement('span');
    dur.className = 'track-duration';
    dur.textContent = formatDuration(t.duration);

    row.append(num, name, dur);
    list.appendChild(row);
  }
}

function playTrack(index) {
  const tracks = currentAlbum.qualities[currentQuality]?.tracks || [];
  if (index < 0 || index >= tracks.length) return;

  currentTrackIndex = index;
  const track = tracks[index];

  audio.src = track.url;
  audio.load();
  audio.play().catch(e => console.warn('Playback error:', e));

  document.getElementById('track-title-display').textContent = track.title;
  updateTrackHighlight();
  updatePlayButton();
}

function switchQuality(q) {
  if (q === currentQuality) return;
  const wasPlaying = !audio.paused;
  currentQuality = q;

  renderQualitySelector();
  renderTrackList();

  const tracks = currentAlbum.qualities[currentQuality]?.tracks || [];
  if (tracks.length === 0) return;

  // Clamp index in case new quality has fewer tracks
  if (currentTrackIndex >= tracks.length) currentTrackIndex = 0;
  const track = tracks[currentTrackIndex];

  audio.src = track.url;
  audio.load();
  document.getElementById('track-title-display').textContent = track.title;

  if (wasPlaying) {
    audio.play().catch(e => console.warn('Playback error:', e));
  }
  updateTrackHighlight();
  updatePlayButton();
}

function togglePlay() {
  if (!currentAlbum) return;
  if (audio.paused) {
    // If no src loaded yet, start from current track
    if (audio.readyState === 0) {
      playTrack(currentTrackIndex);
      return;
    }
    audio.play().catch(e => console.warn('Playback error:', e));
  } else {
    audio.pause();
  }
}

function stopPlayback() {
  audio.pause();
  audio.currentTime = 0;
  updatePlayButton();
}

function prevTrack() {
  if (!currentAlbum) return;
  // If more than 3 seconds in, restart current track; otherwise go back
  if (audio.currentTime > 3) {
    audio.currentTime = 0;
  } else {
    playTrack(Math.max(0, currentTrackIndex - 1));
  }
}

function nextTrack() {
  if (!currentAlbum) return;
  const tracks = currentAlbum.qualities[currentQuality]?.tracks || [];
  if (currentTrackIndex < tracks.length - 1) {
    playTrack(currentTrackIndex + 1);
  }
}

function updateTrackHighlight() {
  const rows = document.querySelectorAll('.track-row');
  for (const row of rows) {
    row.classList.toggle('active', parseInt(row.dataset.index) === currentTrackIndex);
  }
}

function updatePlayButton() {
  document.getElementById('play-btn').textContent = audio.paused ? '\u25b6' : '\u23f8';
}

// ── Button event listeners ────────────────────────────────────────────────

document.getElementById('back-btn').addEventListener('click', showLibrary);
document.getElementById('prev-btn').addEventListener('click', prevTrack);
document.getElementById('play-btn').addEventListener('click', togglePlay);
document.getElementById('stop-btn').addEventListener('click', stopPlayback);
document.getElementById('next-btn').addEventListener('click', nextTrack);

// ── Audio events ──────────────────────────────────────────────────────────

audio.addEventListener('play', updatePlayButton);
audio.addEventListener('pause', updatePlayButton);

audio.addEventListener('timeupdate', () => {
  if (!audio.duration || isNaN(audio.duration)) return;
  const pct = (audio.currentTime / audio.duration) * 100;
  document.getElementById('seek-bar').value = pct;
  document.getElementById('time-current').textContent = formatDuration(audio.currentTime);
  document.getElementById('time-total').textContent = formatDuration(audio.duration);
});

audio.addEventListener('ended', () => {
  if (!document.getElementById('autoplay-toggle').checked) return;
  const tracks = currentAlbum?.qualities[currentQuality]?.tracks || [];
  if (currentTrackIndex < tracks.length - 1) {
    playTrack(currentTrackIndex + 1);
  }
});

document.getElementById('seek-bar').addEventListener('input', e => {
  if (!audio.duration || isNaN(audio.duration)) return;
  audio.currentTime = (e.target.value / 100) * audio.duration;
});

document.getElementById('volume').addEventListener('input', e => {
  audio.volume = parseFloat(e.target.value);
});

// ── Helpers ───────────────────────────────────────────────────────────────

function formatDuration(seconds) {
  if (!seconds || isNaN(seconds)) return '0:00';
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60).toString().padStart(2, '0');
  return `${m}:${s}`;
}

// ── Lightbox ──────────────────────────────────────────────────────────────

const lightbox = document.getElementById('lightbox');
const lightboxImg = document.getElementById('lightbox-img');
const lightboxPrev = document.getElementById('lightbox-prev');
const lightboxNext = document.getElementById('lightbox-next');

let lightboxImages = []; // full list: [cover, ...extras]
let lightboxIndex = 0;

function openLightbox(images, index) {
  lightboxImages = images;
  lightboxIndex = index;
  showLightboxImage();
  lightbox.classList.remove('hidden');
}

function showLightboxImage() {
  lightboxImg.src = lightboxImages[lightboxIndex];
  lightboxPrev.classList.toggle('hidden', lightboxIndex === 0);
  lightboxNext.classList.toggle('hidden', lightboxIndex === lightboxImages.length - 1);
}

function closeLightbox() {
  lightbox.classList.add('hidden');
  lightboxImg.src = '';
  lightboxImages = [];
}

lightboxPrev.addEventListener('click', e => {
  e.stopPropagation();
  if (lightboxIndex > 0) { lightboxIndex--; showLightboxImage(); }
});

lightboxNext.addEventListener('click', e => {
  e.stopPropagation();
  if (lightboxIndex < lightboxImages.length - 1) { lightboxIndex++; showLightboxImage(); }
});

lightbox.addEventListener('click', closeLightbox);
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') closeLightbox();
  if (e.key === 'ArrowLeft' && lightboxIndex > 0) { lightboxIndex--; showLightboxImage(); }
  if (e.key === 'ArrowRight' && lightboxIndex < lightboxImages.length - 1) { lightboxIndex++; showLightboxImage(); }
});

document.getElementById('player-art').addEventListener('click', function () {
  if (this.src && !this.classList.contains('hidden')) {
    // Use album.images (all root images, cover first) if available;
    // fall back to just the art URL.
    const images = (currentAlbum?.images?.length)
      ? currentAlbum.images
      : (currentAlbum?.art ? [currentAlbum.art] : []);
    if (images.length === 0) return;
    openLightbox(images, 0);
  }
});
