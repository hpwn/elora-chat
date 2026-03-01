import { normalizeAlertType, type AlertPlatform, type AlertType } from './preferences';

export interface NormalizedAlert {
  id: string;
  ts: number;
  platform: AlertPlatform;
  type: AlertType;
  username: string;
  sourceChannel?: string;
  sourceUrl?: string;
  message?: string;
  amount?: number;
  currency?: string;
}

export function normalizeAlertPayload(input: unknown): NormalizedAlert | null {
  const record = coerceAlertRecord(input);
  if (!record) return null;

  const rawPlatform = readString(record.platform ?? record.source ?? record.service);
  const platform = normalizePlatform(rawPlatform);
  if (!platform) return null;

  const type = normalizeAlertType(record.type ?? record.alert_type ?? record.kind);
  if (!type) return null;

  const id =
    readString(record.id ?? record.alert_id ?? record.platform_event_id ?? record.uid) ||
    cryptoRandom();
  const username =
    readString(record.username ?? record.author ?? record.user ?? record.display_name) || 'Alert';
  const sourceChannel =
    readString(record.source_channel ?? record.sourceChannel ?? record.channel) || undefined;
  const sourceUrl = readString(record.source_url ?? record.sourceUrl ?? record.url) || undefined;
  const message = readString(record.message ?? record.text ?? record.body) || undefined;
  const amount = readNumber(record.amount ?? record.value);
  const currency = readString(record.currency) || undefined;
  const ts = coerceTimestamp(record.ts ?? record.timestamp ?? record.created_at ?? record.time);

  return {
    id,
    ts,
    platform,
    type,
    username,
    sourceChannel,
    sourceUrl,
    message,
    amount: amount ?? undefined,
    currency
  };
}

export function describeAlert(alert: NormalizedAlert): string {
  const kind = humanizeType(alert.type);
  const amount = typeof alert.amount === 'number' ? formatAmount(alert.amount, alert.currency) : '';
  const prefix = `${alert.username} ${kind}`;
  const summary = amount ? `${prefix} (${amount})` : prefix;
  return alert.message ? `${summary} - ${alert.message}` : summary;
}

function coerceAlertRecord(input: unknown): Record<string, unknown> | null {
  if (typeof input === 'string') {
    const parsed = safeJson<unknown>(input, null);
    return coerceAlertRecord(parsed);
  }
  if (!input || typeof input !== 'object') return null;

  const record = input as Record<string, unknown>;
  if (record.type === 'alert' && 'data' in record) {
    return coerceAlertRecord(record.data);
  }

  return record;
}

function normalizePlatform(raw: string): AlertPlatform | null {
  switch (raw.toLowerCase()) {
    case 'twitch':
      return 'twitch';
    case 'youtube':
      return 'youtube';
    default:
      return null;
  }
}

function humanizeType(type: AlertType): string {
  switch (type) {
    case 'subs':
      return 'subscribed';
    case 'gifted_subs':
      return 'gifted subs';
    case 'bits':
      return 'sent bits';
    case 'raids':
      return 'raided';
    case 'members':
      return 'became a member';
    case 'super_chats':
      return 'sent a super chat';
    case 'gifted_members':
      return 'gifted memberships';
    default:
      return type.replaceAll('_', ' ');
  }
}

function formatAmount(amount: number, currency?: string): string {
  if (!Number.isFinite(amount)) return '';
  const rounded = Number.isInteger(amount) ? amount.toString() : amount.toFixed(2);
  return currency ? `${rounded} ${currency}` : rounded;
}

function readString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

function readNumber(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string') {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
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
  if (tsNum == null) return Date.now();
  return tsNum < 1_000_000_000_000 ? tsNum * 1000 : tsNum;
}

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
  return `alert-${Math.random().toString(36).slice(2)}`;
}
