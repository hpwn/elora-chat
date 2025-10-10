<script lang="ts">
  import type { Message, Keymods } from '$lib/types/messages';
  import { onMount, setContext } from 'svelte';
  import ChatMessage from './ChatMessage.svelte';
  import PauseOverlay from './PauseOverlay.svelte';

  import { deployedUrl, useDeployedApi } from '$lib/config';
  import { parseWsEvent } from '$lib/messages';
  import { SvelteSet } from 'svelte/reactivity';

  const CHAT_DEBUG = import.meta.env.VITE_CHAT_DEBUG === '1';

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
    ws = new WebSocket(wsUrl);
    ws.binaryType = 'arraybuffer';

    ws.onopen = () => CHAT_DEBUG && console.log('[chat] open');

    ws.onmessage = async (event) => {
      try {
        const incoming = await parseWsEvent(event);
        if (!incoming.length) return;

        messageQueue = [...messageQueue, ...incoming];
        if (!processing) processMessageQueue();
      } catch (error) {
        if (import.meta?.env?.VITE_CHAT_DEBUG) {
          console.warn('WS parse error', error);
        }
      }
    };

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
