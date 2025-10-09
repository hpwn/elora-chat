<script lang="ts">
  import type { Message, Keymods } from '$lib/types/messages';
  import { onMount, setContext } from 'svelte';
  import ChatMessage from './ChatMessage.svelte';
  import PauseOverlay from './PauseOverlay.svelte';

  import { deployedUrl, useDeployedApi } from '$lib/config';
  import { parseWsMessages, unwrapWsPayload } from '$lib/messages';
  import { SvelteSet } from 'svelte/reactivity';

  let container: HTMLDivElement;

  let ws: WebSocket | null = $state(null);
  const messageQueue: Message[] = $state([]);
  const messages: Message[] = $state([]);
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

    // If there's a large number of messages, only keep the last N
    const N = 200;
    if (messageQueue.length > N) {
      messageQueue.splice(0, messageQueue.length - N);
    }

    processing = true;
    const message = messageQueue.shift()!;

    // Replace black usernames with higher contrast color to show up on black background
    if (message.colour === '#000000') {
      message.colour = '#CCCCCC'; // Light grey for visibility
    }

    // Add the message to the messages array
    messages.push(message);

    // Scroll to the bottom of the chat container
    if (!paused) {
      setTimeout(() => {
        container.scrollTop = container.scrollHeight;
        newMessageCount = 0;
      }, 0);
    } else {
      newMessageCount++;
    }

    // Continue processing after a delay
    setTimeout(processMessageQueue, 0); // Delay of x ms between messages
  }

  function initializeWebSocket() {
    console.log('Initializing WebSocket');
    const wsProtocol = window.location.protocol === 'https:' ? 'wss' : 'ws';

    const localUrl = `${wsProtocol}://${window.location.host}`;
    const wsUrl = `${useDeployedApi ? deployedUrl : localUrl}/ws/chat`;

    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      console.log('WebSocket is already connected or connecting. No action taken.');
      return;
    }

    console.log('WebSocket URL:', wsUrl);
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
      console.log('WebSocket Connection established');
    };

    ws.onmessage = (event) => {
      let payload: unknown = event.data;
      if (typeof event.data === 'string') {
        payload = unwrapWsPayload(event.data);
      }

      if (payload == null) {
        return;
      }

      const incomingMessages = parseWsMessages(payload);
      if (incomingMessages.length === 0) {
        return;
      }

      messageQueue.push(...incomingMessages);
      if (!processing) {
        processMessageQueue();
      }
    };

    ws.onerror = (error) => {
      console.error('WebSocket Error:', error);
    };

    ws.onclose = () => {
      console.log('WebSocket Connection closed. Attempting to reconnect...');
      // Removed the setTimeout here to avoid automatic reconnection.
      // The reconnection attempt will be managed by the visibility change or manual triggers.
    };
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

<style lang="scss">
  #chat-container {
    display: flex;
    flex-direction: column;
    flex: 1;
    padding: 0 10px;

    overflow-y: auto;
    scrollbar-width: none;
  }
</style>
