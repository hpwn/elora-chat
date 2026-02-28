import { browser } from '$app/environment';
import { writable } from 'svelte/store';
import {
  ALL_ALERT_TYPES,
  DEFAULT_ALERT_PREFERENCES,
  type AlertPreferences,
  type AlertType
} from '$lib/alerts/preferences';

export type Settings = {
  apiBaseUrl: string;
  wsUrl: string;
  showBadges: boolean;
  hideYouTubeAt: boolean;
  pauseChatEnabled: boolean;
  pauseOnMouseLeave: boolean;
  pauseBottomThresholdPx: number;
  pauseScrollIntentWindowMs: number;
  pauseMouseleaveUnpauseCooldownMs: number;
  fetchHistoryOnLoad: boolean;
  chatDebug: boolean;
  settingsDebug: boolean;
  twitchUrl: string;
  youtubeUrl: string;
  recentTwitch: string[];
  recentYouTube: string[];
  alertPreferences: AlertPreferences;
};

const KEY = 'elora.settings.v2';
export const SETTINGS_STORAGE_KEY = KEY;
const MAX_RECENT_DEFAULT = 10;

export const DEFAULT_SETTINGS: Settings = {
  apiBaseUrl: '',
  wsUrl: '',
  showBadges: true,
  hideYouTubeAt: true,
  pauseChatEnabled: true,
  pauseOnMouseLeave: true,
  pauseBottomThresholdPx: 128,
  pauseScrollIntentWindowMs: 2000,
  pauseMouseleaveUnpauseCooldownMs: 0,
  fetchHistoryOnLoad: false,
  chatDebug: false,
  settingsDebug: false,
  twitchUrl: '',
  youtubeUrl: '',
  recentTwitch: [],
  recentYouTube: [],
  alertPreferences: { ...DEFAULT_ALERT_PREFERENCES, byChannel: {} }
};

function readString(value: unknown, fallback = ''): string {
  return typeof value === 'string' ? value.trim() : fallback;
}

function readBoolean(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback;
}

function readNumber(value: unknown, fallback: number, min = 0): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) return fallback;
  return Math.max(min, Math.floor(value));
}

function readRecent(value: unknown, max = MAX_RECENT_DEFAULT): string[] {
  if (!Array.isArray(value)) return [];
  const out: string[] = [];
  const seen = new Set<string>();
  for (const item of value) {
    if (typeof item !== 'string') continue;
    const trimmed = item.trim();
    if (!trimmed) continue;
    const key = trimmed.toLowerCase();
    if (seen.has(key)) continue;
    out.push(trimmed);
    seen.add(key);
    if (out.length >= max) break;
  }
  return out;
}

function readAlertPreferences(value: unknown): AlertPreferences {
  if (!value || typeof value !== 'object') {
    return { ...DEFAULT_ALERT_PREFERENCES, byChannel: {} };
  }

  const record = value as Record<string, unknown>;
  const enabled = typeof record.enabled === 'boolean' ? record.enabled : DEFAULT_ALERT_PREFERENCES.enabled;
  const byChannelRaw = record.byChannel;
  const byChannel: Record<string, Partial<Record<AlertType, boolean>>> = {};

  if (byChannelRaw && typeof byChannelRaw === 'object') {
    for (const [channelKey, channelValue] of Object.entries(byChannelRaw as Record<string, unknown>)) {
      if (typeof channelKey !== 'string') continue;
      if (!channelValue || typeof channelValue !== 'object') continue;
      const nextState: Partial<Record<AlertType, boolean>> = {};
      for (const [typeKey, typeValue] of Object.entries(channelValue as Record<string, unknown>)) {
        if (!ALL_ALERT_TYPES.includes(typeKey as AlertType)) continue;
        if (typeof typeValue !== 'boolean') continue;
        nextState[typeKey as AlertType] = typeValue;
      }
      if (Object.keys(nextState).length > 0) {
        byChannel[channelKey] = nextState;
      }
    }
  }

  return { enabled, byChannel };
}

export function pushRecent(list: string[], value: string, max = MAX_RECENT_DEFAULT): string[] {
  const candidate = value.trim();
  if (!candidate) return list.slice(0, max);

  const out: string[] = [candidate];
  const seen = new Set<string>([candidate.toLowerCase()]);

  for (const item of list) {
    const trimmed = item.trim();
    if (!trimmed) continue;
    const key = trimmed.toLowerCase();
    if (seen.has(key)) continue;
    out.push(trimmed);
    seen.add(key);
    if (out.length >= max) break;
  }

  return out;
}

function loadSettings(): Settings {
  if (!browser) {
    return { ...DEFAULT_SETTINGS };
  }

  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) {
      return { ...DEFAULT_SETTINGS };
    }

    const parsed = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) {
      return { ...DEFAULT_SETTINGS };
    }

    const partial = parsed as Partial<Settings>;

    return {
      apiBaseUrl: readString(partial.apiBaseUrl),
      wsUrl: readString(partial.wsUrl),
      showBadges: readBoolean(partial.showBadges, DEFAULT_SETTINGS.showBadges),
      hideYouTubeAt: readBoolean(partial.hideYouTubeAt, DEFAULT_SETTINGS.hideYouTubeAt),
      pauseChatEnabled: readBoolean(partial.pauseChatEnabled, DEFAULT_SETTINGS.pauseChatEnabled),
      pauseOnMouseLeave: readBoolean(partial.pauseOnMouseLeave, DEFAULT_SETTINGS.pauseOnMouseLeave),
      pauseBottomThresholdPx: readNumber(partial.pauseBottomThresholdPx, DEFAULT_SETTINGS.pauseBottomThresholdPx, 0),
      pauseScrollIntentWindowMs: readNumber(
        partial.pauseScrollIntentWindowMs,
        DEFAULT_SETTINGS.pauseScrollIntentWindowMs,
        0
      ),
      pauseMouseleaveUnpauseCooldownMs: readNumber(
        partial.pauseMouseleaveUnpauseCooldownMs,
        DEFAULT_SETTINGS.pauseMouseleaveUnpauseCooldownMs,
        0
      ),
      fetchHistoryOnLoad: readBoolean(partial.fetchHistoryOnLoad, DEFAULT_SETTINGS.fetchHistoryOnLoad),
      chatDebug: readBoolean(partial.chatDebug, DEFAULT_SETTINGS.chatDebug),
      settingsDebug: readBoolean(partial.settingsDebug, DEFAULT_SETTINGS.settingsDebug),
      twitchUrl: readString(partial.twitchUrl),
      youtubeUrl: readString(partial.youtubeUrl),
      recentTwitch: readRecent(partial.recentTwitch),
      recentYouTube: readRecent(partial.recentYouTube),
      alertPreferences: readAlertPreferences(partial.alertPreferences)
    };
  } catch (error) {
    console.warn('Failed to load settings from storage', error);
    return { ...DEFAULT_SETTINGS };
  }
}

export const settings = writable<Settings>({ ...DEFAULT_SETTINGS });

if (browser) {
  let hadExisting = false;
  try {
    hadExisting = localStorage.getItem(KEY) !== null;
  } catch {
    hadExisting = false;
  }
  settings.set(loadSettings());

  let firstWrite = true;
  settings.subscribe((value) => {
    if (firstWrite) {
      firstWrite = false;
      if (!hadExisting) {
        return;
      }
    }
    try {
      localStorage.setItem(KEY, JSON.stringify(value));
    } catch (error) {
      console.warn('Failed to persist settings', error);
    }
  });
}
