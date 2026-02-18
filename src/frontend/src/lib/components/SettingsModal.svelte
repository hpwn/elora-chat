<script lang="ts">
  import { tick } from 'svelte';
  import ExportPanel from './ExportPanel.svelte';
  import { apiPath } from '$lib/config';
  import { pushRecent, settings, type Settings } from '$lib/stores/settings';

  type TopTwitchItem = {
    login: string;
    display_name: string;
    viewer_count: number;
    url: string;
  };

  type TopYouTubeItem = {
    display_name: string;
    url: string;
  };

  let dialog: HTMLDivElement | null = null;
  export let open = false;

  let activeTab: 'quick' | 'advanced' | 'export' = 'quick';
  let apiBaseDraft = '';
  let wsUrlDraft = '';
  let twitchDraft = '';
  let youtubeDraft = '';

  let twitchTopLoading = false;
  let youtubeTopLoading = false;
  let twitchTopError = '';
  let youtubeTopError = '';
  let twitchTopList: TopTwitchItem[] = [];
  let youtubeTopList: TopYouTubeItem[] = [];

  $: if (open) {
    tick().then(() => {
      dialog?.focus();
      syncDraftsFromSettings();
    });
  }

  function close() {
    open = false;
  }

  function setTab(tab: 'quick' | 'advanced' | 'export') {
    activeTab = tab;
  }

  function updateSettings(patch: Partial<Settings>) {
    settings.update((current) => ({ ...current, ...patch }));
  }

  function syncDraftsFromSettings() {
    apiBaseDraft = $settings.apiBaseUrl;
    wsUrlDraft = $settings.wsUrl;
    twitchDraft = $settings.twitchUrl || $settings.recentTwitch[0] || '';
    youtubeDraft = $settings.youtubeUrl || $settings.recentYouTube[0] || '';
  }

  function applyConnectionSettings() {
    const apiBaseUrl = apiBaseDraft.trim();
    const wsUrl = wsUrlDraft.trim();
    updateSettings({ apiBaseUrl, wsUrl });
    apiBaseDraft = apiBaseUrl;
    wsUrlDraft = wsUrl;
  }

  function normalizeTwitchValue(raw: string): string {
    const trimmed = raw.trim();
    if (!trimmed) return '';

    try {
      const url = new URL(trimmed);
      if (url.hostname.toLowerCase().includes('twitch.tv')) {
        const login = url.pathname.split('/').filter(Boolean)[0] ?? '';
        if (login) return `https://www.twitch.tv/${login}`;
      }
      return trimmed;
    } catch {
      const login = trimmed.replace(/^@/, '').replace(/^\/+/, '').split(/[/?#]/)[0];
      return login ? `https://www.twitch.tv/${login}` : trimmed;
    }
  }

  function normalizeYouTubeValue(raw: string): string {
    const trimmed = raw.trim();
    if (!trimmed) return '';

    if (/^[a-zA-Z0-9_-]{11}$/.test(trimmed)) {
      return `https://www.youtube.com/watch?v=${trimmed}`;
    }

    try {
      const url = new URL(trimmed);
      if (url.hostname.toLowerCase().includes('youtu')) {
        if (url.hostname.toLowerCase() === 'youtu.be') {
          const id = url.pathname.split('/').filter(Boolean)[0] ?? '';
          if (id) return `https://www.youtube.com/watch?v=${id}`;
        }
        const id = url.searchParams.get('v') ?? '';
        if (id) return `https://www.youtube.com/watch?v=${id}`;
      }
      return trimmed;
    } catch {
      return trimmed;
    }
  }

  function applyTwitch(value?: string) {
    const normalized = normalizeTwitchValue(value ?? twitchDraft);
    if (!normalized) return;
    updateSettings({ twitchUrl: normalized, recentTwitch: pushRecent($settings.recentTwitch, normalized) });
    twitchDraft = normalized;
  }

  function applyYouTube(value?: string) {
    const normalized = normalizeYouTubeValue(value ?? youtubeDraft);
    if (!normalized) return;
    updateSettings({ youtubeUrl: normalized, recentYouTube: pushRecent($settings.recentYouTube, normalized) });
    youtubeDraft = normalized;
  }

  async function loadTopTwitch() {
    twitchTopError = '';
    twitchTopLoading = true;
    try {
      const response = await fetch(apiPath('/api/sources/top/twitch'));
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const payload = (await response.json()) as TopTwitchItem[];
      twitchTopList = Array.isArray(payload) ? payload.slice(0, 10) : [];
    } catch (error) {
      twitchTopError = error instanceof Error ? error.message : 'Failed to load Twitch top live';
      twitchTopList = [];
    } finally {
      twitchTopLoading = false;
    }
  }

  async function loadTopYouTube() {
    youtubeTopError = '';
    youtubeTopLoading = true;
    try {
      const response = await fetch(apiPath('/api/sources/top/youtube'));
      if (!response.ok) {
        const failure = await response
          .json()
          .catch(() => ({ error: 'top_live_failed', status: response.status } as { error?: string; status?: number; reason?: string; hint?: string }));
        const reason = failure.reason ? ` (${failure.reason})` : '';
        const hint = failure.hint ? ` - ${failure.hint}` : '';
        throw new Error(`${failure.error ?? 'top_live_failed'} [${failure.status ?? response.status}]${reason}${hint}`);
      }
      const payload = (await response.json()) as TopYouTubeItem[];
      youtubeTopList = Array.isArray(payload) ? payload.slice(0, 10) : [];
    } catch (error) {
      youtubeTopError = error instanceof Error ? error.message : 'Failed to load YouTube top live';
      youtubeTopList = [];
    } finally {
      youtubeTopLoading = false;
    }
  }

  function handleOverlayKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      close();
    }
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (!open) {
      return;
    }

    if (event.key === 'Escape') {
      event.preventDefault();
      close();
    }
  }
</script>

<svelte:window on:keydown={handleWindowKeydown} />

{#if open}
  <div
    aria-label="Close settings"
    class="backdrop"
    on:click={close}
    on:keydown={handleOverlayKeydown}
    role="button"
    tabindex="0"
  ></div>
  <div
    aria-labelledby="settings-modal-title"
    aria-modal="true"
    bind:this={dialog}
    class="modal"
    role="dialog"
    tabindex="-1"
  >
    <header class="modal__header">
      <h2 class="modal__title" id="settings-modal-title">Settings</h2>
      <button class="modal__close" on:click={close} type="button" aria-label="Close settings">
        x
      </button>
    </header>

    <nav class="tabs" aria-label="Settings tabs">
      <button class:active={activeTab === 'quick'} type="button" on:click={() => setTab('quick')}>Quick</button>
      <button class:active={activeTab === 'advanced'} type="button" on:click={() => setTab('advanced')}>Advanced</button>
      <button class:active={activeTab === 'export'} type="button" on:click={() => setTab('export')}>Export</button>
    </nav>

    <div class="modal__content">
      {#if activeTab === 'quick'}
        <section class="section">
          <h3>Connection</h3>
          <label>
            API Base URL
            <input
              type="text"
              bind:value={apiBaseDraft}
              on:blur={applyConnectionSettings}
              placeholder="http://localhost:8080"
            />
          </label>
          <label>
            WS URL
            <input
              type="text"
              bind:value={wsUrlDraft}
              on:blur={applyConnectionSettings}
              placeholder="ws://localhost:8080/ws/chat"
            />
          </label>
          <button type="button" on:click={applyConnectionSettings}>Apply connection</button>
          <p class="current">Current API: {$settings.apiBaseUrl || '(default)'}</p>
          <p class="current">Current WS: {$settings.wsUrl || '(derived from API)'}</p>
        </section>

        <section class="section">
          <h3>Twitch</h3>
          <div class="apply-row">
            <input
              type="text"
              bind:value={twitchDraft}
              on:blur={() => applyTwitch()}
              placeholder="channel or https://www.twitch.tv/..."
            />
            <button type="button" on:click={() => applyTwitch()}>Apply</button>
            <button type="button" on:click={loadTopTwitch} disabled={twitchTopLoading}>
              {twitchTopLoading ? 'Loading...' : 'Top Live (Twitch)'}
            </button>
          </div>
          <div class="chip-list">
            {#each $settings.recentTwitch as value}
              <button type="button" class="chip" on:click={() => applyTwitch(value)}>{value}</button>
            {/each}
          </div>
          <p class="current">Current Twitch: {$settings.twitchUrl || '(not set)'}</p>
          {#if twitchTopError}
            <p class="error">{twitchTopError}</p>
          {/if}
          {#if twitchTopList.length > 0}
            <ul class="source-list">
              {#each twitchTopList as item}
                <li>
                  <button type="button" on:click={() => applyTwitch(item.url)}>
                    {item.display_name} ({item.viewer_count.toLocaleString()})
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </section>

        <section class="section">
          <h3>YouTube</h3>
          <div class="apply-row">
            <input
              type="text"
              bind:value={youtubeDraft}
              on:blur={() => applyYouTube()}
              placeholder="video id or https://www.youtube.com/watch?v=..."
            />
            <button type="button" on:click={() => applyYouTube()}>Apply</button>
            <button type="button" on:click={loadTopYouTube} disabled={youtubeTopLoading}>
              {youtubeTopLoading ? 'Loading...' : 'Suggested Live (YouTube)'}
            </button>
          </div>
          <div class="chip-list">
            {#each $settings.recentYouTube as value}
              <button type="button" class="chip" on:click={() => applyYouTube(value)}>{value}</button>
            {/each}
          </div>
          <p class="current">Current YouTube: {$settings.youtubeUrl || '(not set)'}</p>
          {#if youtubeTopError}
            <p class="error">{youtubeTopError}</p>
          {/if}
          {#if youtubeTopList.length > 0}
            <ul class="source-list">
              {#each youtubeTopList as item}
                <li>
                  <button type="button" on:click={() => applyYouTube(item.url)}>
                    {item.display_name}
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      {:else if activeTab === 'advanced'}
        <section class="section">
          <h3>Advanced</h3>
          <label class="toggle">
            <input
              checked={$settings.showBadges}
              on:change={(event) => updateSettings({ showBadges: (event.currentTarget as HTMLInputElement).checked })}
              type="checkbox"
            />
            Show badges
          </label>
          <label class="toggle">
            <input
              checked={$settings.hideYouTubeAt}
              on:change={(event) => updateSettings({ hideYouTubeAt: (event.currentTarget as HTMLInputElement).checked })}
              type="checkbox"
            />
            Hide @ prefix for YouTube users
          </label>
          <label class="toggle">
            <input
              checked={$settings.fetchHistoryOnLoad}
              on:change={(event) => updateSettings({ fetchHistoryOnLoad: (event.currentTarget as HTMLInputElement).checked })}
              type="checkbox"
            />
            Fetch history on load
          </label>
          <label class="toggle">
            <input
              checked={$settings.chatDebug}
              on:change={(event) => updateSettings({ chatDebug: (event.currentTarget as HTMLInputElement).checked })}
              type="checkbox"
            />
            Chat debug
          </label>
          <label class="toggle">
            <input
              checked={$settings.settingsDebug}
              on:change={(event) => updateSettings({ settingsDebug: (event.currentTarget as HTMLInputElement).checked })}
              type="checkbox"
            />
            Settings debug (show apply/connect events in chat)
          </label>
        </section>
      {:else}
        <section class="section">
          <ExportPanel />
        </section>
      {/if}
    </div>
  </div>
{/if}

<style lang="scss">
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    z-index: 40;
  }

  .modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(96vw, 960px);
    max-height: min(90vh, 840px);
    overflow: auto;
    border-radius: 1rem;
    background: #111;
    border: 1px solid #3f3f46;
    padding: 1rem;
    z-index: 50;
    color: #f4f4f5;
  }

  .modal__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.75rem;
  }

  .modal__title {
    font-size: 1.125rem;
    font-weight: 600;
    margin: 0;
  }

  .modal__close {
    border: none;
    border-radius: 0.5rem;
    background: #1f2937;
    color: inherit;
    padding: 0.25rem 0.75rem;
    cursor: pointer;
  }

  .tabs {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.75rem;
  }

  .tabs button {
    border: 1px solid #374151;
    background: #1f2937;
    color: inherit;
    border-radius: 0.5rem;
    padding: 0.4rem 0.8rem;
    cursor: pointer;
  }

  .tabs button.active {
    background: #374151;
  }

  .modal__content {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    border: 1px solid #2f2f37;
    border-radius: 0.75rem;
    padding: 0.75rem;
  }

  .section h3 {
    margin: 0 0 0.25rem;
    font-size: 1rem;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    font-size: 0.9rem;
  }

  input[type='text'] {
    width: 100%;
    border: 1px solid #4b5563;
    border-radius: 0.5rem;
    background: #1f2937;
    color: inherit;
    padding: 0.5rem 0.65rem;
  }

  .apply-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    gap: 0.5rem;
  }

  .apply-row button,
  .chip,
  .source-list button {
    border: 1px solid #4b5563;
    border-radius: 0.5rem;
    background: #1f2937;
    color: inherit;
    cursor: pointer;
    padding: 0.45rem 0.6rem;
  }

  .chip-list {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
  }

  .chip {
    font-size: 0.8rem;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .source-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .source-list button {
    width: 100%;
    text-align: left;
  }

  .toggle {
    flex-direction: row;
    align-items: center;
    gap: 0.5rem;
  }

  .error {
    margin: 0;
    color: #fca5a5;
    font-size: 0.85rem;
  }

  .current {
    margin: 0;
    color: #cbd5e1;
    font-size: 0.82rem;
    word-break: break-all;
  }

  @media (max-width: 720px) {
    .modal {
      width: min(98vw, 640px);
      max-height: 94vh;
      padding: 0.75rem;
    }

    .apply-row {
      grid-template-columns: 1fr;
    }
  }
</style>
