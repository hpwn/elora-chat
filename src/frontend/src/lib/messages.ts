import type { Message } from '$lib/types/messages';

type MaybeEnvelope<T> = T | { items: T[] } | T[];

const KEEPALIVE_TOKENS = new Set(['__keepalive__', 'ping', 'pong']);

function normalizePayload<T>(payload: MaybeEnvelope<T>): T[] {
  if (Array.isArray(payload)) {
    return payload;
  }

  if (payload && typeof payload === 'object' && 'items' in payload) {
    const items = (payload as { items?: unknown }).items;
    if (Array.isArray(items)) {
      return items as T[];
    }
  }

  if (payload && typeof payload === 'object') {
    return [payload as T];
  }

  return [];
}

function coerceMessage(raw: unknown): Message | null {
  if (!raw || typeof raw !== 'object') {
    return null;
  }

  const candidate = raw as Record<string, unknown>;
  const author = typeof candidate.author === 'string' ? candidate.author : 'Unknown';
  const message = typeof candidate.message === 'string' ? candidate.message : '';
  const colour = typeof candidate.colour === 'string' ? candidate.colour : '#ffffff';
  const badges = Array.isArray(candidate.badges) ? (candidate.badges as Message['badges']) : [];
  const fragments = Array.isArray(candidate.fragments)
    ? (candidate.fragments as Message['fragments'])
    : [];
  const emotes = Array.isArray(candidate.emotes) ? (candidate.emotes as Message['emotes']) : [];
  const source = candidate.source === 'Twitch' || candidate.source === 'YouTube' ? candidate.source : 'YouTube';

  return {
    author,
    badges,
    colour,
    message,
    fragments,
    emotes,
    source
  } as Message;
}

export function parseWsMessages(eventData: unknown): Message[] {
  if (typeof eventData === 'string') {
    if (KEEPALIVE_TOKENS.has(eventData)) {
      return [];
    }

    try {
      const parsed = JSON.parse(eventData) as MaybeEnvelope<unknown>;
      return normalizePayload(parsed)
        .map((item) => coerceMessage(item))
        .filter((item): item is Message => item !== null);
    } catch (error) {
      console.warn('Unable to parse websocket payload', error, eventData);
      return [];
    }
  }

  if (eventData instanceof ArrayBuffer) {
    try {
      const decoded = new TextDecoder().decode(eventData);
      return parseWsMessages(decoded);
    } catch (error) {
      console.warn('Unable to decode websocket ArrayBuffer payload', error);
      return [];
    }
  }

  if (KEEPALIVE_TOKENS.has(String(eventData))) {
    return [];
  }

  return normalizePayload(eventData as MaybeEnvelope<unknown>)
    .map((item) => coerceMessage(item))
    .filter((item): item is Message => item !== null);
}
