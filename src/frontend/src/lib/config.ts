import { get } from 'svelte/store';
import { browser } from '$app/environment';
import { settings, SETTINGS_STORAGE_KEY } from '$lib/stores/settings';

const defaultOrigin = typeof window !== 'undefined' && window.location?.origin ? window.location.origin : 'http://localhost:8080';
const fetchHistoryTruthyValues = new Set(['1', 'true', 'yes', 'on']);

function envBool(name: string, fallback: boolean): boolean {
  const raw = (import.meta.env[name] ?? '').toString().trim().toLowerCase();
  if (!raw) return fallback;
  return raw !== '0' && raw !== 'false' && raw !== 'no' && raw !== 'off';
}

function trimTrailingSlash(raw: string): string {
  return raw.replace(/\/+$/, '');
}

function hasPersistedSettings(): boolean {
  if (!browser) return false;
  try {
    return !!localStorage.getItem(SETTINGS_STORAGE_KEY);
  } catch {
    return false;
  }
}

export function getApiBaseUrl(): string {
  const envApiBase = (import.meta.env.VITE_PUBLIC_API_BASE ?? '').toString().trim();
  const configured = get(settings).apiBaseUrl.trim();
  const resolved = configured || envApiBase || defaultOrigin;
  return trimTrailingSlash(resolved) || defaultOrigin;
}

export function deriveWsUrl(apiBaseUrl: string): string {
  try {
    const parsed = new URL(apiBaseUrl);
    const scheme = parsed.protocol === 'https:' ? 'wss' : 'ws';
    return `${scheme}://${parsed.host}/ws/chat`;
  } catch {
    return 'ws://localhost:8080/ws/chat';
  }
}

export function getWsUrl(): string {
  const envWsUrl = (import.meta.env.VITE_PUBLIC_WS_URL ?? '').toString().trim();
  const configured = get(settings).wsUrl.trim();
  if (configured) return configured;
  if (envWsUrl) return envWsUrl;
  return deriveWsUrl(getApiBaseUrl());
}

export function getHideYouTubeAt(): boolean {
  if (hasPersistedSettings()) {
    const configured = get(settings).hideYouTubeAt;
    if (typeof configured === 'boolean') return configured;
  }
  return envBool('VITE_PUBLIC_HIDE_YT_AT', true);
}

export function getShowBadges(): boolean {
  if (hasPersistedSettings()) {
    const configured = get(settings).showBadges;
    if (typeof configured === 'boolean') return configured;
  }
  return envBool('VITE_PUBLIC_SHOW_BADGES', true);
}

export function isFetchHistoryOnLoad(): boolean {
  if (hasPersistedSettings()) {
    const configured = get(settings).fetchHistoryOnLoad;
    if (typeof configured === 'boolean') return configured;
  }
  const fetchHistoryEnv = (import.meta.env.VITE_PUBLIC_FETCH_HISTORY_ON_LOAD ?? '').toString().trim().toLowerCase();
  return fetchHistoryTruthyValues.has(fetchHistoryEnv);
}

export function isChatDebugEnabled(): boolean {
  if (hasPersistedSettings()) {
    const configured = get(settings).chatDebug;
    if (typeof configured === 'boolean') return configured;
  }
  return (import.meta.env.VITE_CHAT_DEBUG ?? '').toString().trim() === '1';
}

export function apiPath(path: string): string {
  const normalized = path.startsWith('/') ? path : `/${path}`;
  return `${getApiBaseUrl()}${normalized}`;
}
