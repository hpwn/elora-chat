<script lang="ts">
  import { onMount } from 'svelte';

  let format: 'ndjson' | 'csv' = 'ndjson';
  let limit = 1000;
  let since_ts = '';
  let before_ts = '';

  let exportUrl = '';
  let curlCmd = '';

  function buildUrl(): string {
    if (typeof window === 'undefined') {
      return '';
    }

    const url = new URL('/api/messages/export', window.location.origin);
    if (format !== 'ndjson') {
      url.searchParams.set('format', format);
    }
    if (limit > 0) {
      url.searchParams.set('limit', String(limit));
    }

    const sinceTrimmed = since_ts.trim();
    const beforeTrimmed = before_ts.trim();
    if (sinceTrimmed && beforeTrimmed) {
      return '';
    }
    if (sinceTrimmed) {
      url.searchParams.set('since_ts', sinceTrimmed);
    }
    if (beforeTrimmed) {
      url.searchParams.set('before_ts', beforeTrimmed);
    }

    return url.toString();
  }

  function buildCurl(): string {
    const url = buildUrl();
    if (!url) {
      return '# since_ts and before_ts are mutually exclusive';
    }
    const extension = format === 'csv' ? 'csv' : 'ndjson';
    return `curl -sS "${url}" -o messages.${extension}`;
  }

  function refreshDerived() {
    exportUrl = buildUrl();
    curlCmd = buildCurl();
  }

  function openExport() {
    refreshDerived();
    if (!exportUrl) {
      alert('Choose only one of since_ts OR before_ts.');
      return;
    }
    window.open(exportUrl, '_blank');
  }

  function copyCurl() {
    refreshDerived();
    if (!curlCmd || curlCmd.startsWith('#')) {
      alert('Choose only one of since_ts OR before_ts.');
      return;
    }
    navigator.clipboard.writeText(curlCmd).then(
      () => alert('Copied curl to clipboard'),
      () => alert('Failed to copy curl')
    );
  }

  onMount(() => {
    refreshDerived();
  });
</script>

<div class="export-panel">
  <h3 class="heading">Export chat</h3>

  <div class="row">
    <label class="label" for="format">Format</label>
    <select
      id="format"
      class="field"
      bind:value={format}
      on:change={refreshDerived}
    >
      <option value="ndjson">NDJSON (default)</option>
      <option value="csv">CSV</option>
    </select>
  </div>

  <div class="row">
    <label class="label" for="limit">Limit</label>
    <input
      id="limit"
      class="field"
      type="number"
      min="1"
      step="1"
      bind:value={limit}
      on:input={refreshDerived}
    />
  </div>

  <div class="row">
    <label class="label" for="since">since_ts (ms)</label>
    <input
      id="since"
      class="field"
      type="text"
      placeholder="e.g. 1758575000000"
      bind:value={since_ts}
      on:input={refreshDerived}
    />
  </div>

  <div class="row">
    <label class="label" for="before">before_ts (ms)</label>
    <input
      id="before"
      class="field"
      type="text"
      placeholder="e.g. 1758574000000"
      bind:value={before_ts}
      on:input={refreshDerived}
    />
  </div>

  <div class="help">
    Note: <code>since_ts</code> and <code>before_ts</code> are mutually exclusive.
  </div>

  <div class="actions">
    <button class="button" type="button" on:click={openExport}>Open export</button>
    <button class="button secondary" type="button" on:click={copyCurl}>Copy curl</button>
  </div>

  <div class="preview">
    <div class="preview-label">Preview URL</div>
    <div class="preview-value">{exportUrl || '(choose only one of since_ts OR before_ts)'}</div>
    <div class="preview-label">curl</div>
    <div class="preview-value">{curlCmd}</div>
  </div>
</div>

<style>
  .export-panel {
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 12px;
    padding: 12px;
    margin: 12px 0;
    background: rgba(255, 255, 255, 0.02);
  }

  .heading {
    margin: 0 0 10px;
    font-size: 1.05rem;
  }

  .row {
    display: grid;
    grid-template-columns: 140px 1fr;
    gap: 10px;
    align-items: center;
    margin-bottom: 8px;
  }

  .label {
    font-size: 0.9rem;
    color: #ddd;
  }

  .field {
    border: 1px solid rgba(255, 255, 255, 0.25);
    border-radius: 8px;
    padding: 6px 8px;
    font: inherit;
    background: rgba(0, 0, 0, 0.35);
    color: inherit;
  }

  .help {
    font-size: 0.8rem;
    color: #b5b5b5;
    margin: 6px 0 10px;
  }

  .actions {
    display: flex;
    gap: 8px;
    margin-bottom: 10px;
  }

  .button {
    border: 1px solid rgba(255, 255, 255, 0.25);
    background: rgba(0, 0, 0, 0.4);
    border-radius: 10px;
    padding: 6px 10px;
    color: inherit;
    cursor: pointer;
  }

  .button.secondary {
    background: rgba(255, 255, 255, 0.1);
  }

  .preview-label {
    margin-top: 8px;
    font-size: 0.8rem;
    color: #b5b5b5;
  }

  .preview-value {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 0.85rem;
    background: rgba(0, 0, 0, 0.35);
    border: 1px dashed rgba(255, 255, 255, 0.25);
    border-radius: 8px;
    padding: 6px 8px;
    overflow: auto;
  }
</style>
