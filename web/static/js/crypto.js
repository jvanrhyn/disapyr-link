(function () {
  'use strict';

  /**
   * crypto.js — Client-side AES-GCM 256-bit encryption/decryption using Web Crypto API.
   * No external dependencies. Handles both the create and retrieve page flows.
   */

  /* ── Helpers ──────────────────────────────────────────────────────── */

  /** Encode a Uint8Array to standard base64. */
  function arrayToBase64(bytes) {
    let binary = '';
    for (let i = 0; i < bytes.byteLength; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
  }

  /** Encode a Uint8Array to URL-safe base64 (no padding). */
  function arrayToBase64Url(bytes) {
    return arrayToBase64(bytes)
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=/g, '');
  }

  /** Decode a standard base64 string to Uint8Array. */
  function base64ToArray(b64) {
    const binary = atob(b64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return bytes;
  }

  /** Decode a URL-safe base64 (no padding) string to Uint8Array. */
  function base64UrlToArray(b64url) {
    // Restore standard base64 padding
    let b64 = b64url.replace(/-/g, '+').replace(/_/g, '/');
    while (b64.length % 4 !== 0) b64 += '=';
    return base64ToArray(b64);
  }

  /* ── Key operations ───────────────────────────────────────────────────────── */

  /** Generate a new extractable AES-GCM 256-bit key. */
  async function generateKey() {
    return crypto.subtle.generateKey(
      { name: 'AES-GCM', length: 256 },
      true,               // extractable so we can export to the URL fragment
      ['encrypt', 'decrypt']
    );
  }

  /** Export a CryptoKey to URL-safe base64 (no padding). */
  async function exportKey(key) {
    const raw = await crypto.subtle.exportKey('raw', key);
    return arrayToBase64Url(new Uint8Array(raw));
  }

  /** Import an AES-GCM key from URL-safe base64. */
  async function importKey(b64url) {
    const raw = base64UrlToArray(b64url);
    return crypto.subtle.importKey(
      'raw', raw,
      { name: 'AES-GCM' },
      false,              // not extractable on the receive side
      ['decrypt']
    );
  }

  /* ── Encrypt / Decrypt ────────────────────────────────────────────────────── */

  /**
   * Encrypt an ArrayBuffer with a CryptoKey.
   * Returns { ciphertext: string, nonce: string } where both values are standard base64.
   */
  async function encrypt(buffer, key) {
    const nonce = crypto.getRandomValues(new Uint8Array(12)); // 96-bit IV for AES-GCM
    const cipherBuffer = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: nonce },
      key,
      buffer
    );
    return {
      ciphertext: arrayToBase64(new Uint8Array(cipherBuffer)),
      nonce: arrayToBase64(nonce),
    };
  }

  /**
   * Decrypt a standard base64 ciphertext and nonce using the given CryptoKey.
   * Returns the decrypted ArrayBuffer.
   */
  async function decrypt(ciphertextB64, nonceB64, key) {
    const ciphertext = base64ToArray(ciphertextB64);
    const nonce = base64ToArray(nonceB64);
    return crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: nonce },
      key,
      ciphertext
    );
  }

  /* ── Secret envelope ──────────────────────────────────────────────────────── */

  /**
   * Pack content_type, filename, and raw data into a single binary buffer.
   * Format: [4 bytes big-endian header length][UTF-8 JSON header][raw data bytes]
   * This ensures content_type and filename are encrypted alongside the secret data,
   * maintaining the zero-knowledge property (the server never sees metadata).
   */
  function packSecret(contentType, filename, dataBuffer) {
    const header = JSON.stringify({ t: contentType || 'text/plain', f: filename || '' });
    const headerBytes = new TextEncoder().encode(header);
    const lenView = new DataView(new ArrayBuffer(4));
    lenView.setUint32(0, headerBytes.length, false); // big-endian
    const packed = new Uint8Array(4 + headerBytes.length + dataBuffer.byteLength);
    packed.set(new Uint8Array(lenView.buffer), 0);
    packed.set(headerBytes, 4);
    packed.set(new Uint8Array(dataBuffer), 4 + headerBytes.length);
    return packed.buffer;
  }

  /**
   * Unpack a secret buffer created by packSecret.
   * Returns { contentType, filename, dataBuffer }.
   */
  function unpackSecret(buffer) {
    const view = new DataView(buffer);
    const headerLen = view.getUint32(0, false);
    const headerBytes = new Uint8Array(buffer, 4, headerLen);
    const header = JSON.parse(new TextDecoder().decode(headerBytes));
    return {
      contentType: header.t || 'text/plain',
      filename: header.f || '',
      dataBuffer: buffer.slice(4 + headerLen),
    };
  }

  /* ── File safety ──────────────────────────────────────────────────────────── */

  /**
   * Hard-blocked file extensions. These are executable/installer types that
   * have no legitimate reason to be shared via a secret-sharing service and
   * pose a risk to recipients. Source code (.js, .py, etc.) is intentionally
   * NOT blocked — sharing code snippets is a valid use case.
   */
  const BLOCKED_EXTENSIONS = new Set([
    // Windows executables & installers
    'exe', 'msi', 'bat', 'cmd', 'com', 'pif', 'scr',
    // Windows scripts
    'vbs', 'vbe', 'wsh', 'wsf', 'ps1', 'ps2', 'ps1xml', 'ps2xml', 'psc1', 'psc2',
    // macOS executables & installers
    'app', 'dmg', 'pkg', 'command',
    // Unix/Linux executables
    'sh', 'run',
    // Java executables
    'jar',
  ]);

  /**
   * Validate a File object against the blocklist and size limit.
   * Returns an error string if invalid, null if OK.
   */
  function validateFileInput(file) {
    if (!file) return null;

    const ext = (file.name.includes('.')
      ? file.name.split('.').pop()
      : ''
    ).toLowerCase();

    if (ext && BLOCKED_EXTENSIONS.has(ext)) {
      return `Files of type .${ext} cannot be shared — this file type is blocked for security reasons.`;
    }

    const form = document.getElementById('secret-form');
    const maxBytes = parseInt(form?.dataset?.maxBytes || String(10 * 1024 * 1024), 10);
    if (file.size > maxBytes) {
      const mb = Math.round(maxBytes / (1024 * 1024));
      return `File is too large (${(file.size / (1024 * 1024)).toFixed(1)} MB). Maximum allowed size is ${mb} MB.`;
    }

    return null;
  }

  function show(id) {
    const el = document.getElementById(id);
    if (el) el.classList.remove('hidden');
  }

  function hide(id) {
    const el = document.getElementById(id);
    if (el) el.classList.add('hidden');
  }

  function setText(id, text) {
    const el = document.getElementById(id);
    if (el) el.textContent = text;
  }

  /* ── Create page ──────────────────────────────────────────────────────────── */

  let _decryptedBuffer = null;
  let _decryptedFilename = '';
  let _decryptedContentType = '';
  let _droppedFile = null;      // fallback for browsers where DataTransfer file assignment fails
  let _dragDropInitialized = false;

  function switchMode(mode) {
    if (mode === 'text') {
      _droppedFile = null;
      show('input-text'); hide('input-file');
      document.getElementById('btn-text').classList.add('active');
      document.getElementById('btn-file').classList.remove('active');
    } else {
      hide('input-text'); show('input-file');
      document.getElementById('btn-file').classList.add('active');
      document.getElementById('btn-text').classList.remove('active');
    }
  }

  function updateFileName(input) {
    const display = document.getElementById('file-name-display');
    if (!display || !input.files[0]) return;
    const file = input.files[0];
    const err = validateFileInput(file);
    if (err) {
      display.textContent = 'Click to choose a file or drag & drop';
      input.value = '';
      _droppedFile = null;
      showCreateError(err);
      return;
    }
    hide('form-error');
    display.textContent = file.name;
  }

  function showCreateError(msg) {
    const el = document.getElementById('form-error');
    if (el) { el.textContent = msg; el.classList.remove('hidden'); }
  }

  // Set the visual submit loading state.
  function setSubmitLoading(loading) {
    const btn = document.getElementById('submit-btn');
    if (btn) btn.disabled = loading;
    loading ? show('submit-spinner') : hide('submit-spinner');
  }

  function showCreated(link) {
    hide('create-panel');
    const panel = document.getElementById('created-panel');
    if (panel) panel.classList.remove('hidden');
    const input = document.getElementById('secret-link');
    if (input) input.value = link;
  }

  function resetForm() {
    show('create-panel');
    hide('created-panel');
    const ta = document.getElementById('secret-text');
    if (ta) ta.value = '';
    const fi = document.getElementById('secret-file');
    if (fi) fi.value = '';
    setText('file-name-display', 'Click to choose a file or drag & drop');
    _droppedFile = null;
    hide('form-error');
    switchMode('text');
  }

  async function copyLink() {
    const link = document.getElementById('secret-link')?.value;
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link);
      const btn = document.getElementById('copy-btn');
      if (btn) {
        const label = document.getElementById('copy-label');
        if (label) label.textContent = 'Copied!';
        btn.classList.add('copied');
        setTimeout(() => {
          if (label) label.textContent = 'Copy';
          btn.classList.remove('copied');
        }, 2000);
      }
    } catch {
      document.getElementById('secret-link')?.select();
      document.execCommand('copy');
    }
  }

  async function copySecret() {
    const pre = document.getElementById('revealed-text');
    if (!pre) return;
    try {
      await navigator.clipboard.writeText(pre.textContent);
      const btn = document.getElementById('copy-secret-btn');
      if (btn) {
        const label = btn.querySelector('.copy-secret-label');
        if (label) label.textContent = 'Copied!';
        btn.classList.add('copied');
        setTimeout(() => {
          if (label) label.textContent = 'Copy';
          btn.classList.remove('copied');
        }, 2000);
      }
    } catch {
      const range = document.createRange();
      range.selectNodeContents(pre);
      const sel = window.getSelection();
      if (sel) { sel.removeAllRanges(); sel.addRange(range); }
      document.execCommand('copy');
    }
  }

  function initCreateForm() {
    initCreatePageEvents();
    const form = document.getElementById('secret-form');
    if (!form) return;

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      hide('form-error');

      const fileInput = document.getElementById('secret-file');
      const textInput = document.getElementById('secret-text');
      const expiresIn = parseInt(document.getElementById('expires-in')?.value || '0', 10);

      let buffer, contentType, filename;

      const isFileMode = !document.getElementById('input-file')?.classList.contains('hidden');

      if (isFileMode && (fileInput?.files[0] || _droppedFile)) {
        const file = fileInput?.files[0] || _droppedFile;
        const fileErr = validateFileInput(file);
        if (fileErr) {
          showCreateError(fileErr);
          return;
        }
        buffer = await file.arrayBuffer();
        contentType = file.type || 'application/octet-stream';
        filename = file.name;
      } else {
        const text = textInput?.value?.trim() || '';
        if (!text) {
          showCreateError('Please enter a secret before creating a link.');
          return;
        }
        buffer = new TextEncoder().encode(text).buffer;
        contentType = 'text/plain';
        filename = '';
      }

      setSubmitLoading(true);

      try {
        const key = await generateKey();
        const keyB64 = await exportKey(key);

        // Pack metadata + data into a single buffer before encrypting.
        // This ensures content_type and filename are never seen by the server.
        const packed = packSecret(contentType, filename, buffer);
        const { ciphertext, nonce } = await encrypt(packed, key);

        const resp = await fetch('/', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ciphertext, nonce, expires_in: expiresIn }),
        });

        if (!resp.ok) {
          const msg = resp.status === 413 ? 'File is too large.' : 'Failed to store secret. Please try again.';
          showCreateError(msg);
          return;
        }

        const data = await resp.json();
        const link = `${window.location.origin}/s/${data.token}#key=${keyB64}`;
        showCreated(link);
      } catch (err) {
        console.error('create error:', err);
        showCreateError('Encryption failed. Please try again.');
      } finally {
        _droppedFile = null;
        setSubmitLoading(false);
      }
    });
  }

  /* ── Retrieve page ────────────────────────────────────────────────────────── */

  function showRevealError(msg) {
    const el = document.getElementById('reveal-error');
    if (el) { el.textContent = msg; el.classList.remove('hidden'); }
  }

  function setRevealLoading(loading) {
    const btn = document.getElementById('reveal-btn');
    if (btn) btn.disabled = loading;
    loading ? show('reveal-spinner') : hide('reveal-spinner');
  }

  function downloadFile() {
    if (!_decryptedBuffer) return;
    const blob = new Blob([_decryptedBuffer], { type: _decryptedContentType });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = _decryptedFilename || 'secret-file';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }

  async function revealSecret() {
    const hash = window.location.hash;
    const keyB64 = hash.startsWith('#key=') ? hash.slice(5) : null;

    if (!keyB64) {
      showRevealError('No encryption key found in this URL. Make sure you have the complete link including the #key= fragment.');
      return;
    }

    // Immediately clear the hash from the browser URL address bar and history to prevent exposure.
    // Preserves query strings if present.
    window.history.replaceState(null, '', window.location.pathname + window.location.search);

    const token = document.getElementById('secret-page')?.dataset?.token;
    if (!token) {
      showRevealError('Invalid page state.');
      return;
    }

    setRevealLoading(true);
    hide('reveal-error');

    try {
      const resp = await fetch(`/s/${token}/reveal`, { method: 'POST' });

      if (resp.status === 404) {
        hide('reveal-panel');
        show('gone-panel');
        return;
      }

      if (!resp.ok) {
        showRevealError('Failed to retrieve secret. Please try again.');
        return;
      }

      const { ciphertext, nonce } = await resp.json();
      const key = await importKey(keyB64);
      const plainBuffer = await decrypt(ciphertext, nonce, key);

      // Unpack the envelope to get content_type, filename, and data.
      const { contentType: content_type, filename, dataBuffer } = unpackSecret(plainBuffer);

      hide('reveal-panel');

      if (content_type && !content_type.startsWith('text/')) {
        _decryptedBuffer = dataBuffer;
        _decryptedFilename = filename || 'secret-file';
        _decryptedContentType = content_type;

        const info = document.getElementById('file-info');
        if (info) info.textContent = filename ? `File: ${filename} (${content_type})` : `File type: ${content_type}`;

        show('file-panel');
      } else {
        const text = new TextDecoder().decode(dataBuffer);
        const pre = document.getElementById('revealed-text');
        // Safely set as text content to prevent XSS
        if (pre) pre.textContent = text;
        show('text-panel');
      }
    } catch (err) {
      console.error('reveal error:', err);
      showRevealError('Decryption failed. The key may be incorrect or the data may be corrupted.');
    } finally {
      setRevealLoading(false);
    }
  }

  function initRetrievePage() {
    initRetrievePageEvents();
    const page = document.getElementById('secret-page');
    if (!page) return;

    // If there is no key fragment, warn the user immediately.
    if (!window.location.hash.startsWith('#key=')) {
      showRevealError('Warning: The URL appears to be missing the encryption key (#key=…). The full link is required to decrypt the secret.');
    }
  }

  /* ── Bootstrap ────────────────────────────────────────────────────────────── */

  document.addEventListener('DOMContentLoaded', () => {
    initCreateForm();
    initRetrievePage();
  });

  /* ── Create page event wiring ─────────────────────────────────────────────── */

  function initCreatePageEvents() {
    document.getElementById('btn-text')?.addEventListener('click', () => switchMode('text'));
    document.getElementById('btn-file')?.addEventListener('click', () => switchMode('file'));
    document.getElementById('secret-file')?.addEventListener('change', function () { updateFileName(this); });
    document.getElementById('copy-btn')?.addEventListener('click', copyLink);
    document.getElementById('reset-btn')?.addEventListener('click', resetForm);
    initDragAndDrop();
  }

  function initDragAndDrop() {
    if (_dragDropInitialized) return;
    _dragDropInitialized = true;

    const dropZone = document.getElementById('input-file');
    const fileLabel = dropZone?.querySelector('.file-label');
    const fileInput = document.getElementById('secret-file');
    if (!dropZone || !fileInput) return;

    // Prevent browser from navigating to dropped files, but only for file drags
    // (scoped to Files type to avoid breaking text-drag into other inputs)
    window.addEventListener('dragover', (e) => {
      if (e.dataTransfer?.types?.includes('Files')) e.preventDefault();
    }, false);
    window.addEventListener('drop', (e) => {
      if (e.dataTransfer?.types?.includes('Files')) e.preventDefault();
    }, false);

    dropZone.addEventListener('dragover', (e) => {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
      fileLabel?.classList.add('drag-active');
    });

    dropZone.addEventListener('dragleave', () => {
      fileLabel?.classList.remove('drag-active');
    });

    dropZone.addEventListener('drop', (e) => {
      e.preventDefault();
      fileLabel?.classList.remove('drag-active');
      const file = e.dataTransfer.files[0];
      if (!file) return;
      const err = validateFileInput(file);
      if (err) {
        showCreateError(err);
        return;
      }
      try {
        const dt = new DataTransfer();
        dt.items.add(file);
        fileInput.files = dt.files;
        updateFileName(fileInput);
      } catch {
        // DataTransfer file assignment not supported — store for submit fallback
        // and update display directly since fileInput.files is still empty
        _droppedFile = file;
        const display = document.getElementById('file-name-display');
        if (display) display.textContent = file.name;
      }
    });
  }

  /* ── Retrieve page event wiring ───────────────────────────────────────────── */

  function initRetrievePageEvents() {
    document.getElementById('reveal-btn')?.addEventListener('click', revealSecret);
    document.getElementById('download-btn')?.addEventListener('click', downloadFile);
    document.getElementById('copy-secret-btn')?.addEventListener('click', copySecret);
  }

})();
