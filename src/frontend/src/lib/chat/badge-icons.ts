const BADGE_BASE_COLORS = {
  broadcaster: '#e91916',
  moderator: '#34ae0a',
  vip: '#e005d9',
  subscriber: '#9146ff',
  founder: '#f2b807',
  partner: '#a970ff',
  staff: '#18181b',
  premium: '#9146ff',
  bits1: '#b9a3e3',
  bits100: '#9c7be4',
  bits1000: '#7851a9',
  bits5000: '#5c16c5',
  bits10000: '#009dd7',
  bits25000: '#3a1484',
  bits50000: '#e54bb5',
  bits75000: '#f200ff',
  bits100000: '#0cb6a6',
  bits200000: '#ffea00'
} as const;

type BadgeVersionIcon = {
  src: string;
  text?: string;
};

type BadgeIconEntry = {
  label: string;
  defaultSrc?: string;
  defaultText?: string;
  versions?: Record<string, BadgeVersionIcon>;
};

export type ResolvedBadgeIcon = {
  label: string;
  alt: string;
  src?: string;
};

function createBadgeSvg(background: string, text: string, textColor = '#ffffff'): string {
  const trimmed = text.trim();
  const length = trimmed.length;
  const fontSize = length <= 2 ? 32 : length === 3 ? 28 : length === 4 ? 24 : 20;
  const svg = `<?xml version="1.0" encoding="UTF-8"?>\n<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">\n  <rect width="64" height="64" rx="12" fill="${background}"/>\n  <text x="32" y="40" text-anchor="middle" font-family="'Inter','Segoe UI',sans-serif" font-size="${fontSize}" font-weight="700" fill="${textColor}">${trimmed}</text>\n</svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

function createBitsEntry(version: string, text: string, colorKey: keyof typeof BADGE_BASE_COLORS): BadgeVersionIcon {
  return {
    src: createBadgeSvg(BADGE_BASE_COLORS[colorKey], text),
    text
  };
}

const BADGE_ICON_MAP: Record<string, BadgeIconEntry> = {
  broadcaster: {
    label: 'Broadcaster',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.broadcaster, 'HOST'),
    defaultText: 'HOST'
  },
  moderator: {
    label: 'Moderator',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.moderator, 'MOD'),
    defaultText: 'MOD'
  },
  vip: {
    label: 'VIP',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.vip, 'VIP'),
    defaultText: 'VIP'
  },
  subscriber: {
    label: 'Subscriber',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.subscriber, 'SUB'),
    defaultText: 'SUB'
  },
  founder: {
    label: 'Founder',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.founder, 'FD'),
    defaultText: 'FD'
  },
  partner: {
    label: 'Partner',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.partner, 'P'),
    defaultText: 'P'
  },
  staff: {
    label: 'Staff',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.staff, 'STAFF', '#f8fafc'),
    defaultText: 'STAFF'
  },
  premium: {
    label: 'Turbo',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.premium, 'TB'),
    defaultText: 'TB'
  },
  prime: {
    label: 'Turbo',
    defaultSrc: createBadgeSvg(BADGE_BASE_COLORS.premium, 'TB'),
    defaultText: 'TB'
  },
  bits: {
    label: 'Bits',
    versions: {
      '1': createBitsEntry('1', '1', 'bits1'),
      '100': createBitsEntry('100', '100', 'bits100'),
      '1000': createBitsEntry('1000', '1K', 'bits1000'),
      '5000': createBitsEntry('5000', '5K', 'bits5000'),
      '10000': createBitsEntry('10000', '10K', 'bits10000'),
      '25000': createBitsEntry('25000', '25K', 'bits25000'),
      '50000': createBitsEntry('50000', '50K', 'bits50000'),
      '75000': createBitsEntry('75000', '75K', 'bits75000'),
      '100000': createBitsEntry('100000', '100K', 'bits100000'),
      '200000': createBitsEntry('200000', '200K', 'bits200000')
    },
    defaultText: 'BITS'
  }
};

const BADGE_ALIASES: Record<string, string> = {
  broadcaster_badge: 'broadcaster',
  moderator_badge: 'moderator',
  administrator: 'staff',
  admin: 'staff',
  primegaming: 'premium',
  turbo: 'premium'
};

function normalizeId(id: string): string {
  const lower = id.toLowerCase();
  return BADGE_ALIASES[lower] ?? lower;
}

function formatBadgeName(id: string): string {
  return id
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

export function resolveBadgeIcon(id: string, version?: string | null): ResolvedBadgeIcon {
  const normalizedId = normalizeId(id);
  const entry = BADGE_ICON_MAP[normalizedId];
  const cleanVersion = version?.toString().trim();

  let src: string | undefined;
  let text: string | undefined;

  if (entry?.versions && cleanVersion && entry.versions[cleanVersion]) {
    const versionEntry = entry.versions[cleanVersion];
    src = versionEntry.src;
    text = versionEntry.text;
  }

  if (!src && entry?.defaultSrc) {
    src = entry.defaultSrc;
  }

  if (!text && entry?.defaultText) {
    text = entry.defaultText;
  }

  const baseLabel = entry?.label ?? formatBadgeName(normalizedId || id);
  const alt = cleanVersion ? `${baseLabel} ${cleanVersion}` : baseLabel;
  const label = text ?? (cleanVersion ? cleanVersion.toUpperCase() : baseLabel.slice(0, 3).toUpperCase());

  return { label, alt, src };
}
