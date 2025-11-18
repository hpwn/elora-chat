interface Image {
  id: string;
  url: string;
  width: number;
  height: number;
}

interface Badge {
  id: string;
  version?: string;
  platform?: 'YouTube' | 'Twitch' | 'youtube' | 'twitch' | string;
  images?: Image[];
  imageUrl?: string;
  title?: string;
}

export interface Emote {
  id: string;
  name: string;
  images: Image[];
  locations: unknown; // TODO: determine the correct type for this
}

export const enum FragmentType {
  Text = 'text',
  Emote = 'emote',
  Colour = 'colour',
  Effect = 'effect',
  Pattern = 'pattern'
}

export interface Fragment {
  type: FragmentType;
  text: string;
  emote: Emote | null;
}

export interface Message {
  author: string;
  badges: Badge[];
  displayBadges?: Badge[];
  badges_raw?: unknown;
  colour: string;
  message: string;
  fragments: Fragment[];
  emotes: Emote[];
  source: 'YouTube' | 'Twitch' | 'Test' | 'youtube' | 'twitch';
}

export interface Keymods {
  ctrl: boolean;
  shift: boolean;
  alt: boolean;

  reset: () => void;
}
