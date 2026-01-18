// @vitest-environment jsdom

import { render, screen, waitFor } from '@testing-library/svelte';
import { cleanup } from '@testing-library/svelte';
import Chat from '../Chat.svelte';
import type { ChatMessage as WsChatMessage, OnMessage } from '$lib/chat/ws';
import { afterEach, describe, expect, it, vi } from 'vitest';

const handlers: OnMessage[] = [];

vi.mock('$lib/chat/ws', () => {
  return {
    connectChat: (onMessage: OnMessage) => {
      handlers.push(onMessage);
      const mockSocket = {
        close: vi.fn(),
        readyState: 1,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn()
      } as unknown as WebSocket;
      return mockSocket;
    },
    __pushMockMessage: (msg: WsChatMessage) => handlers.forEach((fn) => fn(msg))
  };
});

async function renderChat(opts: { fetchHistoryOnLoad?: boolean } = {}) {
  vi.stubEnv('VITE_CHAT_DEBUG', '1');
  vi.stubEnv('VITE_PUBLIC_FETCH_HISTORY_ON_LOAD', opts.fetchHistoryOnLoad ? '1' : '0');
  (globalThis as any).fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ items: [], next_before_ts: null, next_before_rowid: null })
  });

  (globalThis as any).WebSocket = class MockWebSocket {
    static OPEN = 1;
    static CONNECTING = 0;
    readyState = 1;
    close = vi.fn();
  } as unknown as typeof WebSocket;

  return render(Chat);
}

describe('Chat websocket queue', () => {
  afterEach(() => {
    handlers.length = 0;
    cleanup();
    vi.resetAllMocks();
    vi.unstubAllEnvs();
  });

  it('renders interleaved Twitch and YouTube messages without dropping', async () => {
    await renderChat();
    const { __pushMockMessage } = await import('$lib/chat/ws');

    const baseTs = Date.now();
    const incoming: WsChatMessage[] = [
      { id: 'yt-1', ts: baseTs, username: 'yt-a', platform: 'YouTube', text: 'yt-one', emotes: [], badges: [] },
      { id: 'tw-1', ts: baseTs, username: 'tw-a', platform: 'Twitch', text: 'tw-one', emotes: [], badges: [] },
      { id: 'yt-2', ts: baseTs, username: 'yt-b', platform: 'YouTube', text: 'yt-two', emotes: [], badges: [] }
    ];

    for (const msg of incoming) __pushMockMessage(msg);

    await waitFor(() => expect(screen.getAllByRole('button')).toHaveLength(incoming.length));

    const rendered = screen.getAllByRole('button');
    const platforms = rendered.map((node) => node.getAttribute('data-platform'));
    expect(platforms).toEqual(['YouTube', 'Twitch', 'YouTube']);

    expect(screen.getByText('yt-one')).toBeInTheDocument();
    expect(screen.getByText('yt-two')).toBeInTheDocument();
    expect(screen.getByText('tw-one')).toBeInTheDocument();
  });

  it('does not fetch history by default', async () => {
    await renderChat();

    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it('fetches history when enabled', async () => {
    await renderChat({ fetchHistoryOnLoad: true });

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
  });

  it('does not drop when Twitch and YouTube share the same upstream id', async () => {
    await renderChat();
    const { __pushMockMessage } = await import('$lib/chat/ws');

    const baseTs = Date.now();
    const incoming: WsChatMessage[] = [
      { id: 'same-id', ts: baseTs + 1, username: 'yt-a', platform: 'YouTube', text: 'yt-one', emotes: [], badges: [] },
      { id: 'same-id', ts: baseTs + 2, username: 'tw-a', platform: 'Twitch',  text: 'tw-one', emotes: [], badges: [] },
      { id: 'same-id-2', ts: baseTs + 3, username: 'yt-b', platform: 'YouTube', text: 'yt-two', emotes: [], badges: [] }
    ];

    for (const msg of incoming) __pushMockMessage(msg);

    await waitFor(() => expect(screen.getAllByRole('button')).toHaveLength(incoming.length));

    const rendered = screen.getAllByRole('button');
    const platforms = rendered.map((node) => node.getAttribute('data-platform'));
    expect(platforms).toEqual(['YouTube', 'Twitch', 'YouTube']);

    expect(screen.getByText('yt-one')).toBeInTheDocument();
    expect(screen.getByText('tw-one')).toBeInTheDocument();
    expect(screen.getByText('yt-two')).toBeInTheDocument();
  });

  it('renders text when fragment emote is empty', async () => {
    await renderChat();
    const { __pushMockMessage } = await import('$lib/chat/ws');

    const baseTs = Date.now();
    const incoming: WsChatMessage = {
      id: 'yt-emoji-1',
      ts: baseTs,
      username: 'yt-a',
      platform: 'YouTube',
      text: '',
      emotes: [],
      badges: [],
      fragments: [
        {
          type: 'text',
          text: ':grinning_squinting_face:',
          emote: { id: '', name: '', locations: [], images: [] }
        }
      ]
    };

    __pushMockMessage(incoming);

    await waitFor(() => expect(screen.getByText('😆')).toBeInTheDocument());
    expect(screen.queryByText(':grinning_squinting_face:')).not.toBeInTheDocument();
  });

  it('replaces known YouTube shortcodes with unicode', async () => {
    await renderChat();
    const { __pushMockMessage } = await import('$lib/chat/ws');

    const baseTs = Date.now();
    const incoming: WsChatMessage = {
      id: 'yt-emoji-2',
      ts: baseTs,
      username: 'yt-b',
      platform: 'YouTube',
      text: '',
      emotes: [],
      badges: [],
      fragments: [
        {
          type: 'text',
          text: ':rolling_on_the_floor_laughing:',
          emote: null
        }
      ]
    };

    __pushMockMessage(incoming);

    await waitFor(() => expect(screen.getByText('🤣')).toBeInTheDocument());
    expect(screen.queryByText(':rolling_on_the_floor_laughing:')).not.toBeInTheDocument();
  });
});
