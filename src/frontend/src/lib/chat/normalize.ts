// Normalizes incoming websocket payloads into a single shape the UI can render safely.
// - Ignores keepalives ("__keepalive__")
// - Tolerates both harvester (author/message/...) and tailer (username/text/...) shapes
// - Accepts emotes/badges from either array fields or JSON-string fields
// - Coerces ts to ms epoch (handles seconds and ISO-8601 text)
// - Drops completely empty messages
export type Emote = { id?: string; name?: string; images?: any[]; [k: string]: any };
export type BadgeImage = { url: string; width?: number; height?: number; id?: string };
export type Badge = {
  id: string;
  version?: string | null;
  platform?: 'YouTube' | 'Twitch' | 'youtube' | 'twitch' | string;
  images?: BadgeImage[];
  imageUrl?: string;
  title?: string;
};

export type DisplayBadge = Badge;

export interface ChatMessage {
  id: string;
  ts: number; // ms since epoch
  username: string;
  platform: 'YouTube' | 'Twitch' | 'youtube' | 'twitch' | 'Test' | string;
  text: string;
  emotes: Emote[];
  badges: Badge[];
  badges_raw?: unknown;
  displayBadges?: DisplayBadge[];
  fragments?: any[];
  colour?: string;
  raw?: unknown;
}

const YOUTUBE_MODERATOR_ICON = '/images/youtube-moderator.svg';

export const KEEPALIVE = '__keepalive__';

export function normalizeWsPayload(evtData: unknown): ChatMessage | null {
  const [first] = normalizeWsPayloads(evtData);
  return first ?? null;
}

export function normalizeWsPayloads(evtData: unknown): ChatMessage[] {
  if (evtData == null) return [];

  if (typeof evtData === 'string') {
    return normalizeFromString(evtData);
  }

  if (Array.isArray(evtData)) {
    return evtData.flatMap((entry) => normalizeWsPayloads(entry));
  }

  if (typeof evtData === 'object') {
    const maybeEnvelope = evtData as Record<string, unknown>;
    if (maybeEnvelope && maybeEnvelope.type === 'chat' && 'data' in maybeEnvelope) {
      return normalizeWsPayloads(maybeEnvelope.data);
    }
    const normalized = normalizeObject(maybeEnvelope);
    return normalized ? [normalized] : [];
  }

  return [];
}

function normalizeFromString(raw: string): ChatMessage[] {
  if (!raw) return [];
  const trimmed = raw.trim();
  if (!trimmed) return [];
  if (trimmed === KEEPALIVE) return [];

  // Try JSON parse first (object/array/envelope)
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    const parsed = safeJson<unknown>(trimmed, null);
    if (parsed != null) {
      return normalizeWsPayloads(parsed);
    }
  }

  // NDJSON fallback
  if (trimmed.includes('\n')) {
    const lines = trimmed.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
    const out: ChatMessage[] = [];
    for (const line of lines) {
      if (!line || line === KEEPALIVE) continue;
      const parsed = safeJson<unknown>(line, null);
      if (parsed != null) {
        out.push(...normalizeWsPayloads(parsed));
        continue;
      }
      // Last resort: treat as single tokenised object string
      if (line.startsWith('{') || line.startsWith('[')) continue;
    }
    if (out.length > 0) return out;
  }

  return [];
}

function normalizeObject(obj: Record<string, unknown> | null | undefined): ChatMessage | null {
  if (!obj) return null;

  if (obj.type === 'chat' && 'data' in obj) {
    return normalizeWsPayload(obj.data);
  }

  const id = String(obj.id ?? obj.message_id ?? cryptoRandom());

  const usernameRaw = obj.author ?? obj.username ?? obj.name ?? '(unknown)';
  const username = typeof usernameRaw === 'string' && usernameRaw.trim() ? usernameRaw : '(unknown)';

  const platformRaw = obj.source ?? obj.platform ?? obj.service ?? 'Unknown';
  const platform = typeof platformRaw === 'string' && platformRaw.trim() ? platformRaw : 'Unknown';

  const textRaw = obj.message ?? obj.text ?? obj.body ?? '';
  const text = typeof textRaw === 'string' ? textRaw : '';

  const colourRaw = (obj.colour ?? obj.color) as unknown;
  const colour = typeof colourRaw === 'string' && colourRaw.trim() ? colourRaw : undefined;

  const emotes = coerceArray(obj.emotes, obj.emotes_json);
  const fragments = coerceArray(
    (obj as any).fragments,
    (obj as any).fragments_json ?? (obj as any).tokens ?? (obj as any).tokens_json
  );
  const badgesRaw = coerceArray(obj.badges, obj.badges_json);
  const badges = normalizeBadges(badgesRaw);
  const badgesRawPayload = coerceRawBadges((obj as any).badges_raw ?? (obj as any).badgesRaw ?? null);
  const displayBadges = buildDisplayBadges(badges, badgesRawPayload);

  const ts = coerceTimestamp(obj.ts ?? obj.timestamp ?? obj.created_at ?? obj.time ?? null);

  if (!text && emotes.length === 0 && (!Array.isArray(fragments) || fragments.length === 0)) {
    return null;
  }

  const raw = typeof obj.raw_json === 'string' ? safeJson(obj.raw_json, obj) : obj.raw ?? obj;

  return {
    id,
    ts,
    username,
    platform,
    text,
    fragments,
    emotes,
    badges,
    badges_raw: badgesRawPayload,
    displayBadges,
    colour,
    raw
  } satisfies ChatMessage;
}

function normalizeBadges(badges: unknown[]): Badge[] {
  if (!Array.isArray(badges)) return [];
  const out: Badge[] = [];
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
    const idRaw = record.id ?? record.badge_id ?? record.name ?? record.title;
    if (typeof idRaw !== 'string') continue;
    const id = idRaw.trim();
    if (!id) continue;
    const versionRaw = record.version ?? record.badgeVersion ?? record.tier ?? record.slot;
    const version = typeof versionRaw === 'string' ? versionRaw.trim() : undefined;
    const platform = typeof record.platform === 'string' ? record.platform : undefined;
    const imagesRaw = Array.isArray(record.images) ? record.images : [];
    const images: BadgeImage[] = imagesRaw.flatMap((img) => {
      if (!img || typeof img !== 'object') return [] as BadgeImage[];
      const imageRecord = img as Record<string, any>;
      const url = typeof imageRecord.url === 'string' && imageRecord.url.trim().length > 0 ? imageRecord.url : undefined;
      if (!url) return [] as BadgeImage[];
      return [
        {
          url,
          id: typeof imageRecord.id === 'string' ? imageRecord.id : undefined,
          width: typeof imageRecord.width === 'number' ? imageRecord.width : undefined,
          height: typeof imageRecord.height === 'number' ? imageRecord.height : undefined
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

    out.push(images.length > 0 ? { ...base, images } : base);
  }
  return out;
}

function buildDisplayBadges(badges: Badge[], badgesRaw: unknown): DisplayBadge[] {
  const youtubeRenderers = extractYoutubeBadgeRenderers(badgesRaw);
  let youtubeIndex = 0;

  return badges.map((badge) => {
    const baseImages = Array.isArray(badge.images) ? [...badge.images] : [];
    const renderer = isYoutubePlatform(badge.platform) ? youtubeRenderers[youtubeIndex++] : undefined;

    let imageUrl = badge.imageUrl ?? baseImages.at(-1)?.url;
    let title = badge.title;

    if (renderer) {
      if (renderer.thumbnail) {
        const thumbnail = renderer.thumbnail;
        imageUrl = thumbnail.url ?? imageUrl;

        if (thumbnail.url) {
          const hasThumb = baseImages.some((img) => img.url === thumbnail.url);
          if (!hasThumb) {
            baseImages.push({ url: thumbnail.url, width: thumbnail.width, height: thumbnail.height });
          }
        }
      } else if (renderer.iconType === 'MODERATOR') {
        imageUrl = YOUTUBE_MODERATOR_ICON;
      }

      if (!title && renderer.tooltip) {
        title = renderer.tooltip;
      }
    }

    const display: DisplayBadge = { ...badge };
    if (baseImages.length > 0) {
      display.images = baseImages;
    }
    if (imageUrl) {
      display.imageUrl = imageUrl;
    }
    if (title) {
      display.title = title;
    }
    return display;
  });
}

type YoutubeRenderer = {
  thumbnail?: BadgeImage;
  tooltip?: string;
  iconType?: string;
};

function extractYoutubeBadgeRenderers(raw: unknown): YoutubeRenderer[] {
  const youtube = raw && typeof raw === 'object' ? (raw as Record<string, unknown>).youtube : undefined;
  if (!youtube || typeof youtube !== 'object') return [];

  const authorBadgesRaw = (youtube as Record<string, unknown>).authorBadges;
  const authorBadges = Array.isArray(authorBadgesRaw) ? authorBadgesRaw : [];

  return authorBadges.flatMap((entry) => {
    if (!entry || typeof entry !== 'object') return [] as YoutubeRenderer[];
    const renderer = (entry as Record<string, any>).liveChatAuthorBadgeRenderer;
    if (!renderer || typeof renderer !== 'object') return [] as YoutubeRenderer[];

    const thumbnails = renderer.customThumbnail?.thumbnails;
    let bestThumb: BadgeImage | undefined;
    if (Array.isArray(thumbnails)) {
      for (const thumb of thumbnails) {
        if (!thumb || typeof thumb !== 'object') continue;
        const url = typeof thumb.url === 'string' ? thumb.url : undefined;
        const width = typeof thumb.width === 'number' ? thumb.width : undefined;
        const height = typeof thumb.height === 'number' ? thumb.height : undefined;
        if (!url) continue;
        if (!bestThumb || (width ?? 0) > (bestThumb.width ?? 0)) {
          bestThumb = { url, width, height };
        }
      }
    }

    const tooltip = typeof renderer.tooltip === 'string' && renderer.tooltip.trim().length > 0 ? renderer.tooltip : undefined;
    const iconType = typeof renderer.icon?.iconType === 'string' ? renderer.icon.iconType : undefined;

    return [
      {
        thumbnail: bestThumb,
        tooltip,
        iconType
      }
    ];
  });
}

function isYoutubePlatform(platform: Badge['platform']): boolean {
  return typeof platform === 'string' && platform.toLowerCase() === 'youtube';
}

function coerceRawBadges(input: unknown): unknown {
  if (typeof input === 'string') {
    const parsed = safeJson<unknown | undefined>(input, undefined);
    if (parsed !== undefined) return parsed;
    return undefined;
  }
  if (input && typeof input === 'object') {
    return input;
  }
  return undefined;
}

function coerceArray(primary: unknown, fallbackJson: unknown): any[] {
  if (Array.isArray(primary)) {
    return primary as any[];
  }
  if (typeof primary === 'string') {
    const parsed = safeJson<any[]>(primary, []);
    if (Array.isArray(parsed)) return parsed;
  }
  if (typeof fallbackJson === 'string') {
    const parsed = safeJson<any[]>(fallbackJson, []);
    if (Array.isArray(parsed)) return parsed;
  }
  if (Array.isArray(fallbackJson)) {
    return fallbackJson as any[];
  }
  return [];
}

function coerceTimestamp(input: unknown): number {
  let tsNum: number | null = null;

  if (typeof input === 'number' && Number.isFinite(input)) {
    tsNum = input;
  } else if (typeof input === 'string') {
    const numeric = Number(input);
    if (Number.isFinite(numeric)) {
      tsNum = numeric;
    } else {
      const parsed = Date.parse(input);
      if (!Number.isNaN(parsed)) {
        tsNum = parsed;
      }
    }
  }

  if (tsNum == null) {
    tsNum = Date.now();
  }

  if (tsNum < 1_000_000_000_000) {
    tsNum *= 1000;
  }

  return tsNum;
}

function safeJson<T>(value: string, fallback: T): T;
function safeJson<T>(value: unknown, fallback: T): T;
function safeJson<T>(value: unknown, fallback: T): T {
  if (typeof value !== 'string') return fallback;
  try {
    return JSON.parse(value) as T;
  } catch {
    return fallback;
  }
}

function cryptoRandom(): string {
  if (typeof globalThis.crypto !== 'undefined' && 'randomUUID' in globalThis.crypto) {
    return globalThis.crypto.randomUUID();
  }
  return `msg-${Math.random().toString(36).slice(2)}`;
}

declare global {
  interface Window {
    parseWsEvent?: (x: unknown) => ChatMessage | null;
  }
}

if (typeof window !== 'undefined') {
  window.parseWsEvent = normalizeWsPayload;
}

/** Coerce a websocket "emote-like" object into the UI Emote shape or null */
function normalizeEmote(input: any): Emote | null {
  if (!input || typeof input !== 'object') return null;
  const name = typeof input.name === 'string' ? input.name : '';
  const imagesArr = Array.isArray(input.images) ? input.images : [];
  const images = imagesArr.flatMap((img: any) => {
    if (!img || typeof img !== 'object') return [];
    const url = typeof img.url === 'string' ? img.url : undefined;
    if (!url) return [];
    return [{
      url,
      width: typeof img.width === 'number' ? img.width : 0,
      height: typeof img.height === 'number' ? img.height : 0,
      id: typeof img.id === 'string' ? img.id : `${name || 'emote'}-${url}`
    }];
  });

  return {
    id: typeof input.id === 'string' ? input.id : (name || ''),
    name,
    images: images.length > 0 ? images : [],
    // Keep locations if present; otherwise empty
    locations: Array.isArray(input.locations) ? input.locations as string[] : []
  };
}

/** Map string/number fragment types from the socket to the enum the renderer expects */
function mapFragmentType(t: any): FragmentType | null {
  if (typeof t === 'number') {
    // Already an enum value (best effort sanity check)
    if (t in FragmentType) return t as FragmentType;
  }
  if (typeof t === 'string') {
    const k = t.toLowerCase();
    switch (k) {
      case 'text':   return FragmentType.Text;
      case 'emote':  return FragmentType.Emote;
      case 'color':
      case 'colour': return FragmentType.Colour;
      case 'effect': return FragmentType.Effect;
      case 'pattern':return FragmentType.Pattern;
    }
  }
  return null;
}

/** Turn raw websocket fragments into UI-ready Fragment[] (enum types, sanitized text, emote objects) */
function normalizeFragments(input: any): Fragment[] {
  const arr = Array.isArray(input) ? input : [];
  const out: Fragment[] = [];
  for (const f of arr) {
    if (!f || typeof f !== 'object') continue;
    const t = mapFragmentType((f as any).type);
    if (t == null) continue;

    const text = typeof (f as any).text === 'string' ? (f as any).text : '';
    const emoteObj = normalizeEmote((f as any).emote);

    // Enforce the interface expected by formatMessageFragments()
    out.push({
      type: t,
      text,
      emote: t === FragmentType.Emote ? (emoteObj ?? null) : null
    } as Fragment);
  }
  return out;
}
