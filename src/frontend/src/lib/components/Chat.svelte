<script lang="ts">
  import type { Message } from '$lib/types/messages';
  import { onMount } from 'svelte';
  import ChatMessage from './ChatMessage.svelte';

  import { deployedUrl, useDeployedApi } from '$lib/config';

  let container: HTMLDivElement;

  let ws: WebSocket | null = $state(null);
  const messageQueue: Message[] = $state([]);
  const messages: Message[] = $state([]);
  let processing = $state(false);

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
    setTimeout(() => {
      container.scrollTop = container.scrollHeight;
    }, 0);

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
      // console.log("Message received: ", event.data);
      const msg = event.data;
      if (msg === '__keepalive__') {
        return;
      }

      try {
        const parsedMsg = JSON.parse(msg);
        messageQueue.push(parsedMsg);
        if (!processing) {
          processMessageQueue();
        }
      } catch (e) {
        console.error('Error parsing message:', msg, e);
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

    window.addEventListener('beforeunload', () => {
      if (ws) {
        ws.close();
        ws = null;
      }
    });
  });
</script>

<div id="chat-container" bind:this={container}>
  {#each messages as message}
    <ChatMessage {message} />
  {/each}
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
