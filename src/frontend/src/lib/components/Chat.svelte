<script lang="ts">
  import type { Message, Keymods } from '$lib/types/messages';
  import { FragmentType } from '$lib/types/messages';
  import { onDestroy, onMount, setContext } from 'svelte';
  import ChatMessage from './ChatMessage.svelte';
  import PauseOverlay from './PauseOverlay.svelte';

  import { apiPath, hideYouTubeAt, showBadges, wsUrl as configuredWsUrl } from '$lib/config';
  import { connectChat, type ChatMessage as WsChatMessage } from '$lib/chat/ws';
  import { normalizeWsPayload } from '$lib/chat/normalize';
  import { SvelteSet } from 'svelte/reactivity';

  const chatDebug =
    typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('chat_debug') === '1';
  const DEFAULT_COLOUR = '#ffffff';
  const DEFAULT_SOURCE: Message['source'] = 'YouTube';
  const ALLOWED_SOURCES = new Set<Message['source']>(['YouTube', 'Twitch', 'Test', 'youtube', 'twitch']);
  const HISTORY_LIMIT = 200;
  const MESSAGE_LIMIT = HISTORY_LIMIT * 2;
  const QUEUE_LIMIT = 400;
  const WS_CONSTANTS =
    typeof WebSocket !== 'undefined'
      ? WebSocket
      : ({
          CONNECTING: 0,
          OPEN: 1,
          CLOSING: 2,
          CLOSED: 3
        } as const);

  type MessageWithMeta = Message & { id?: string; ts?: number };

  const microtask =
    typeof queueMicrotask === 'function'
      ? queueMicrotask
      : (fn: () => void) => {
          Promise.resolve().then(fn);
        };

  let container: HTMLDivElement;

  let ws: WebSocket | null = $state(null);
  let wsState: number | null = $state(WS_CONSTANTS.CLOSED);
  let lastWsAt: number | null = $state(null);
  let reconnectCount = $state(0);
  let wsReceived = $state(0);
  let enqueued = $state(0);
  let appended = $state(0);
  let trimmed = $state(0);
  let messageQueue: MessageWithMeta[] = $state([]);
  let messages: MessageWithMeta[] = $state([]);
  let renderedDomNodes: number | null = $state(null);
  let hudNow = $state(Date.now());
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

  function convertIncomingMessage(message: WsChatMessage): MessageWithMeta | null {
    if (chatDebug) {
      console.debug('[convertIncomingMessage]', {
        id: (message as any).id,
        ts: (message as any).ts,
        fragments: (message as any).fragments,
        emotes: (message as any).emotes,
        text: (message as any).text,
        platform: (message as any).platform,
        username: (message as any).username
      });
    }

    let author = message.username && message.username.trim().length > 0 ? message.username : 'Unknown';
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
    const id = typeof (message as any).id === 'string' ? (message as any).id : undefined;
    const ts = typeof (message as any).ts === 'number' && Number.isFinite((message as any).ts) ? (message as any).ts : Date.now();

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
    } satisfies MessageWithMeta;
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

  async function fetchRecentMessages() {
    try {
      const params = new URLSearchParams({ limit: HISTORY_LIMIT.toString() });
      const response = await fetch(apiPath(`/api/messages?${params.toString()}`));
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const payload = await response.json();
      const items = Array.isArray(payload?.items) ? payload.items : [];
      const normalizedMessages = items
        .map(normalizeApiMessage)
        .filter((m): m is WsChatMessage => m !== null)
        .map((m) => convertIncomingMessage(m))
        .filter((m): m is MessageWithMeta => m !== null);

      if (chatDebug) {
        console.log('[chat] fetched recent messages', {
          count: normalizedMessages.length,
          sample: normalizedMessages.slice(0, 5).map((m) => m.author)
        });
      }

      if (normalizedMessages.length > 0) {
        const history = normalizedMessages.reverse();
        ingestHistory(history);
        setTimeout(() => {
          container.scrollTop = container.scrollHeight;
        }, 0);
      }
    } catch (err) {
      console.error('[chat] failed to load recent messages', err);
    }
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

  function messageKey(msg: MessageWithMeta): string | null {
    if (typeof msg.id === 'string' && msg.id.trim().length > 0) {
      return msg.id;
    }
    if (typeof msg.ts === 'number' && Number.isFinite(msg.ts)) {
      return `ts-${msg.ts}`;
    }
    return null;
  }

  const seenMessageKeys = new Set<string>();

  function trimMessages() {
    const overflow = messages.length - MESSAGE_LIMIT;
    if (overflow > 0) {
      const removed = messages.slice(0, overflow);
      messages = messages.slice(overflow);
      trimmed += overflow;
      for (const msg of removed) {
        const key = messageKey(msg);
        if (key) seenMessageKeys.delete(key);
      }
    }
  }

  function appendMessage(msg: MessageWithMeta) {
    const key = messageKey(msg);
    if (key && seenMessageKeys.has(key)) {
      if (chatDebug) console.debug('[chat] dedupe skip', key);
      return;
    }
    if (key) seenMessageKeys.add(key);
    messages = [...messages, msg];
    appended += 1;

    if (!paused) {
      setTimeout(() => {
        container.scrollTop = container.scrollHeight;
        newMessageCount = 0;
      }, 0);
    } else {
      newMessageCount = newMessageCount + 1;
    }

    trimMessages();
  }

  function enqueueMessage(msg: MessageWithMeta) {
    enqueued += 1;
    messageQueue = [...messageQueue, msg];
    if (messageQueue.length > QUEUE_LIMIT) {
      const overflow = messageQueue.length - QUEUE_LIMIT;
      messageQueue = messageQueue.slice(-QUEUE_LIMIT);
      trimmed += overflow;
      if (chatDebug) console.warn('[chat] queue overflow trimmed', overflow);
    }

    if (!processing) processMessageQueue();
  }

  function drainQueue() {
    try {
      if (messageQueue.length === 0) {
        return;
      }

      const batch = messageQueue;
      messageQueue = [];

      for (const next of batch) {
        if (!next) continue;
        if (next.colour === '#000000') next.colour = '#CCCCCC';
        appendMessage(next);
      }
    } catch (err) {
      if (chatDebug) console.error('[chat] drain failed', err);
    } finally {
      processing = false;
      if (messageQueue.length > 0) {
        microtask(processMessageQueue);
      }
    }
  }

  function processMessageQueue() {
    if (processing) return;
    processing = true;
    microtask(drainQueue);
  }

  function initializeWebSocket() {
    const wsProtocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const localUrl = `${wsProtocol}://${window.location.host}/ws/chat`;
    const wsUrl = configuredWsUrl || localUrl;

    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      if (chatDebug) console.log('[chat] ws already connected/connecting');
      return;
    }

    if (chatDebug) console.log('[chat] url:', wsUrl);
    ws = connectChat((incoming) => {
      wsReceived += 1;
      lastWsAt = Date.now();

      const normalized = convertIncomingMessage(incoming);
      if (!normalized) return;

      enqueueMessage(normalized);
    }, wsUrl);

    wsState = ws?.readyState ?? null;

    ws.onopen = () => {
      wsState = ws?.readyState ?? null;
      if (chatDebug) console.log('[chat] open');
    };

    ws.onerror = (error) => {
      wsState = ws?.readyState ?? null;
      if (chatDebug) console.error('[chat] error:', error);
    };

    ws.onclose = () => {
      wsState = ws?.readyState ?? null;
      reconnectCount += 1;
      if (chatDebug) console.log('[chat] close');
    };
  }

  onMount(() => {
    fetchRecentMessages();
    initializeWebSocket();
    const hudInterval = chatDebug ? window.setInterval(() => (hudNow = Date.now()), 1000) : null;

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

    return () => {
      if (hudInterval) {
        clearInterval(hudInterval);
      }
    };
  });

  onDestroy(() => {
    if (ws) {
      ws.close();
      ws = null;
    }
  });

  function ingestHistory(history: MessageWithMeta[]) {
    if (history.length === 0) return;
    const deduped = history.filter((msg) => {
      const key = messageKey(msg);
      if (key && seenMessageKeys.has(key)) return false;
      if (key) seenMessageKeys.add(key);
      return true;
    });
    if (deduped.length === 0) return;
    messages = messages.length === 0 ? deduped : [...deduped, ...messages];
    appended += deduped.length;
    trimMessages();
  }

  $effect(() => {
    // Track rendered DOM nodes for HUD; depend on messages length for updates.
    void messages.length;
    renderedDomNodes = container?.childElementCount ?? null;
  });

  const wsStateLabel = () => {
    switch (wsState) {
      case WS_CONSTANTS.CONNECTING:
        return 'CONNECTING';
      case WS_CONSTANTS.OPEN:
        return 'OPEN';
      case WS_CONSTANTS.CLOSING:
        return 'CLOSING';
      case WS_CONSTANTS.CLOSED:
        return 'CLOSED';
      default:
        return 'N/A';
    }
  };
</script>

<div
  id="chat-container"
  aria-label="Chat messages"
  role="list"
  onmouseenter={pauseChat}
  onmouseleave={unpauseChat}
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

{#if chatDebug}
  <div class="chat-debug-hud">
    <div>ws:{wsStateLabel()} rc:{reconnectCount} last:{lastWsAt ? `${hudNow - lastWsAt}ms` : '-'}</div>
    <div>recv:{wsReceived} enq:{enqueued} app:{appended} trim:{trimmed}</div>
    <div>queue:{messageQueue.length} processing:{String(processing)} paused:{String(paused)}</div>
    {#if renderedDomNodes !== null}
      <div>dom:{renderedDomNodes}</div>
    {/if}
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

  .chat-debug-hud {
    position: absolute;
    right: 0.5rem;
    top: 0.5rem;
    display: inline-flex;
    flex-direction: column;
    gap: 2px;
    font: 12px/1.2 monospace;
    background: #000c;
    color: #fff;
    padding: 0.35rem 0.5rem;
    border-radius: 0.5rem;
    z-index: 9999;
    pointer-events: none;
  }
</style>
