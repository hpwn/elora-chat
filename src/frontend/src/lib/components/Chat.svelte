<script lang="ts">
  import type { Message, Keymods } from '$lib/types/messages';
  import { onMount, setContext } from 'svelte';
  import ChatMessage from './ChatMessage.svelte';
  import PauseOverlay from './PauseOverlay.svelte';

  import { deployedUrl, useDeployedApi } from '$lib/config';
  import { connectChat, type ChatMessage as WsChatMessage } from '$lib/chat/ws';
  import { SvelteSet } from 'svelte/reactivity';

  const CHAT_DEBUG = import.meta.env.VITE_CHAT_DEBUG === '1';
  const DEFAULT_COLOUR = '#ffffff';
  const DEFAULT_SOURCE: Message['source'] = 'YouTube';
  const ALLOWED_SOURCES = new Set<Message['source']>(['YouTube', 'Twitch', 'Test']);

  let container: HTMLDivElement;

  let ws: WebSocket | null = $state(null);
  let messageQueue: Message[] = $state([]);
  let messages: Message[] = $state([]);
  let processing = $state(false);
  let paused = $state(false);
  let newMessageCount = $state(0);
  let blacklist = loadBlacklist();
  let keymods: Keymods = {
    ctrl: false,
    shift: false,
    alt: false,
    reset() {
      this.ctrl = false;
      this.shift = false;
      this.alt = false;
    }
  };

  setContext('blacklist', blacklist);
  setContext('keymods', keymods);

  function convertIncomingMessage(message: WsChatMessage): Message | null {
    const author = message.username && message.username.trim().length > 0 ? message.username : 'Unknown';
    const text = typeof message.text === 'string' ? message.text : '';
    if (!text && (!Array.isArray(message.emotes) || message.emotes.length === 0)) {
      return null;
    }

    const rawColour = typeof message.colour === 'string' ? message.colour : '';
    const colour = rawColour && rawColour.trim().length > 0 ? rawColour : DEFAULT_COLOUR;

    const sourceCandidate = typeof message.platform === 'string' ? (message.platform as Message['source']) : DEFAULT_SOURCE;
    const source = ALLOWED_SOURCES.has(sourceCandidate) ? sourceCandidate : DEFAULT_SOURCE;

    const emotes = coerceEmotes(message.emotes);
    const badges = coerceBadges(message.badges);

    return {
      author,
      message: text,
      colour,
      source,
      fragments: [],
      emotes,
      badges
    } satisfies Message;
  }

  function coerceEmotes(emotes: WsChatMessage['emotes']): Message['emotes'] {
    if (!Array.isArray(emotes)) return [];
    const out: Message['emotes'] = [];
    for (const emote of emotes) {
      if (!emote || typeof emote !== 'object') continue;
      const record = emote as Record<string, unknown>;
      const name = typeof record.name === 'string' && record.name.trim().length > 0 ? record.name : undefined;
      if (!name) continue;
      const id = typeof record.id === 'string' && record.id.trim().length > 0 ? record.id : name;
      const imagesRaw = Array.isArray(record.images) ? record.images : [];
      const images = imagesRaw.flatMap((img) => {
        if (!img || typeof img !== 'object') return [] as Message['emotes'][number]['images'];
        const imageRecord = img as Record<string, unknown>;
        const url = typeof imageRecord.url === 'string' ? imageRecord.url : undefined;
        if (!url) return [] as Message['emotes'][number]['images'];
        return [
          {
            id: typeof imageRecord.id === 'string' ? imageRecord.id : `${id}-${url}`,
            url,
            width: typeof imageRecord.width === 'number' ? imageRecord.width : 0,
            height: typeof imageRecord.height === 'number' ? imageRecord.height : 0
          }
        ];
      });

      out.push({
        id,
        name,
        images,
        locations: record.locations ?? []
      });
    }
    return out;
  }

  function coerceBadges(badges: WsChatMessage['badges']): Message['badges'] {
    if (!Array.isArray(badges)) return [];
    const out: Message['badges'] = [];
    for (const badge of badges) {
      if (!badge || typeof badge !== 'object') continue;
      const record = badge as Record<string, unknown>;
      const nameCandidate = typeof record.name === 'string' && record.name.trim().length > 0 ? record.name : undefined;
      const titleCandidate = typeof record.title === 'string' && record.title.trim().length > 0 ? record.title : undefined;
      const name = nameCandidate ?? titleCandidate ?? 'badge';
      const title = titleCandidate ?? name;
      const iconsRaw = Array.isArray(record.icons) ? record.icons : [];
      const icons = iconsRaw.flatMap((icon) => {
        if (!icon || typeof icon !== 'object') return [] as Message['badges'][number]['icons'];
        const iconRecord = icon as Record<string, unknown>;
        const url = typeof iconRecord.url === 'string' ? iconRecord.url : undefined;
        if (!url) return [] as Message['badges'][number]['icons'];
        return [
          {
            id: typeof iconRecord.id === 'string' ? iconRecord.id : `${name}-${url}`,
            url,
            width: typeof iconRecord.width === 'number' ? iconRecord.width : 0,
            height: typeof iconRecord.height === 'number' ? iconRecord.height : 0
          }
        ];
      });

      out.push({
        name,
        title,
        icons,
        clickAction: typeof record.clickAction === 'string' ? record.clickAction : '',
        clickURL: typeof record.clickURL === 'string' ? record.clickURL : ''
      });
    }
    return out;
  }

  function loadBlacklist(): SvelteSet<string> {
    const list = window.localStorage.getItem('blacklist');
    if (!list) {
      return new SvelteSet();
    }
    const parsedList = JSON.parse(list);
    if (!parsedList) {
      return new SvelteSet();
    }
    return new SvelteSet(parsedList);
  }

  function saveBlacklist() {
    window.localStorage.setItem('blacklist', JSON.stringify([...blacklist]));
  }

  function pauseChat() {
    paused = true;
  }

  function unpauseChat() {
    paused = false;
    setTimeout(() => {
      container.scrollTop = container.scrollHeight;
      newMessageCount = 0;
    }, 0);
  }

  function togglePause() {
    if (paused) {
      unpauseChat();
    } else {
      pauseChat();
    }
  }

  function processMessageQueue() {
    // console.log("Processing message queue", messageQueue);
    if (messageQueue.length === 0) {
      processing = false;
      return;
    }

    const N = 200;
    if (messageQueue.length > N) {
      messageQueue = messageQueue.slice(-N);
    }

    processing = true;

    const [next, ...rest] = messageQueue;
    messageQueue = rest;

    if (!next) {
      processing = false;
      return;
    }

    if (next.colour === '#000000') next.colour = '#CCCCCC';

    messages = [...messages, next];

    if (!paused) {
      setTimeout(() => {
        container.scrollTop = container.scrollHeight;
        newMessageCount = 0;
      }, 0);
    } else {
      newMessageCount = newMessageCount + 1;
    }

    setTimeout(processMessageQueue, 0);
  }

  function initializeWebSocket() {
    const wsProtocol = window.location.protocol === 'https:' ? 'wss' : 'ws';

    const localUrl = `${wsProtocol}://${window.location.host}`;
    const wsUrl = `${useDeployedApi ? deployedUrl : localUrl}/ws/chat`;

    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      if (CHAT_DEBUG) console.log('[chat] ws already connected/connecting');
      return;
    }

    if (CHAT_DEBUG) console.log('[chat] url:', wsUrl);
    ws = connectChat((incoming) => {
      const normalized = convertIncomingMessage(incoming);
      if (!normalized) return;

      messageQueue = [...messageQueue, normalized];
      if (!processing) processMessageQueue();
    }, wsUrl);

    ws.onopen = () => CHAT_DEBUG && console.log('[chat] open');

    ws.onerror = (error) => CHAT_DEBUG && console.error('[chat] error:', error);

    ws.onclose = () => CHAT_DEBUG && console.log('[chat] close');
  }

  onMount(() => {
    initializeWebSocket();

    document.addEventListener('keydown', (e) => {
      switch (e.key) {
        case 'P':
        case 'p':
          togglePause();
          break;
        case 'Control':
          keymods.ctrl = true;
          break;
        case 'Shift':
          keymods.shift = true;
          break;
        case 'Alt':
          keymods.alt = true;
          break;
      }
    });

    document.addEventListener('keyup', (e) => {
      switch (e.key) {
        case 'Control':
          keymods.ctrl = false;
          break;
        case 'Shift':
          keymods.shift = false;
          break;
        case 'Alt':
          keymods.alt = false;
          break;
      }
    });

    document.addEventListener('visibilitychange', () => {
      saveBlacklist();
      keymods.reset();
    });

    window.addEventListener('beforeunload', () => {
      if (ws) {
        ws.close();
        ws = null;
      }
      saveBlacklist();
    });
  });
</script>

<div
  id="chat-container"
  aria-label="Chat messages"
  role="list"
  onmouseenter={pauseChat}
  onmouseleave={unpauseChat}
  bind:this={container}
>
  {#each messages as message}
    {#if !blacklist.has(message.author)}
      <ChatMessage {message} />
    {/if}
  {/each}
  {#if paused}
    <PauseOverlay {newMessageCount} {unpauseChat} />
  {/if}
</div>

{#if import.meta?.env?.VITE_CHAT_DEBUG}
  <div
    style="position:absolute;right:.5rem;bottom:.5rem;font:12px/1.2 monospace;background:#0008;color:#fff;padding:.25rem .5rem;border-radius:.5rem;z-index:9999"
  >
    msgs:{messages.length} q:{messageQueue.length} pause:{String(paused)}
  </div>
{/if}

<style lang="scss">
  #chat-container {
    display: flex;
    flex-direction: column;
    flex: 1;
    position: relative;
    padding: 0 10px;

    overflow-y: auto;
    scrollbar-width: none;
  }
</style>
