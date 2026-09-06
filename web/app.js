/* global state */
const audioPlayers = [document.getElementById('audio'), new Audio()];
let audio = audioPlayers[0];
audioPlayers[1].preload = 'auto';

// Start the preloaded next track slightly before the browser reports `ended`.
// This trades a tiny cut-off at the end of the current track for a smoother
// transition with HTMLAudioElement playback.
const NEXT_TRACK_HANDOFF_SECONDS = 0.275;

let library = null;
let currentAlbum = null;   // album whose audio is loaded/playing
let currentQuality = 'medium';
let currentTrackIndex = 0;
let viewAlbum = null;      // album currently displayed in the player view
let viewQuality = 'medium';
const albumCardMap = new Map(); // albumId → card DOM element
let currentShareLinks = null;
let toastTimer = null;
let preloadedNext = null;

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
    viewQuality = currentQuality;
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
    restoreViewFromLocation();
  } catch (e) {
    console.error('Failed to load library:', e);
  }
}

function restoreViewFromLocation() {
  const hash = location.hash;
  if (!hash || hash === '#') return;

  if (hash.startsWith('#album/') && library) {
    const id = hash.slice('#album/'.length);
    const album = library.albums.find(a => a.id === id);
    if (album) openAlbum(album, false);
  }
}

function renderLibrary(albums) {
  const grid = document.getElementById('album-grid');
  albumCardMap.clear();
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
    albumCardMap.set(album.id, card);
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
  updateNowPlayingBadge();
}

// ── Views ─────────────────────────────────────────────────────────────────

function showLibrary(pushState = true) {
  document.getElementById('library-view').classList.remove('hidden');
  document.getElementById('player-view').classList.add('hidden');
  document.getElementById('back-btn').classList.add('hidden');
  viewAlbum = null;
  updateNowPlayingBadge();
  if (pushState) history.pushState(null, '', '#');
}

function openAlbum(album, pushState = true) {
  viewAlbum = album;

  // Pick best available quality for the view; fall back to first available if preferred isn't present
  const available = availableQualities(album);
  if (!available.includes(viewQuality)) {
    viewQuality = available[0];
  }

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
  updateAlbumPlayButton();
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

  const available = availableQualities(viewAlbum);
  if (available.length <= 1) return;

  for (const q of available) {
    const btn = document.createElement('button');
    btn.textContent = q.charAt(0).toUpperCase() + q.slice(1);
    if (q === viewQuality) btn.classList.add('active');
    btn.addEventListener('click', () => switchQuality(q));
    container.appendChild(btn);
  }
}

function renderTrackList() {
  const list = document.getElementById('track-list');
  list.innerHTML = '';
  const tracks = viewAlbum.qualities[viewQuality]?.tracks || [];
  const isViewingPlaying = viewAlbum.id === currentAlbum?.id && viewQuality === currentQuality;

  for (let i = 0; i < tracks.length; i++) {
    const t = tracks[i];
    const row = document.createElement('div');
    row.className = 'track-row' + (isViewingPlaying && i === currentTrackIndex ? ' active' : '');
    row.dataset.index = i;
    row.onclick = () => playTrackFromView(i);

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

function toggleAlbumPlaybackFromView() {
  if (!viewAlbum) return;

  const isViewingCurrentAlbum = viewAlbum.id === currentAlbum?.id;
  if (isViewingCurrentAlbum && audio.src) {
    togglePlay();
    return;
  }

  currentAlbum = viewAlbum;
  currentQuality = viewQuality;
  updateNowPlayingBadge();
  playTrack(0);
}

function playTrackFromView(index) {
  // Sync playing state from the viewed album before playing
  if (viewAlbum.id !== currentAlbum?.id || viewQuality !== currentQuality) {
    currentAlbum = viewAlbum;
    currentQuality = viewQuality;
    updateNowPlayingBadge();
  }
  playTrack(index);
}

function playTrack(index) {
  const tracks = currentAlbum.qualities[currentQuality]?.tracks || [];
  if (index < 0 || index >= tracks.length) return;

  currentTrackIndex = index;
  const track = tracks[index];

  clearPreload();
  audio.src = track.url;
  audio.load();
  audio.play().catch(e => console.warn('Playback error:', e));

  updatePlayingUI(track);
  preloadNextTrack();
}

function updatePlayingUI(track) {
  // Reveal controls on first play and clear idle state
  const controls = document.getElementById('controls');
  controls.classList.remove('hidden', 'idle');
  document.getElementById('now-playing-idle').classList.add('hidden');
  document.getElementById('track-title-display').classList.remove('hidden');
  document.getElementById('track-title-display').textContent = track.title;

  const artEl = document.getElementById('now-playing-art');
  if (currentAlbum.art) {
    artEl.src = currentAlbum.art;
    artEl.classList.remove('hidden');
  } else {
    artEl.src = '';
    artEl.classList.add('hidden');
  }
  updateTrackHighlight();
  updatePlayButton();
}

function inactiveAudio() {
  return audio === audioPlayers[0] ? audioPlayers[1] : audioPlayers[0];
}

function clearPreload() {
  const player = inactiveAudio();
  player.pause();
  player.removeAttribute('src');
  player.load();
  preloadedNext = null;
}

function preloadNextTrack() {
  const tracks = currentAlbum?.qualities[currentQuality]?.tracks || [];
  const nextIndex = currentTrackIndex + 1;
  if (nextIndex >= tracks.length) {
    clearPreload();
    return;
  }

  const nextTrack = tracks[nextIndex];
  const nextState = {
    albumId: currentAlbum.id,
    quality: currentQuality,
    index: nextIndex,
    url: nextTrack.url,
  };
  if (preloadedNext && preloadedNext.albumId === nextState.albumId &&
      preloadedNext.quality === nextState.quality &&
      preloadedNext.index === nextState.index &&
      preloadedNext.url === nextState.url) {
    return;
  }

  const player = inactiveAudio();
  player.volume = audio.volume;
  player.src = nextTrack.url;
  player.preload = 'auto';
  player.load();
  preloadedNext = nextState;
}

function playPreloadedNextTrack() {
  const tracks = currentAlbum?.qualities[currentQuality]?.tracks || [];
  const nextIndex = currentTrackIndex + 1;
  const nextTrack = tracks[nextIndex];
  if (!nextTrack || !preloadedNext ||
      preloadedNext.albumId !== currentAlbum.id ||
      preloadedNext.quality !== currentQuality ||
      preloadedNext.index !== nextIndex ||
      preloadedNext.url !== nextTrack.url) {
    return false;
  }

  const previousAudio = audio;
  audio = inactiveAudio();
  previousAudio.pause();
  previousAudio.removeAttribute('src');
  previousAudio.load();

  currentTrackIndex = nextIndex;
  preloadedNext = null;
  audio.play().catch(e => console.warn('Playback error:', e));
  updatePlayingUI(nextTrack);
  preloadNextTrack();
  return true;
}

function switchQuality(q) {
  if (q === viewQuality) return;
  viewQuality = q;

  // If viewing the currently playing album, also switch playback quality
  if (currentAlbum && viewAlbum.id === currentAlbum.id) {
    const wasPlaying = !audio.paused;
    currentQuality = q;

    renderQualitySelector();
    renderTrackList();

    const tracks = currentAlbum.qualities[currentQuality]?.tracks || [];
    if (tracks.length === 0) return;

    // Clamp index in case new quality has fewer tracks
    if (currentTrackIndex >= tracks.length) currentTrackIndex = 0;
    const track = tracks[currentTrackIndex];

    clearPreload();
    audio.src = track.url;
    audio.load();
    document.getElementById('track-title-display').textContent = track.title;

    if (wasPlaying) {
      audio.play().catch(e => console.warn('Playback error:', e));
    }
    updateTrackHighlight();
    updatePlayButton();
    preloadNextTrack();
  } else {
    // View-only: just re-render the player view
    renderQualitySelector();
    renderTrackList();
  }
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
  for (const player of audioPlayers) {
    player.pause();
    player.removeAttribute('src');
    player.load();
  }
  audio = audioPlayers[0];
  preloadedNext = null;
  currentAlbum = null;
  currentTrackIndex = 0;
  updatePlayButton();

  const controls = document.getElementById('controls');
  controls.classList.add('idle');
  document.getElementById('track-title-display').classList.add('hidden');
  document.getElementById('now-playing-idle').classList.remove('hidden');
  const nowPlayingArt = document.getElementById('now-playing-art');
  nowPlayingArt.src = '';
  nowPlayingArt.classList.add('hidden');
  document.getElementById('seek-bar').value = 0;
  document.getElementById('time-current').textContent = '0:00';
  document.getElementById('time-total').textContent = '0:00';

  updateNowPlayingBadge();
  // Re-render track list to clear active highlight
  if (viewAlbum) renderTrackList();
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
    if (!playPreloadedNextTrack()) playTrack(currentTrackIndex + 1);
  }
}

function updateTrackHighlight() {
  const isViewingPlaying = viewAlbum?.id === currentAlbum?.id && viewQuality === currentQuality;
  const rows = document.querySelectorAll('.track-row');
  for (const row of rows) {
    row.classList.toggle('active', isViewingPlaying && parseInt(row.dataset.index) === currentTrackIndex);
  }
}

function updatePlayButton() {
  document.getElementById('play-btn').textContent = audio.paused ? '\u25b6' : '\u23f8';
  updateAlbumPlayButton();
}

function updateAlbumPlayButton() {
  const btn = document.getElementById('album-play-btn');
  const isViewingCurrentAlbum = viewAlbum?.id === currentAlbum?.id;
  const isPlayingViewedAlbum = isViewingCurrentAlbum && !audio.paused;
  btn.textContent = isPlayingViewedAlbum ? '\u23f8' : '\u25b6';
  btn.title = isPlayingViewedAlbum ? 'Pause album' : 'Play album from the beginning';
  btn.setAttribute('aria-label', btn.title);
  btn.classList.toggle('playing', isPlayingViewedAlbum);
}

function updateNowPlayingBadge() {
  for (const [id, card] of albumCardMap) {
    card.classList.toggle('now-playing', id === currentAlbum?.id);
  }
}

// ── Sharing ───────────────────────────────────────────────────────────────

async function handleShareButtonClick() {
  if (!viewAlbum) return;

  const dropdown = document.getElementById('share-dropdown');
  if (!dropdown.classList.contains('hidden')) {
    closeShareDropdown();
    return;
  }

  try {
    const links = await loadShareLinks();
    if (!links.albumOnlyLink) {
      await copyTextToClipboard(links.fullAccessLink);
      showToast('Link copied');
      return;
    }
    openShareDropdown();
  } catch (e) {
    showToast(e.message || 'Could not generate share link');
  }
}

function openShareDropdown() {
  document.getElementById('share-dropdown').classList.remove('hidden');
  document.getElementById('album-share-btn').setAttribute('aria-expanded', 'true');
}

function closeShareDropdown() {
  document.getElementById('share-dropdown').classList.add('hidden');
  document.getElementById('album-share-btn').setAttribute('aria-expanded', 'false');
}

async function loadShareLinks() {
  if (currentShareLinks?.albumId === viewAlbum?.id) return currentShareLinks;
  const resp = await fetch('/api/share?album=' + encodeURIComponent(viewAlbum.id));
  if (!resp.ok) throw new Error('Share links are only available to full-library users.');
  currentShareLinks = await resp.json();
  currentShareLinks.albumId = viewAlbum.id;
  return currentShareLinks;
}

async function copyShareLink(kind) {
  if (!viewAlbum) return;
  closeShareDropdown();
  try {
    const links = await loadShareLinks();
    const link = kind === 'albumOnly' ? links.albumOnlyLink : links.fullAccessLink;
    if (!link) throw new Error('That link type is not configured.');
    await copyTextToClipboard(link);
    showToast('Link copied');
  } catch (e) {
    showToast(e.message || 'Could not copy link');
  }
}

async function copyTextToClipboard(text) {
  await navigator.clipboard.writeText(text);
}

function showToast(text) {
  const toast = document.getElementById('toast');
  toast.textContent = text;
  toast.classList.remove('hidden');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.add('hidden'), 2200);
}

// ── Button event listeners ────────────────────────────────────────────────

document.getElementById('site-title').addEventListener('click', e => {
  e.preventDefault();
  showLibrary();
});
document.getElementById('back-btn').addEventListener('click', showLibrary);
document.getElementById('album-play-btn').addEventListener('click', toggleAlbumPlaybackFromView);
document.getElementById('album-share-btn').addEventListener('click', e => {
  e.stopPropagation();
  handleShareButtonClick();
});
document.getElementById('share-dropdown').addEventListener('click', e => e.stopPropagation());
document.getElementById('share-album-only-btn').addEventListener('click', () => copyShareLink('albumOnly'));
document.getElementById('share-full-access-btn').addEventListener('click', () => copyShareLink('fullAccess'));
document.addEventListener('click', closeShareDropdown);
document.getElementById('prev-btn').addEventListener('click', prevTrack);
document.getElementById('play-btn').addEventListener('click', togglePlay);
document.getElementById('stop-btn').addEventListener('click', stopPlayback);
document.getElementById('next-btn').addEventListener('click', nextTrack);

// ── Audio events ──────────────────────────────────────────────────────────

for (const player of audioPlayers) {
  player.addEventListener('play', () => {
    if (player === audio) updatePlayButton();
  });

  player.addEventListener('pause', () => {
    if (player === audio) updatePlayButton();
  });

  player.addEventListener('timeupdate', () => {
    if (player !== audio) return;
    if (!audio.duration || isNaN(audio.duration)) return;

    const remaining = audio.duration - audio.currentTime;
    const tracks = currentAlbum?.qualities[currentQuality]?.tracks || [];
    if (!audio.paused &&
        document.getElementById('autoplay-toggle').checked &&
        currentTrackIndex < tracks.length - 1 &&
        remaining > 0 && remaining <= NEXT_TRACK_HANDOFF_SECONDS) {
      if (playPreloadedNextTrack()) return;
    }

    const pct = (audio.currentTime / audio.duration) * 100;
    document.getElementById('seek-bar').value = pct;
    document.getElementById('time-current').textContent = formatDuration(audio.currentTime);
    document.getElementById('time-total').textContent = formatDuration(audio.duration);
  });

  player.addEventListener('ended', () => {
    if (player !== audio) return;
    if (!document.getElementById('autoplay-toggle').checked) return;
    const tracks = currentAlbum?.qualities[currentQuality]?.tracks || [];
    if (currentTrackIndex < tracks.length - 1) {
      if (!playPreloadedNextTrack()) playTrack(currentTrackIndex + 1);
    }
  });
}

document.getElementById('seek-bar').addEventListener('input', e => {
  if (!audio.duration || isNaN(audio.duration)) return;
  audio.currentTime = (e.target.value / 100) * audio.duration;
});

document.getElementById('volume').addEventListener('input', e => {
  const volume = parseFloat(e.target.value);
  for (const player of audioPlayers) player.volume = volume;
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
    const images = (viewAlbum?.images?.length)
      ? viewAlbum.images
      : (viewAlbum?.art ? [viewAlbum.art] : []);
    if (images.length === 0) return;
    openLightbox(images, 0);
  }
});
