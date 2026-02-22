<script lang="ts">
  import type { Message, Keymods } from '$lib/types/messages';
  import { FragmentType } from '$lib/types/messages';
  import { onMount, setContext } from 'svelte';
  import ChatMessage from './ChatMessage.svelte';
  import PauseOverlay from './PauseOverlay.svelte';

  import { apiPath, getHideYouTubeAt, getShowBadges, getWsUrl, isChatDebugEnabled, isFetchHistoryOnLoad } from '$lib/config';
  import { connectChat, type ChatMessage as WsChatMessage } from '$lib/chat/ws';
  import { normalizeWsPayload } from '$lib/chat/normalize';
  import { settings, type Settings } from '$lib/stores/settings';
  import { SvelteSet } from 'svelte/reactivity';
  import * as EmojilibNS from 'emojilib';

  let chatDebug = $state(isChatDebugEnabled());
  const DEFAULT_COLOUR = '#ffffff';
  const DEFAULT_SOURCE: Message['source'] = 'YouTube';
  const ALLOWED_SOURCES = new Set<Message['source']>(['YouTube', 'Twitch', 'Test', 'youtube', 'twitch']);
  const HISTORY_PAGE_LIMIT = 200;
  const HISTORY_TRIM_LIMIT: number | null = null;
  const SHORTCODE_REGEX = /:([a-z0-9_\-+]+):/gi;
  const shortcodeToEmoji = new Map<string, string>();
  const shortcodeAliases = new Map<string, string>([
    ['rolling_on_the_floor_laughing', 'rofl'],
    ['face_with_tears_of_joy', 'joy'],
    ['grinning_squinting_face', 'laughing']
  ]);
  const ytShortcodeOverrides = new Map<string, string>([
    ['grinning_squinting_face', '😆']
  ]);
  const keywordSet = (EmojilibNS as any).default ?? (EmojilibNS as any);

  if (keywordSet && typeof keywordSet === 'object') {
    for (const [emojiChar, keywords] of Object.entries(keywordSet)) {
      if (!emojiChar) continue;
      if (!Array.isArray(keywords)) continue;
      for (const keyword of keywords) {
        if (typeof keyword === 'string' && keyword) {
          shortcodeToEmoji.set(keyword, emojiChar);
        }
      }
    }
  }

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
  let currentWsUrl = $state('');
  let settingsDebug = $state(false);
  let pendingTwitchChannel = $state<string | null>(null);
  let pendingYouTubeSource = $state<string | null>(null);
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

  function sourceLower(source: Message['source'] | 'unknown'): string {
    return typeof source === 'string' ? source.toLowerCase() : '';
  }

  function normalizeTwitchChannelIdentity(raw: unknown): string | null {
    if (typeof raw !== 'string') return null;
    const trimmed = raw.trim();
    if (!trimmed) return null;
    try {
      const parsed = new URL(trimmed.includes('://') ? trimmed : `https://${trimmed}`);
      if (!parsed.hostname.toLowerCase().includes('twitch.tv')) return null;
      const login = parsed.pathname.split('/').filter(Boolean)[0] ?? '';
      return login ? login.toLowerCase() : null;
    } catch {
      const login = trimmed.replace(/^@/, '').replace(/^\/+/, '').split(/[/?#]/)[0] ?? '';
      if (!login) return null;
      return /^[a-z0-9_]+$/i.test(login) ? login.toLowerCase() : null;
    }
  }

  function normalizeYouTubeSourceIdentity(raw: unknown): string | null {
    if (typeof raw !== 'string') return null;
    const trimmed = raw.trim();
    if (!trimmed) return null;
    if (/^[a-zA-Z0-9_-]{11}$/.test(trimmed)) {
      return `https://www.youtube.com/watch?v=${trimmed}`;
    }

    try {
      const parsed = new URL(trimmed.includes('://') ? trimmed : `https://${trimmed}`);
      const hostname = parsed.hostname.toLowerCase();
      if (hostname === 'youtu.be') {
        const id = parsed.pathname.split('/').filter(Boolean)[0] ?? '';
        return /^[a-zA-Z0-9_-]{11}$/.test(id) ? `https://www.youtube.com/watch?v=${id}` : null;
      }
      if (!hostname.includes('youtube.com')) return null;

      const path = parsed.pathname.replace(/\/+$/, '');
      if (path.startsWith('/@')) {
        const handle = path.split('/').filter(Boolean)[0]?.slice(1) ?? '';
        return handle ? `https://www.youtube.com/@${handle}/live` : null;
      }
      const id = parsed.searchParams.get('v') ?? '';
      return /^[a-zA-Z0-9_-]{11}$/.test(id) ? `https://www.youtube.com/watch?v=${id}` : null;
    } catch {
      const handle = trimmed.replace(/^@/, '');
      if (/^[a-zA-Z0-9._-]+$/.test(handle)) {
        return `https://www.youtube.com/@${handle}/live`;
      }
      return null;
    }
  }

  function systemMessage(text: string): Message {
    const ts = Date.now();
    return {
      id: `system:${ts}-${Math.random().toString(36).slice(2, 8)}`,
      ts,
      author: 'system',
      badges: [],
      displayBadges: [],
      badges_raw: null,
      colour: '#94a3b8',
      usernameColor: '#94a3b8',
      message: text,
      fragments: [{ type: FragmentType.Text, text, emote: null }],
      emotes: [],
      source: 'Test'
    };
  }

  function emitSystem(text: string): void {
    if (!settingsDebug) return;
    messageQueue = [...messageQueue, systemMessage(text)];
    if (!processing) processMessageQueue();
  }

  function logDebug(stage: string) {
    if (!chatDebug) return;
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

  function shortcodesToUnicode(text: string): string {
    if (!text) return text;
    return text.replace(SHORTCODE_REGEX, (match, token) => {
      const rawKey = String(token).toLowerCase();
      const override = ytShortcodeOverrides.get(rawKey);
      if (override) return override;

      const normalized = rawKey.replace(/-/g, '_');
      const key = shortcodeAliases.get(normalized) ?? normalized;
      const mapped = shortcodeToEmoji.get(key);
      return typeof mapped === 'string' && mapped.length > 0 ? mapped : match;
    });
  }

  function pickEmoteUrl(emote: unknown): string | null {
    if (!emote || typeof emote !== 'object') return null;
    const record = emote as Record<string, any>;
    const pickString = (value: unknown): string | null => {
      if (typeof value !== 'string') return null;
      const trimmed = value.trim();
      return trimmed ? trimmed : null;
    };

    const directUrl = pickString(record.url);
    if (directUrl) return directUrl;

    const urls = record.urls;
    if (urls && typeof urls === 'object') {
      const preferredKeys = ['4x', '3x', '2x', '1x', '4', '3', '2', '1', 'large', 'medium', 'small'];
      for (const key of preferredKeys) {
        const candidate = pickString((urls as Record<string, unknown>)[key]);
        if (candidate) return candidate;
      }
      for (const value of Object.values(urls as Record<string, unknown>)) {
        const candidate = pickString(value);
        if (candidate) return candidate;
      }
    }

    const images = Array.isArray(record.images) ? record.images : [];
    for (const img of images) {
      if (!img || typeof img !== 'object') continue;
      const url = pickString((img as Record<string, unknown>).url ?? (img as Record<string, unknown>).src);
      if (url) return url;
    }

    const image = record.image;
    if (image && typeof image === 'object') {
      const url = pickString((image as Record<string, unknown>).url ?? (image as Record<string, unknown>).src);
      if (url) return url;
    }

    return null;
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
  const BOTTOM_THRESHOLD_PX = 128;
  const USER_SCROLL_INTENT_WINDOW_MS = 2000;
  const MOUSELEAVE_UNPAUSE_COOLDOWN_MS = 2000;
  let lastUserScrollTs = $state(0);
  let lastScrollTop = $state(0);
  let programmaticScrollTs = $state(0);

  function convertIncomingMessage(message: WsChatMessage): Message | null {
    if (chatDebug) {
      console.debug('[convertIncomingMessage]', {
        fragments: (message as any).fragments,
        emotes: (message as any).emotes,
        text: (message as any).text,
        platform: (message as any).platform,
        username: (message as any).username
      });
    }
    let author = message.username && message.username.trim().length > 0 ? message.username : 'Unknown';
    const tsCandidate = typeof message.ts === 'number' ? message.ts : Number(message.ts);
    const ts = Number.isFinite(tsCandidate) ? tsCandidate : Date.now();

    const sourceCandidate = typeof message.platform === 'string' ? (message.platform as Message['source']) : DEFAULT_SOURCE;
    const source = ALLOWED_SOURCES.has(sourceCandidate) ? sourceCandidate : DEFAULT_SOURCE;
    const idCandidate = typeof message.id === 'string' && message.id.trim().length > 0 ? message.id.trim() : '';
    const rawId = idCandidate || `${ts}-${Math.random().toString(36).slice(2, 8)}`;
    const id = rawId.startsWith(source + ':') ? rawId : `${source}:${rawId}`;

    const text = typeof message.text === 'string' ? message.text : '';
    if (
      !text &&
      (!Array.isArray(message.emotes) || message.emotes.length === 0) &&
      (!Array.isArray((message as any).fragments) || (message as any).fragments.length === 0)
    ) {
      return null;
    }

    const rawUsernameColor =
      typeof (message as any).usernameColor === 'string'
        ? (message as any).usernameColor
        : typeof (message as any).username_color === 'string'
          ? (message as any).username_color
          : '';
    const rawColour = typeof message.colour === 'string' ? message.colour : '';
    const colourCandidate = rawUsernameColor && rawUsernameColor.trim().length > 0 ? rawUsernameColor : rawColour;
    const colour = colourCandidate && colourCandidate.trim().length > 0 ? colourCandidate : DEFAULT_COLOUR;

    if (getHideYouTubeAt() && source === 'YouTube' && author.startsWith('@')) {
      author = author.slice(1).trim() || author;
    }

    const emotes = coerceEmotes(message.emotes);
    let fragments = Array.isArray((message as any).fragments) ? coerceFragments((message as any).fragments, source) : [];

    if (fragments.length === 0 && text.trim().length > 0) {
      const normalizedText = source === 'YouTube' ? shortcodesToUnicode(text) : text;
      fragments = [{ type: FragmentType.Text, text: normalizedText, emote: null }];
    }

    const badgeInput = message.displayBadges ?? message.badges;
    const showBadges = getShowBadges();
    const badges = showBadges ? coerceBadges(badgeInput) : [];
    const badges_raw = showBadges ? (message.badges_raw ?? (message as any).badgesRaw ?? null) : null;

    return {
        id,
      ts,
      author,
      message: text,
      colour,
      usernameColor: colour,
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

    const platformCandidate = typeof item.platform === 'string' ? item.platform.trim() : '';
    const platform = platformCandidate ? platformCandidate : DEFAULT_SOURCE;
    const source = ALLOWED_SOURCES.has(platform as any) ? (platform as any) : DEFAULT_SOURCE;
    const idCandidate = typeof item.id === 'string' && item.id.trim().length > 0 ? item.id.trim() : `${ts}-${Math.random().toString(36).slice(2, 8)}`;
    const id = idCandidate.startsWith(source + ':') ? idCandidate : `${source}:${idCandidate}`;
    const emotes = safeJsonParse<any[]>(item.emotes_json, []);

    return {
        id,
      ts,
      username: typeof item.username === 'string' && item.username.trim().length > 0 ? item.username : '(unknown)',
        platform: source,
      text,
      fragments: text ? [{ type: 'text', text, emote: null }] : [],
      emotes,
      badges: [],
      colour: undefined,
      usernameColor: undefined
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
      const params = new URLSearchParams({ limit: HISTORY_PAGE_LIMIT.toString() });
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
    if (!isFetchHistoryOnLoad()) return;
    if (loadingHistory || historyExhausted || nextBeforeTs === null) return;
    loadingHistory = true;
    await fetchMessagesPage({ beforeTs: nextBeforeTs, beforeRowID: nextBeforeRowID });
    loadingHistory = false;
  }

  function coerceFragments(fragments: any, source: Message['source']): Message['fragments'] {
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
      if (r.emote && typeof r.emote === 'object') {
        const er = r.emote as Record<string, any>;
        const pickedUrl = pickEmoteUrl(er);
        const hasName = typeof er.name === 'string' && er.name.trim().length > 0;
        const hasId = typeof er.id === 'string' && er.id.trim().length > 0;
        const imagesRaw = Array.isArray(er.images) ? er.images : [];
        const locationsRaw = Array.isArray(er.locations) ? er.locations : [];

        if (!pickedUrl) {
          emote = null;
          if (type === FragmentType.Emote) {
            type = FragmentType.Text;
          }
        } else if (type === FragmentType.Emote) {
          const name = hasName ? er.name : text;
          const id = hasId ? er.id : name;
          let images = imagesRaw.flatMap((img) => {
            if (!img || typeof img !== 'object') return [] as Message['emotes'][number]['images'];
            const ir = img as Record<string, any>;
            const url =
              typeof ir.url === 'string' ? ir.url : typeof ir.src === 'string' ? ir.src : undefined;
            if (!url) return [] as Message['emotes'][number]['images'];
            return [{
              id: typeof ir.id === 'string' ? ir.id : `${id}-${url}`,
              url,
              width: typeof ir.width === 'number' ? ir.width : 0,
              height: typeof ir.height === 'number' ? ir.height : 0,
            }];
          });

          if (pickedUrl && !images.some((img) => img.url === pickedUrl)) {
            images = [{ id: `${id}-${pickedUrl}`, url: pickedUrl, width: 0, height: 0 }, ...images];
          }

          emote = { id, name, images, locations: locationsRaw, ...(pickedUrl ? { url: pickedUrl } : {}) };
        }
      }

      const normalizedText =
        source === 'YouTube' && type === FragmentType.Text && emote === null ? shortcodesToUnicode(text) : text;

      out.push({ type, text: normalizedText, emote });
    }

    return out;
  }


  function coerceBadges(badges: WsChatMessage['badges'] | WsChatMessage['displayBadges']): Message['badges'] {
    if (!Array.isArray(badges)) return [];
    const out: Message['badges'] = [];
    type BadgeImage = NonNullable<Message['badges'][number]['images']>[number];
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
      const images: BadgeImage[] = imagesRaw.flatMap((img) => {
        if (!img || typeof img !== 'object') return [] as BadgeImage[];
        const imageRecord = img as Record<string, unknown>;
        const url = typeof imageRecord.url === 'string' ? imageRecord.url : undefined;
        if (!url) return [] as BadgeImage[];
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
      if (!container) return;
      programmaticScrollTs = Date.now();
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

  function trimHistory(limit = HISTORY_TRIM_LIMIT) {
    if (limit === null) return;
    if (messages.length <= limit) return;
    const overflow = messages.length - limit;
    const trimmed = messages.slice(0, overflow);
    messages = messages.slice(overflow);

    if (chatDebug) {
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

      if (chatDebug) {
        debugCounters.appended++;
        incrementCounter(debugCounters.appendedBySource, next.source ?? 'unknown');
      }

      trimHistory();
    }

    if (processed === 0) {
      processing = false;
      return;
    }

    if (chatDebug) {
      logDebug('append');
    }

    if (!paused) {
      requestAnimationFrame(() => {
        if (container) {
          programmaticScrollTs = Date.now();
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

    const now = Date.now();
    const previousScrollTop = lastScrollTop;
    lastScrollTop = container.scrollTop;
    const distanceFromBottom = container.scrollHeight - (container.scrollTop + container.clientHeight);
    const isAtBottom = distanceFromBottom <= BOTTOM_THRESHOLD_PX;
    if (isAtBottom) {
      if (paused) {
        unpauseChat();
      }
    } else if (
      container.scrollTop < previousScrollTop &&
      now - lastUserScrollTs < USER_SCROLL_INTENT_WINDOW_MS) {
      paused = true;
    }

    if (isFetchHistoryOnLoad() && container.scrollTop <= 50) {
      loadOlderMessages();
    }
  }

  function initializeWebSocket() {
    const wsUrl = getWsUrl();

    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      if (chatDebug) console.log('[chat] ws already connected/connecting');
      return;
    }

    currentWsUrl = wsUrl;
    if (chatDebug) console.log('[chat] url:', wsUrl);
    emitSystem(`ws: reconnecting -> ${wsUrl}`);
    ws = connectChat((incoming) => {
      if (chatDebug) {
        const platform = normalizeSourceValue((incoming as any)?.platform);
        debugCounters.wsReceived += 1;
        incrementCounter(debugCounters.wsBySource, platform);
      }

      const normalized = convertIncomingMessage(incoming);
      if (!normalized) {
        if (chatDebug) {
          logDebug('ws-drop');
        }
        return;
      }

      const normalizedSource = sourceLower(normalized.source);
      const twitchIdentity = normalizeTwitchChannelIdentity((incoming as any)?.sourceChannel);
      if (pendingTwitchChannel !== null && normalizedSource === 'twitch' && twitchIdentity === pendingTwitchChannel) {
        emitSystem(`twitch: receiving messages [ok] channel=${pendingTwitchChannel}`);
        pendingTwitchChannel = null;
      }
      const youtubeIdentity = normalizeYouTubeSourceIdentity((incoming as any)?.sourceUrl);
      if (pendingYouTubeSource !== null && normalizedSource === 'youtube' && youtubeIdentity === pendingYouTubeSource) {
        emitSystem(`youtube: receiving messages [ok] source=${pendingYouTubeSource}`);
        pendingYouTubeSource = null;
      }

      messageQueue = [...messageQueue, normalized];
      if (chatDebug) {
        debugCounters.enqueued += 1;
        incrementCounter(debugCounters.queueBySource, normalized.source ?? 'unknown');
        logDebug('enqueue');
      }
      if (!processing) processMessageQueue();
    }, wsUrl);

    ws.onopen = () => {
      emitSystem('ws: connected');
      chatDebug && console.log('[chat] open');
    };

    ws.onerror = (error) => chatDebug && console.error('[chat] error:', error);

    ws.onclose = (event) => {
      emitSystem(`ws: closed code=${event.code} reason=${event.reason || 'n/a'}`);
      chatDebug && console.log('[chat] close');
    };
  }

  function markUserScrollIntent() {
    lastUserScrollTs = Date.now();
  }

  function handleMouseLeave() {
      if (!paused) return;
      unpauseChat();
    }

  function handleKeyDown(e: KeyboardEvent) {
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
  }

  function handleKeyUp(e: KeyboardEvent) {
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
  }

  function handleVisibilityChange() {
    saveBlacklist();
    keymods.reset();
    if (document.visibilityState === 'visible' && !paused) {
      programmaticScrollTs = Date.now();
      requestAnimationFrame(() => {
        if (!container) return;
        container.scrollTop = container.scrollHeight;
      });
    }
  }

  onMount(() => {
    let lastSettings: Settings | null = null;

    const unsubscribeSettings = settings.subscribe((nextSettings) => {
      chatDebug = isChatDebugEnabled();
      const wasSettingsDebug = settingsDebug;
      settingsDebug = !!nextSettings.settingsDebug;

      if (lastSettings) {
        const apiChanged = lastSettings.apiBaseUrl !== nextSettings.apiBaseUrl;
        const wsChanged = lastSettings.wsUrl !== nextSettings.wsUrl;
        const twitchChanged = lastSettings.twitchUrl !== nextSettings.twitchUrl;
        const youtubeChanged = lastSettings.youtubeUrl !== nextSettings.youtubeUrl;
        const normalizedTwitch = normalizeTwitchChannelIdentity(nextSettings.twitchUrl);
        const normalizedYouTube = normalizeYouTubeSourceIdentity(nextSettings.youtubeUrl);

        if ((apiChanged || wsChanged || twitchChanged || youtubeChanged) && settingsDebug) {
          emitSystem(
            `settings: applied api=${nextSettings.apiBaseUrl || '(default)'} ws=${nextSettings.wsUrl || '(derived)'} tw=${normalizedTwitch || '(unset)'} yt=${normalizedYouTube || '(unset)'}`
          );
        }

        if (!wasSettingsDebug && settingsDebug) {
          emitSystem('settings: debug enabled');
        }

        if (twitchChanged && settingsDebug) {
          pendingTwitchChannel = normalizedTwitch;
          if (pendingTwitchChannel) {
            emitSystem(`twitch: channel set -> ${pendingTwitchChannel} (waiting for first matching message)`);
          }
        }

        if (youtubeChanged && settingsDebug) {
          pendingYouTubeSource = normalizedYouTube;
          if (pendingYouTubeSource) {
            emitSystem(`youtube: source set -> ${pendingYouTubeSource} (waiting for first matching message)`);
          }
        }
      }

      const nextWsUrl = getWsUrl();
      const shouldReconnect = !!nextWsUrl && nextWsUrl !== currentWsUrl;
      if (shouldReconnect) {
        if (ws) {
          ws.close();
          ws = null;
        }
        initializeWebSocket();
      }

      lastSettings = { ...nextSettings };
    });

    if (isFetchHistoryOnLoad()) {
      fetchMessagesPage();
    } else {
      historyExhausted = true;
    }
    initializeWebSocket();

    document.addEventListener('keydown', handleKeyDown);
    document.addEventListener('keyup', handleKeyUp);

    document.addEventListener('visibilitychange', handleVisibilityChange);

    window.addEventListener('beforeunload', () => {
      if (ws) {
        ws.close();
        ws = null;
      }
      saveBlacklist();
    });

    return () => {
      unsubscribeSettings();
      if (ws) {
        ws.close();
        ws = null;
      }
      document.removeEventListener('keydown', handleKeyDown);
      document.removeEventListener('keyup', handleKeyUp);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  });
</script>

<div class="chat-shell">
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    id="chat-container"
    class:paused={paused}
    aria-label="Chat messages"
    role="list"
    onmouseleave={handleMouseLeave}
    onwheel={markUserScrollIntent}
    ontouchstart={markUserScrollIntent}
    onpointerdown={markUserScrollIntent}
    onscroll={handleScroll}
    bind:this={container}
  
      onmousedown={markUserScrollIntent}
    
      onauxclick={markUserScrollIntent}
    >
    {#each messages as message (message.id)}
      {#if !blacklist.has(message.author)}
        <ChatMessage {message} />
      {/if}
    {/each}
  </div>
  {#if paused}
    <div class="pause-overlay">
      <PauseOverlay {newMessageCount} {unpauseChat} />
    </div>
  {/if}
</div>

{#if chatDebug}
  <div
    style="position:absolute;right:.5rem;bottom:.5rem;font:12px/1.2 monospace;background:#0008;color:#fff;padding:.25rem .5rem;border-radius:.5rem;z-index:9999"
  >
    msgs:{messages.length} q:{messageQueue.length} pause:{String(paused)}
  </div>
{/if}

<style lang="scss">
  .chat-shell {
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1;
    height: 100%;
    min-height: 0;
  }

  #chat-container {
    --pauseOverlayH: 72px;
    display: flex;
    flex-direction: column;
    flex: 1;
    padding: 0 10px;
    min-height: 0;

    overflow-y: auto;
    overflow-anchor: none;
    scrollbar-gutter: stable;
    scrollbar-width: none;
  }

  #chat-container.paused {
    padding-bottom: var(--pauseOverlayH);
  }

  .pause-overlay {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 8px;
    padding: 0 10px 10px;
    display: flex;
    justify-content: center;
    z-index: 2;
  }
</style>


