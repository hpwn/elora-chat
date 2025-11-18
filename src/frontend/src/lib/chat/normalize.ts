// Normalizes incoming websocket payloads into a single shape the UI can render safely.
// - Ignores keepalives ("__keepalive__")
// - Tolerates both harvester (author/message/...) and tailer (username/text/...) shapes
// - Accepts emotes/badges from either array fields or JSON-string fields
// - Coerces ts to ms epoch (handles seconds and ISO-8601 text)
// - Drops completely empty messages
export type Emote = { id?: string; name?: string; images?: any[]; [k: string]: any };
export type Badge = { id: string; version?: string | null; platform?: 'YouTube' | 'Twitch' | 'youtube' | 'twitch' | string };

export interface ChatMessage {
  id: string;
  ts: number; // ms since epoch
  username: string;
  platform: 'YouTube' | 'Twitch' | 'youtube' | 'twitch' | 'Test' | string;
  text: string;
  emotes: Emote[];
  badges: Badge[];
  badges_raw?: unknown;
  fragments?: any[];
  colour?: string;
  raw?: unknown;
}

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
    out.push(
      version
        ? platform
          ? { id, version, platform }
          : { id, version }
        : platform
          ? { id, platform }
          : { id }
    );
  }
  return out;
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
