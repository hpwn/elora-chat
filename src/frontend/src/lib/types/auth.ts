interface TwitchSessionData {
  id: string;
  login: string;
  display_name: string;
  description: string;
  profile_image_url: string;
  offline_image_url: string;
  type: string;
  broadcaster_type: string;
  view_count: number;
  created_at: string;
}

export interface TwitchSession {
  twitch_token: string;
  refresh_token: string;
  token_expiry: number;
  data: TwitchSessionData[];
  services: string[];
}
