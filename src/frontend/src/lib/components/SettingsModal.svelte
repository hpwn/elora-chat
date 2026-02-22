<script lang="ts">
  import { tick } from 'svelte';
  import ExportPanel from './ExportPanel.svelte';
  import { apiPath } from '$lib/config';
  import { pushRecent, settings, type Settings } from '$lib/stores/settings';
  import type { RuntimeConfig, RuntimeConfigResponse } from '$lib/types/runtime-config';

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
  let configError = '';
  let configLoading = false;
  let wasOpen = false;
  let backendConfig: RuntimeConfig | null = null;
  let twitchTopList: TopTwitchItem[] = [];
  let youtubeTopList: TopYouTubeItem[] = [];
  let allowedOriginsDraft = '';
  let tailerPollDraft = 0;
  let tailerBatchDraft = 0;
  let tailerLagDraft = 0;
  let tailerPersistDraft = false;
  let tailerOffsetDraft = '';
  let wsPingDraft = 0;
  let wsPongDraft = 0;
  let wsWriteDeadlineDraft = 0;
  let wsMaxMessageDraft = 0;
  let gnastySinkEnabledDraft = '';
  let gnastySinkBatchDraft = 0;
  let gnastySinkFlushDraft = 0;
  let gnastyTwitchNickDraft = '';
  let gnastyTwitchTLSDraft = true;
  let gnastyTwitchDebugDropsDraft = false;
  let gnastyTwitchBackoffMinDraft = 0;
  let gnastyTwitchBackoffMaxDraft = 0;
  let gnastyTwitchRefreshBackoffMinDraft = 0;
  let gnastyTwitchRefreshBackoffMaxDraft = 0;
  let gnastyYTRetryDraft = 0;
  let gnastyYTDumpUnhandledDraft = false;
  let gnastyYTPollTimeoutDraft = 0;
  let gnastyYTPollIntervalDraft = 0;
  let gnastyYTDebugDraft = false;

  $: if (open && !wasOpen) {
    wasOpen = true;
    tick().then(async () => {
      dialog?.focus();
      await loadBackendConfig();
      syncDraftsFromSettings();
    });
  }

  $: if (!open && wasOpen) {
    wasOpen = false;
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

  async function applyConnectionSettings() {
    const apiBaseUrl = apiBaseDraft.trim();
    const wsUrl = wsUrlDraft.trim();
    await applyBackendConfig((current) => ({
      ...current,
      apiBaseUrl,
      wsUrl
    }));
  }

  async function applyTwitch(value?: string) {
    const input = (value ?? twitchDraft).trim();
    if (!input) return;
    const updated = await applyBackendConfig((current) => ({
      ...current,
      twitchChannel: input
    }));
    if (updated?.twitchChannel) {
      const twitchUrl = toTwitchURL(updated.twitchChannel);
      updateSettings({ recentTwitch: pushRecent($settings.recentTwitch, twitchUrl) });
    }
  }

  async function applyYouTube(value?: string) {
    const input = (value ?? youtubeDraft).trim();
    if (!input) return;
    const updated = await applyBackendConfig((current) => ({
      ...current,
      youtubeSourceUrl: input
    }));
    if (updated?.youtubeSourceUrl) {
      updateSettings({ recentYouTube: pushRecent($settings.recentYouTube, updated.youtubeSourceUrl) });
    }
  }

  function toTwitchURL(channel: string): string {
    const trimmed = channel.trim();
    if (!trimmed) return '';
    return `https://www.twitch.tv/${trimmed}`;
  }

  function syncSettingsFromBackend(config: RuntimeConfig) {
    const twitchUrl = toTwitchURL(config.twitchChannel);
    const youtubeUrl = config.youtubeSourceUrl;
    updateSettings({
      apiBaseUrl: config.apiBaseUrl,
      wsUrl: config.wsUrl,
      twitchUrl,
      youtubeUrl,
      showBadges: config.features.showBadges,
      hideYouTubeAt: config.features.hideYouTubeAt,
      recentTwitch: twitchUrl ? pushRecent($settings.recentTwitch, twitchUrl) : $settings.recentTwitch,
      recentYouTube: youtubeUrl ? pushRecent($settings.recentYouTube, youtubeUrl) : $settings.recentYouTube
    });
    syncAdvancedDrafts(config);
    syncDraftsFromSettings();
  }

  function syncAdvancedDrafts(config: RuntimeConfig) {
    allowedOriginsDraft = (config.allowedOrigins || []).join(', ');
    tailerPollDraft = config.tailer.pollIntervalMs;
    tailerBatchDraft = config.tailer.maxBatch;
    tailerLagDraft = config.tailer.maxLagMs;
    tailerPersistDraft = config.tailer.persistOffsets;
    tailerOffsetDraft = config.tailer.offsetPath;
    wsPingDraft = config.websocket.pingIntervalMs;
    wsPongDraft = config.websocket.pongWaitMs;
    wsWriteDeadlineDraft = config.websocket.writeDeadlineMs;
    wsMaxMessageDraft = config.websocket.maxMessageBytes;
    gnastySinkEnabledDraft = (config.gnasty.sinks.enabled || []).join(', ');
    gnastySinkBatchDraft = config.gnasty.sinks.batchSize;
    gnastySinkFlushDraft = config.gnasty.sinks.flushMaxMs;
    gnastyTwitchNickDraft = config.gnasty.twitch.nick;
    gnastyTwitchTLSDraft = config.gnasty.twitch.tls;
    gnastyTwitchDebugDropsDraft = config.gnasty.twitch.debugDrops;
    gnastyTwitchBackoffMinDraft = config.gnasty.twitch.backoffMinMs;
    gnastyTwitchBackoffMaxDraft = config.gnasty.twitch.backoffMaxMs;
    gnastyTwitchRefreshBackoffMinDraft = config.gnasty.twitch.refreshBackoffMinMs;
    gnastyTwitchRefreshBackoffMaxDraft = config.gnasty.twitch.refreshBackoffMaxMs;
    gnastyYTRetryDraft = config.gnasty.youtube.retrySeconds;
    gnastyYTDumpUnhandledDraft = config.gnasty.youtube.dumpUnhandled;
    gnastyYTPollTimeoutDraft = config.gnasty.youtube.pollTimeoutSecs;
    gnastyYTPollIntervalDraft = config.gnasty.youtube.pollIntervalMs;
    gnastyYTDebugDraft = config.gnasty.youtube.debug;
  }

  function splitCSVList(value: string): string[] {
    return value
      .split(',')
      .map((item) => item.trim())
      .filter((item) => item.length > 0);
  }

  async function applyAdvancedSettings() {
    await applyBackendConfig((current) => ({
      ...current,
      allowedOrigins: splitCSVList(allowedOriginsDraft),
      tailer: {
        ...current.tailer,
        pollIntervalMs: tailerPollDraft,
        maxBatch: tailerBatchDraft,
        maxLagMs: tailerLagDraft,
        persistOffsets: tailerPersistDraft,
        offsetPath: tailerOffsetDraft
      },
      websocket: {
        ...current.websocket,
        pingIntervalMs: wsPingDraft,
        pongWaitMs: wsPongDraft,
        writeDeadlineMs: wsWriteDeadlineDraft,
        maxMessageBytes: wsMaxMessageDraft
      },
      gnasty: {
        sinks: {
          enabled: splitCSVList(gnastySinkEnabledDraft),
          batchSize: gnastySinkBatchDraft,
          flushMaxMs: gnastySinkFlushDraft
        },
        twitch: {
          nick: gnastyTwitchNickDraft.trim(),
          tls: gnastyTwitchTLSDraft,
          debugDrops: gnastyTwitchDebugDropsDraft,
          backoffMinMs: gnastyTwitchBackoffMinDraft,
          backoffMaxMs: gnastyTwitchBackoffMaxDraft,
          refreshBackoffMinMs: gnastyTwitchRefreshBackoffMinDraft,
          refreshBackoffMaxMs: gnastyTwitchRefreshBackoffMaxDraft
        },
        youtube: {
          retrySeconds: gnastyYTRetryDraft,
          dumpUnhandled: gnastyYTDumpUnhandledDraft,
          pollTimeoutSecs: gnastyYTPollTimeoutDraft,
          pollIntervalMs: gnastyYTPollIntervalDraft,
          debug: gnastyYTDebugDraft
        }
      }
    }));
  }

  async function loadBackendConfig(): Promise<RuntimeConfig | null> {
    configError = '';
    configLoading = true;
    try {
      const response = await fetch(apiPath('/api/config'));
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const payload = (await response.json()) as RuntimeConfigResponse;
      backendConfig = payload.config;
      syncSettingsFromBackend(payload.config);
      return payload.config;
    } catch (error) {
      configError = error instanceof Error ? error.message : 'Failed to load runtime config';
      return null;
    } finally {
      configLoading = false;
    }
  }

  async function saveBackendConfig(next: RuntimeConfig): Promise<RuntimeConfig | null> {
    configError = '';
    try {
      const putPayload = 'config' in (next as RuntimeConfig | RuntimeConfigResponse) ? (next as RuntimeConfigResponse).config : next;
      const response = await fetch(apiPath('/api/config'), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(putPayload)
      });
      const payload = (await response.json().catch(() => null)) as RuntimeConfigResponse | { error?: string; message?: string } | null;
      if (!response.ok) {
        const fallback = `HTTP ${response.status}`;
        const message = payload && typeof payload === 'object' ? payload.message || payload.error || fallback : fallback;
        throw new Error(message);
      }
      if (!payload || typeof payload !== 'object' || !('config' in payload)) {
        throw new Error('Runtime config response missing config payload');
      }
      const updated = (payload as RuntimeConfigResponse).config;
      backendConfig = updated;
      syncSettingsFromBackend(updated);
      return updated;
    } catch (error) {
      configError = error instanceof Error ? error.message : 'Failed to save runtime config';
      return null;
    }
  }

  async function applyBackendConfig(mutator: (current: RuntimeConfig) => RuntimeConfig): Promise<RuntimeConfig | null> {
    const current = backendConfig ?? (await loadBackendConfig());
    if (!current) return null;
    return saveBackendConfig(mutator(current));
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
      {#if configLoading}
        <p class="current">Loading runtime config...</p>
      {/if}
      {#if configError}
        <p class="error">{configError}</p>
      {/if}
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
          <h3>Sources</h3>
          <div class="apply-row">
            <input
              type="text"
              bind:value={twitchDraft}
              on:blur={() => applyTwitch()}
              placeholder="channel or https://www.twitch.tv/..."
            />
            <button type="button" on:click={() => applyTwitch()}>Apply Twitch</button>
          </div>
          <div class="apply-row">
            <input
              type="text"
              bind:value={youtubeDraft}
              on:blur={() => applyYouTube()}
              placeholder="video id, handle, or https://www.youtube.com/..."
            />
            <button type="button" on:click={() => applyYouTube()}>Apply YouTube</button>
          </div>
        </section>

        <section class="section">
          <h3>Tailer</h3>
          <label>
            Poll interval (ms)
            <input type="number" min="25" bind:value={tailerPollDraft} />
          </label>
          <label>
            Max batch
            <input type="number" min="1" bind:value={tailerBatchDraft} />
          </label>
          <label>
            Max lag (ms)
            <input type="number" min="0" bind:value={tailerLagDraft} />
          </label>
          <label class="toggle">
            <input type="checkbox" bind:checked={tailerPersistDraft} />
            Persist offsets
          </label>
          <label>
            Offset path
            <input type="text" bind:value={tailerOffsetDraft} placeholder="/data/gnasty.db.offset.json" />
          </label>
        </section>

        <section class="section">
          <h3>WebSocket</h3>
          <label>
            Allowed origins (comma-separated)
            <input type="text" bind:value={allowedOriginsDraft} placeholder="http://localhost:5173" />
          </label>
          <label>
            Ping interval (ms)
            <input type="number" min="500" bind:value={wsPingDraft} />
          </label>
          <label>
            Pong wait (ms)
            <input type="number" min="500" bind:value={wsPongDraft} />
          </label>
          <label>
            Write deadline (ms)
            <input type="number" min="0" bind:value={wsWriteDeadlineDraft} />
          </label>
          <label>
            Max message bytes
            <input type="number" min="1024" bind:value={wsMaxMessageDraft} />
          </label>
        </section>

        <section class="section">
          <h3>Gnasty Advanced</h3>
          <label>
            Sinks enabled (comma-separated)
            <input type="text" bind:value={gnastySinkEnabledDraft} placeholder="sqlite" />
          </label>
          <label>
            Sink batch size
            <input type="number" min="1" bind:value={gnastySinkBatchDraft} />
          </label>
          <label>
            Sink flush max (ms)
            <input type="number" min="0" bind:value={gnastySinkFlushDraft} />
          </label>
          <label>
            Twitch nick
            <input type="text" bind:value={gnastyTwitchNickDraft} placeholder="elora_bot" />
          </label>
          <label class="toggle">
            <input type="checkbox" bind:checked={gnastyTwitchTLSDraft} />
            Twitch TLS
          </label>
          <label class="toggle">
            <input type="checkbox" bind:checked={gnastyTwitchDebugDropsDraft} />
            Twitch debug drops
          </label>
          <label>
            Twitch backoff min (ms)
            <input type="number" min="1" bind:value={gnastyTwitchBackoffMinDraft} />
          </label>
          <label>
            Twitch backoff max (ms)
            <input type="number" min="1" bind:value={gnastyTwitchBackoffMaxDraft} />
          </label>
          <label>
            Twitch refresh backoff min (ms)
            <input type="number" min="1" bind:value={gnastyTwitchRefreshBackoffMinDraft} />
          </label>
          <label>
            Twitch refresh backoff max (ms)
            <input type="number" min="1" bind:value={gnastyTwitchRefreshBackoffMaxDraft} />
          </label>
          <label>
            YouTube retry/backoff seconds
            <input type="number" min="1" bind:value={gnastyYTRetryDraft} />
          </label>
          <label class="toggle">
            <input type="checkbox" bind:checked={gnastyYTDumpUnhandledDraft} />
            YouTube dump unhandled
          </label>
          <label>
            YouTube poll timeout (secs)
            <input type="number" min="0" bind:value={gnastyYTPollTimeoutDraft} />
          </label>
          <label>
            YouTube poll interval (ms)
            <input type="number" min="0" bind:value={gnastyYTPollIntervalDraft} />
          </label>
          <label class="toggle">
            <input type="checkbox" bind:checked={gnastyYTDebugDraft} />
            YouTube debug
          </label>
          <button type="button" on:click={applyAdvancedSettings}>Apply advanced runtime config</button>
        </section>

        <section class="section">
          <h3>Feature Flags</h3>
          <label class="toggle">
            <input
              checked={$settings.showBadges}
              on:change={async (event) => {
                const checked = (event.currentTarget as HTMLInputElement).checked;
                await applyBackendConfig((current) => ({
                  ...current,
                  features: { ...current.features, showBadges: checked }
                }));
              }}
              type="checkbox"
            />
            Show badges
          </label>
          <label class="toggle">
            <input
              checked={$settings.hideYouTubeAt}
              on:change={async (event) => {
                const checked = (event.currentTarget as HTMLInputElement).checked;
                await applyBackendConfig((current) => ({
                  ...current,
                  features: { ...current.features, hideYouTubeAt: checked }
                }));
              }}
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
