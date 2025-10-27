const envApiBase = (import.meta.env.VITE_PUBLIC_API_BASE ?? '').toString().trim();
const defaultOrigin = typeof window !== 'undefined' && window.location?.origin ? window.location.origin : 'http://localhost:8080';

const normalizedApiBase = (envApiBase || defaultOrigin).replace(/\/+$/, '');
export const apiBaseUrl = normalizedApiBase || defaultOrigin;

const envWsUrl = (import.meta.env.VITE_PUBLIC_WS_URL ?? '').toString().trim();
export const wsUrl = (() => {
  if (envWsUrl) return envWsUrl;
  try {
    const parsed = new URL(apiBaseUrl);
    const scheme = parsed.protocol === 'https:' ? 'wss' : 'ws';
    return `${scheme}://${parsed.host}/ws/chat`;
  } catch {
    return 'ws://localhost:8080/ws/chat';
  }
})();

const hideEnv = (import.meta.env.VITE_PUBLIC_HIDE_YT_AT ?? '1').toString().trim().toLowerCase();
export const hideYouTubeAt = hideEnv !== '0' && hideEnv !== 'false';

const showEnv = (import.meta.env.VITE_PUBLIC_SHOW_BADGES ?? '1').toString().trim().toLowerCase();
export const showBadges = showEnv !== '0' && showEnv !== 'false';

export function apiPath(path: string): string {
  const normalized = path.startsWith('/') ? path : `/${path}`;
  return `${apiBaseUrl}${normalized}`;
}
