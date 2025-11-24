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

async function renderChat() {
  vi.stubEnv('VITE_CHAT_DEBUG', '1');
  (global as any).fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ items: [], next_before_ts: null, next_before_rowid: null })
  });

  (global as any).WebSocket = class MockWebSocket {
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
      { id: 'yt-1', ts: baseTs, username: 'yt-a', platform: 'YouTube', text: 'yt-one' },
      { id: 'tw-1', ts: baseTs, username: 'tw-a', platform: 'Twitch', text: 'tw-one' },
      { id: 'yt-2', ts: baseTs, username: 'yt-b', platform: 'YouTube', text: 'yt-two' }
    ];

    for (const msg of incoming) {
      __pushMockMessage(msg);
    }

    await waitFor(() => expect(screen.getAllByRole('button')).toHaveLength(incoming.length));

    const rendered = screen.getAllByRole('button');
    const platforms = rendered.map((node) => node.getAttribute('data-platform'));
    expect(platforms).toEqual(['YouTube', 'Twitch', 'YouTube']);

    expect(screen.getByText('yt-one')).toBeInTheDocument();
    expect(screen.getByText('yt-two')).toBeInTheDocument();
    expect(screen.getByText('tw-one')).toBeInTheDocument();
  });

  it('retains all YouTube messages when interleaved with Twitch bursts sharing timestamps', async () => {
    await renderChat();
    const { __pushMockMessage } = await import('$lib/chat/ws');

    const baseTs = Date.now();
    const incoming: WsChatMessage[] = [];

    for (let i = 0; i < 10; i++) {
      incoming.push({ id: `yt-${i}`, ts: baseTs, username: `yt-${i}`, platform: 'YouTube', text: `yt-${i}` });
      incoming.push({ id: `tw-${i}`, ts: baseTs, username: `tw-${i}`, platform: 'Twitch', text: `tw-${i}` });
    }

    for (const msg of incoming) {
      __pushMockMessage(msg);
    }

    await waitFor(() => expect(screen.getAllByRole('button')).toHaveLength(incoming.length));

    for (let i = 0; i < 10; i++) {
      expect(screen.getByText(`yt-${i}`)).toBeInTheDocument();
      expect(screen.getByText(`tw-${i}`)).toBeInTheDocument();
    }
  });
});
