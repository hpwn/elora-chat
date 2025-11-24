<script lang="ts">
  import type { Message, Keymods } from '$lib/types/messages';
  import { FragmentType } from '$lib/types/messages';
  import { onMount, setContext } from 'svelte';
  import ChatMessage from './ChatMessage.svelte';
  import PauseOverlay from './PauseOverlay.svelte';

  import { apiPath, hideYouTubeAt, showBadges, wsUrl as configuredWsUrl } from '$lib/config';
  import { connectChat, type ChatMessage as WsChatMessage } from '$lib/chat/ws';
  import { normalizeWsPayload } from '$lib/chat/normalize';
  import { SvelteSet } from 'svelte/reactivity';

  const CHAT_DEBUG = import.meta.env.VITE_CHAT_DEBUG === '1';
  const DEFAULT_COLOUR = '#ffffff';
  const DEFAULT_SOURCE: Message['source'] = 'YouTube';
  const ALLOWED_SOURCES = new Set<Message['source']>(['YouTube', 'Twitch', 'Test', 'youtube', 'twitch']);
  const HISTORY_LIMIT = 200;

  type PlatformCounter = Map<Message['source'] | 'unknown', number>;
  const debugCounters = {
    wsReceived: 0,
    enqueued: 0,
    appended: 0,
    trimmed: 0,
    wsBySource: new Map() as PlatformCounter,
    queueBySource: new Map() as PlatformCounter,
    appendedBySource: new Map() as PlatformCounter,
    trimmedBySource: new Map() as PlatformCounter
  };

  let container: HTMLDivElement;

  let ws: WebSocket | null = $state(null);
  let messageQueue: Message[] = $state([]);
  let messages: Message[] = $state([]);
  let processing = $state(false);
  let paused = $state(false);
  let newMessageCount = $state(0);
  let blacklist = loadBlacklist();
  let keymods: Keymods = {
    ctrl: false,
    shift: false,
    alt: false,
    reset() {
      this.ctrl = false;
      this.shift = false;
      this.alt = false;
    }
  };

  setContext('blacklist', blacklist);
  setContext('keymods', keymods);

  function incrementCounter(map: PlatformCounter, platform: Message['source'] | 'unknown', delta = 1) {
    const current = map.get(platform) ?? 0;
    map.set(platform, current + delta);
  }

  function normalizeSourceValue(raw: unknown): Message['source'] | 'unknown' {
    if (typeof raw !== 'string') return 'unknown';
    const trimmed = raw.trim() as Message['source'];
    return ALLOWED_SOURCES.has(trimmed) ? trimmed : 'unknown';
  }

  function logDebug(stage: string) {
    if (!CHAT_DEBUG) return;
    console.debug('[chat-debug]', stage, {
      wsReceived: debugCounters.wsReceived,
      enqueued: debugCounters.enqueued,
      appended: debugCounters.appended,
      trimmed: debugCounters.trimmed,
      wsBySource: Object.fromEntries(debugCounters.wsBySource),
      queueBySource: Object.fromEntries(debugCounters.queueBySource),
      appendedBySource: Object.fromEntries(debugCounters.appendedBySource),
      trimmedBySource: Object.fromEntries(debugCounters.trimmedBySource)
    });
  }

  interface MessagesResponse {
    items?: any[];
    next_before_ts?: number | null;
    next_before_rowid?: number | null;
  }

  type MessageCursor = {
    beforeTs: number;
    beforeRowID: number | null;
  };

  let nextBeforeTs: number | null = $state(null);
  let nextBeforeRowID: number | null = $state(null);
  let loadingHistory = $state(false);
  let historyExhausted = $state(false);

  function convertIncomingMessage(message: WsChatMessage): Message | null {
    console.debug('[convertIncomingMessage]', {
      fragments: (message as any).fragments,
      emotes: (message as any).emotes,
      text: (message as any).text,
      platform: (message as any).platform,
      username: (message as any).username
    });

    let author = message.username && message.username.trim().length > 0 ? message.username : 'Unknown';
    const tsCandidate = typeof message.ts === 'number' ? message.ts : Number(message.ts);
    const ts = Number.isFinite(tsCandidate) ? tsCandidate : Date.now();

    const idCandidate = typeof message.id === 'string' && message.id.trim().length > 0 ? message.id : '';
    const id = idCandidate || `${ts}-${Math.random().toString(36).slice(2, 8)}`;

    const text = typeof message.text === 'string' ? message.text : '';
    if (
      !text &&
      (!Array.isArray(message.emotes) || message.emotes.length === 0) &&
      (!Array.isArray((message as any).fragments) || (message as any).fragments.length === 0)
    ) {
      return null;
    }

    const rawColour = typeof message.colour === 'string' ? message.colour : '';
    const colour = rawColour && rawColour.trim().length > 0 ? rawColour : DEFAULT_COLOUR;

    const sourceCandidate = typeof message.platform === 'string' ? (message.platform as Message['source']) : DEFAULT_SOURCE;
    const source = ALLOWED_SOURCES.has(sourceCandidate) ? sourceCandidate : DEFAULT_SOURCE;
    if (hideYouTubeAt && source === 'YouTube' && author.startsWith('@')) {
      author = author.slice(1).trim() || author;
    }

    const emotes = coerceEmotes(message.emotes);
    let fragments = Array.isArray((message as any).fragments) ? coerceFragments((message as any).fragments) : [];

    if (fragments.length === 0 && text.trim().length > 0) {
      fragments = [{ type: FragmentType.Text, text, emote: null }];
    }

    const badgeInput = message.displayBadges ?? message.badges;
    const badges = showBadges ? coerceBadges(badgeInput) : [];
    const badges_raw = showBadges ? (message.badges_raw ?? (message as any).badgesRaw ?? null) : null;

    return {
      id,
      ts,
      author,
      message: text,
      colour,
      source,
      fragments,
      emotes,
      badges,
      displayBadges: badges,
      badges_raw
    } satisfies Message;
  }

  function coerceEmotes(emotes: WsChatMessage['emotes']): Message['emotes'] {
    if (!Array.isArray(emotes)) return [];
    const out: Message['emotes'] = [];
    for (const emote of emotes) {
      if (!emote || typeof emote !== 'object') continue;
      const record = emote as Record<string, unknown>;
      const name = typeof record.name === 'string' && record.name.trim().length > 0 ? record.name : undefined;
      if (!name) continue;
      const id = typeof record.id === 'string' && record.id.trim().length > 0 ? record.id : name;
      const imagesRaw = Array.isArray(record.images) ? record.images : [];
      const images = imagesRaw.flatMap((img) => {
        if (!img || typeof img !== 'object') return [] as Message['emotes'][number]['images'];
        const imageRecord = img as Record<string, unknown>;
        const url = typeof imageRecord.url === 'string' ? imageRecord.url : undefined;
        if (!url) return [] as Message['emotes'][number]['images'];
        return [
          {
            id: typeof imageRecord.id === 'string' ? imageRecord.id : `${id}-${url}`,
            url,
            width: typeof imageRecord.width === 'number' ? imageRecord.width : 0,
            height: typeof imageRecord.height === 'number' ? imageRecord.height : 0
          }
        ];
      });

      out.push({
        id,
        name,
        images,
        locations: record.locations ?? []
      });
    }
    return out;
  }

  function safeJsonParse<T>(raw: unknown, fallback: T): T {
    if (typeof raw !== 'string') return fallback;
    try {
      return JSON.parse(raw) as T;
    } catch (err) {
      console.error('[chat] failed to parse json', err);
      return fallback;
    }
  }

  function normalizeApiMessage(item: any): WsChatMessage | null {
    if (!item || typeof item !== 'object') return null;

    const rawPayload = typeof item.raw_json === 'string' ? safeJsonParse<Record<string, unknown> | null>(item.raw_json, null) : null;
    const normalizedFromRaw = rawPayload ? normalizeWsPayload(rawPayload) : null;
    if (normalizedFromRaw) {
      return normalizedFromRaw;
    }

    const text = typeof item.text === 'string' ? item.text : '';
    if (!text.trim()) return null;

    const tsCandidate = typeof item.ts === 'string' ? Date.parse(item.ts) : Number(item.ts);
    const ts = Number.isFinite(tsCandidate) ? tsCandidate : Date.now();

    const idCandidate = typeof item.id === 'string' && item.id.trim().length > 0 ? item.id : `${ts}-${Math.random().toString(36).slice(2, 8)}`;
    const emotes = safeJsonParse<any[]>(item.emotes_json, []);

    return {
      id: idCandidate,
      ts,
      username: typeof item.username === 'string' && item.username.trim().length > 0 ? item.username : '(unknown)',
      platform: typeof item.platform === 'string' && item.platform.trim().length > 0 ? item.platform : DEFAULT_SOURCE,
      text,
      fragments: text ? [{ type: 'text', text, emote: null }] : [],
      emotes,
      badges: [],
      colour: undefined
    } satisfies WsChatMessage;
  }

  function parseCursorValue(value: unknown): number | null {
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string') {
      const parsed = Number(value);
      return Number.isFinite(parsed) ? parsed : null;
    }
    return null;
  }

  function applyHistoryToState(history: Message[], cursor?: MessageCursor) {
    if (history.length === 0) return;

    const ordered = history.reverse();
    const existingIds = new Set(messages.map((m) => m.id ?? ''));
    const mergedHistory = ordered.filter((m) => {
      const id = m.id ?? '';
      if (!id) return true;
      return !existingIds.has(id);
    });

    if (mergedHistory.length === 0) return;

    if (cursor) {
      const prevScrollHeight = container?.scrollHeight ?? 0;
      messages = [...mergedHistory, ...messages];
      // Keep the user's place when prepending older messages.
      requestAnimationFrame(() => {
        if (!container) return;
        const newHeight = container.scrollHeight;
        container.scrollTop = container.scrollTop + (newHeight - prevScrollHeight);
      });
    } else {
      if (messages.length === 0) {
        messages = mergedHistory;
      } else {
        messages = [...mergedHistory, ...messages];
      }
      setTimeout(() => {
        if (container) {
          container.scrollTop = container.scrollHeight;
        }
      }, 0);
    }
  }

  async function fetchMessagesPage(cursor?: MessageCursor) {
    try {
      const params = new URLSearchParams({ limit: HISTORY_LIMIT.toString() });
      if (cursor?.beforeTs != null) {
        params.set('before_ts', cursor.beforeTs.toString());
        if (cursor.beforeRowID != null) {
          params.set('before_rowid', cursor.beforeRowID.toString());
        }
      }

      // Use (ts, rowid) as a stable pagination cursor when available to avoid skipping
      // or repeating messages that share the same timestamp between pages.
      const response = await fetch(apiPath(`/api/messages?${params.toString()}`));
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const payload = (await response.json()) as MessagesResponse;
      const items = Array.isArray(payload?.items) ? payload.items : [];
      const normalizedMessages = items
        .map(normalizeApiMessage)
        .filter((m): m is WsChatMessage => m !== null)
        .map((m) => convertIncomingMessage(m))
        .filter((m): m is Message => m !== null);

      nextBeforeTs = parseCursorValue(payload?.next_before_ts);
      nextBeforeRowID = parseCursorValue(payload?.next_before_rowid);
      historyExhausted = nextBeforeTs === null;

      console.log('[chat] fetched messages page', {
        count: normalizedMessages.length,
        before_ts: cursor?.beforeTs,
        before_rowid: cursor?.beforeRowID,
        next_before_ts: nextBeforeTs,
        next_before_rowid: nextBeforeRowID
      });

      if (normalizedMessages.length > 0) {
        applyHistoryToState(normalizedMessages, cursor);
      }
    } catch (err) {
      console.error('[chat] failed to load messages', err);
    }
  }

  async function loadOlderMessages() {
    if (loadingHistory || historyExhausted || nextBeforeTs === null) return;
    loadingHistory = true;
    await fetchMessagesPage({ beforeTs: nextBeforeTs, beforeRowID: nextBeforeRowID });
    loadingHistory = false;
  }

  function coerceFragments(fragments: any): Message['fragments'] {
    if (!Array.isArray(fragments)) return [];
    const out: Message['fragments'] = [];

    for (const frag of fragments) {
      if (!frag || typeof frag !== 'object') continue;
      const r = frag as Record<string, any>;

      const typeRaw = typeof r.type === 'string' ? r.type.toLowerCase() : 'text';
      let type: FragmentType = FragmentType.Text;
      switch (typeRaw) {
        case 'emote':   type = FragmentType.Emote; break;
        case 'colour':
        case 'color':   type = FragmentType.Colour; break;
        case 'effect':  type = FragmentType.Effect; break;
        case 'pattern': type = FragmentType.Pattern; break;
        // default -> Text
      }

      const text = typeof r.text === 'string' ? r.text : '';

      // If this fragment is an emote, normalize the emote object shape
      let emote: Message['emotes'][number] | null = null;
      if (type === FragmentType.Emote && r.emote && typeof r.emote === 'object') {
        const er = r.emote as Record<string, any>;
        const name = typeof er.name === 'string' && er.name.trim().length > 0 ? er.name : text;
        const id   = typeof er.id === 'string'   && er.id.trim().length > 0   ? er.id   : name;

        const imagesRaw = Array.isArray(er.images) ? er.images : [];
        const images = imagesRaw.flatMap((img) => {
          if (!img || typeof img !== 'object') return [] as Message['emotes'][number]['images'];
          const ir = img as Record<string, any>;
          const url = typeof ir.url === 'string' ? ir.url : undefined;
          if (!url) return [] as Message['emotes'][number]['images'];
          return [{
            id: typeof ir.id === 'string' ? ir.id : `${id}-${url}`,
            url,
            width: typeof ir.width === 'number' ? ir.width : 0,
            height: typeof ir.height === 'number' ? ir.height : 0,
          }];
        });

        emote = { id, name, images, locations: er.locations ?? [] };
      }

      out.push({ type, text, emote });
    }

    return out;
  }


  function coerceBadges(badges: WsChatMessage['badges'] | WsChatMessage['displayBadges']): Message['badges'] {
    if (!Array.isArray(badges)) return [];
    const out: Message['badges'] = [];
    for (const badge of badges) {
      if (typeof badge === 'string') {
        const trimmed = badge.trim();
        if (!trimmed) continue;
        const [idPart, versionPart] = trimmed.split('/', 2);
        const id = idPart.trim();
        if (!id) continue;
        const version = versionPart?.trim();
        out.push(version ? { id, version } : { id });
        continue;
      }
      if (!badge || typeof badge !== 'object') continue;
      const record = badge as Record<string, unknown>;
      const idCandidate = record.id ?? record.name ?? record.title;
      if (typeof idCandidate !== 'string') continue;
      const id = idCandidate.trim();
      if (!id) continue;
      const versionCandidate = record.version ?? record.tier ?? record.slot;
      const version = typeof versionCandidate === 'string' ? versionCandidate.trim() : undefined;
      const platformCandidate = record.platform;
      const platform = typeof platformCandidate === 'string' ? platformCandidate : undefined;
      const imagesRaw = Array.isArray(record.images) ? record.images : [];
      const imageUrl = typeof (record as any).imageUrl === 'string' ? (record as any).imageUrl : undefined;
      const title = typeof (record as any).title === 'string' ? (record as any).title : undefined;
      const images = imagesRaw.flatMap((img) => {
        if (!img || typeof img !== 'object') return [] as Message['badges'][number]['images'];
        const imageRecord = img as Record<string, unknown>;
        const url = typeof imageRecord.url === 'string' ? imageRecord.url : undefined;
        if (!url) return [] as Message['badges'][number]['images'];
        return [
          {
            id: typeof imageRecord.id === 'string' ? imageRecord.id : `${id}-${url}`,
            url,
            width: typeof imageRecord.width === 'number' ? imageRecord.width : 0,
            height: typeof imageRecord.height === 'number' ? imageRecord.height : 0
          }
        ];
      });

      const base =
        version
          ? platform
            ? { id, version, platform }
            : { id, version }
          : platform
            ? { id, platform }
            : { id };

      const badgeWithImages = images.length > 0 ? { ...base, images } : base;
      const withMeta = {
        ...badgeWithImages,
        ...(imageUrl ? { imageUrl } : {}),
        ...(title ? { title } : {})
      };

      out.push(withMeta);
    }
    return out;
  }

  function loadBlacklist(): SvelteSet<string> {
    const list = window.localStorage.getItem('blacklist');
    if (!list) {
      return new SvelteSet();
    }
    const parsedList = JSON.parse(list);
    if (!parsedList) {
      return new SvelteSet();
    }
    return new SvelteSet(parsedList);
  }

  function saveBlacklist() {
    window.localStorage.setItem('blacklist', JSON.stringify([...blacklist]));
  }

  function pauseChat() {
    paused = true;
  }

  function unpauseChat() {
    paused = false;
    setTimeout(() => {
      container.scrollTop = container.scrollHeight;
      newMessageCount = 0;
    }, 0);
  }

  function togglePause() {
    if (paused) {
      unpauseChat();
    } else {
      pauseChat();
    }
  }

  function trimHistory(limit = HISTORY_LIMIT) {
    if (messages.length <= limit) return;
    const overflow = messages.length - limit;
    const trimmed = messages.slice(0, overflow);
    messages = messages.slice(overflow);

    if (CHAT_DEBUG) {
      debugCounters.trimmed += trimmed.length;
      for (const m of trimmed) {
        incrementCounter(debugCounters.trimmedBySource, m.source ?? 'unknown');
      }
      console.warn('[chat] trimmed chat history', {
        overflow,
        trimmedBySource: Object.fromEntries(debugCounters.trimmedBySource),
        remaining: messages.length
      });
      logDebug('trim');
    }
  }

  function processMessageQueue() {
    if (processing) {
      return;
    }

    processing = true;

    let processed = 0;

    while (messageQueue.length > 0) {
      const next = messageQueue.shift();
      if (!next) {
        continue;
      }

      if (next.colour === '#000000') next.colour = '#CCCCCC';

      messages = [...messages, next];
      processed++;

      if (CHAT_DEBUG) {
        debugCounters.appended++;
        incrementCounter(debugCounters.appendedBySource, next.source ?? 'unknown');
      }

      trimHistory();
    }

    if (processed === 0) {
      processing = false;
      return;
    }

    if (CHAT_DEBUG) {
      logDebug('append');
    }

    if (!paused) {
      requestAnimationFrame(() => {
        if (container) {
          container.scrollTop = container.scrollHeight;
        }
        newMessageCount = 0;
      });
    } else {
      newMessageCount = newMessageCount + processed;
    }

    processing = false;

    if (messageQueue.length > 0) {
      setTimeout(processMessageQueue, 0);
    }
  }

  function handleScroll() {
    if (!container) return;
    if (container.scrollTop <= 50) {
      loadOlderMessages();
    }
  }

  function initializeWebSocket() {
    const wsProtocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const localUrl = `${wsProtocol}://${window.location.host}/ws/chat`;
    const wsUrl = configuredWsUrl || localUrl;

    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      if (CHAT_DEBUG) console.log('[chat] ws already connected/connecting');
      return;
    }

    if (CHAT_DEBUG) console.log('[chat] url:', wsUrl);
    ws = connectChat((incoming) => {
      if (CHAT_DEBUG) {
        const platform = normalizeSourceValue((incoming as any)?.platform);
        debugCounters.wsReceived += 1;
        incrementCounter(debugCounters.wsBySource, platform);
      }

      const normalized = convertIncomingMessage(incoming);
      if (!normalized) {
        if (CHAT_DEBUG) {
          logDebug('ws-drop');
        }
        return;
      }

      messageQueue = [...messageQueue, normalized];
      if (CHAT_DEBUG) {
        debugCounters.enqueued += 1;
        incrementCounter(debugCounters.queueBySource, normalized.source ?? 'unknown');
        logDebug('enqueue');
      }
      if (!processing) processMessageQueue();
    }, wsUrl);

    ws.onopen = () => CHAT_DEBUG && console.log('[chat] open');

    ws.onerror = (error) => CHAT_DEBUG && console.error('[chat] error:', error);

    ws.onclose = () => CHAT_DEBUG && console.log('[chat] close');
  }

  onMount(() => {
    fetchMessagesPage();
    initializeWebSocket();

    document.addEventListener('keydown', (e) => {
      switch (e.key) {
        case 'P':
        case 'p':
          togglePause();
          break;
        case 'Control':
          keymods.ctrl = true;
          break;
        case 'Shift':
          keymods.shift = true;
          break;
        case 'Alt':
          keymods.alt = true;
          break;
      }
    });

    document.addEventListener('keyup', (e) => {
      switch (e.key) {
        case 'Control':
          keymods.ctrl = false;
          break;
        case 'Shift':
          keymods.shift = false;
          break;
        case 'Alt':
          keymods.alt = false;
          break;
      }
    });

    document.addEventListener('visibilitychange', () => {
      saveBlacklist();
      keymods.reset();
    });

    window.addEventListener('beforeunload', () => {
      if (ws) {
        ws.close();
        ws = null;
      }
      saveBlacklist();
    });
  });
</script>

<div
  id="chat-container"
  aria-label="Chat messages"
  role="list"
  onmouseenter={pauseChat}
  onmouseleave={unpauseChat}
  onscroll={handleScroll}
  bind:this={container}
>
  {#each messages as message}
    {#if !blacklist.has(message.author)}
      <ChatMessage {message} />
    {/if}
  {/each}
  {#if paused}
    <PauseOverlay {newMessageCount} {unpauseChat} />
  {/if}
</div>

{#if import.meta?.env?.VITE_CHAT_DEBUG}
  <div
    style="position:absolute;right:.5rem;bottom:.5rem;font:12px/1.2 monospace;background:#0008;color:#fff;padding:.25rem .5rem;border-radius:.5rem;z-index:9999"
  >
    msgs:{messages.length} q:{messageQueue.length} pause:{String(paused)}
  </div>
{/if}

<style lang="scss">
  #chat-container {
    display: flex;
    flex-direction: column;
    flex: 1;
    position: relative;
    padding: 0 10px;

    overflow-y: auto;
    scrollbar-width: none;
  }
</style>
