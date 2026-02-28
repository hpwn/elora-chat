export const TWITCH_ALERT_TYPES = ['subs', 'gifted_subs', 'bits', 'raids'] as const;
export const YOUTUBE_ALERT_TYPES = ['members', 'super_chats', 'gifted_members'] as const;

export type TwitchAlertType = (typeof TWITCH_ALERT_TYPES)[number];
export type YouTubeAlertType = (typeof YOUTUBE_ALERT_TYPES)[number];
export type AlertType = TwitchAlertType | YouTubeAlertType;
export type AlertPlatform = 'twitch' | 'youtube';

export type AlertPreferences = {
  enabled: boolean;
  byChannel: Record<string, Partial<Record<AlertType, boolean>>>;
};

export const ALL_ALERT_TYPES: readonly AlertType[] = [...TWITCH_ALERT_TYPES, ...YOUTUBE_ALERT_TYPES];

export const ALERT_TYPES_BY_PLATFORM: Record<AlertPlatform, readonly AlertType[]> = {
  twitch: TWITCH_ALERT_TYPES,
  youtube: YOUTUBE_ALERT_TYPES
};

export const DEFAULT_ALERT_PREFERENCES: AlertPreferences = {
  enabled: true,
  byChannel: {}
};

export function buildAlertPreferenceKey(platform: AlertPlatform, identity: string): string {
  return `${platform}:${identity}`;
}

export function normalizeTwitchChannelIdentity(raw: unknown): string | null {
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

export function normalizeYouTubeSourceIdentity(raw: unknown): string | null {
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

export function normalizeAlertType(raw: unknown): AlertType | null {
  if (typeof raw !== 'string') return null;
  const lowered = raw.trim().toLowerCase();
  if (!lowered) return null;

  const stripped =
    lowered.startsWith('twitch.') || lowered.startsWith('youtube.') ? lowered.split('.', 2)[1] : lowered;

  return ALL_ALERT_TYPES.includes(stripped as AlertType) ? (stripped as AlertType) : null;
}

