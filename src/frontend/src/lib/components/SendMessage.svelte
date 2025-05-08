<script lang="ts">
  import { authState } from '$lib/api/auth.svelte';
  import { buildApiUrl } from '$lib/utils';

  let message = '';

  function sendMessage(event: Event) {
    event.preventDefault();
    if (!authState.loggedIn) return;

    // Check if the message is empty
    if (!message) {
      console.log('No message to send');
      return;
    }

    // Send the message
    fetch(buildApiUrl('/auth/send-message'), {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ message }),
      credentials: 'include' // Important for session handling
    })
      .then((response) => {
        if (response.ok) {
          console.log('Message sent successfully');
        } else {
          console.error('Failed to send message');
        }
      })
      .catch((error) => console.error('Error sending message:', error));

    // Clear message input
    message = '';
  }
</script>

<form id="message-send-container" on:submit={sendMessage}>
  <input id="message-input" type="text" placeholder="Type a message..." bind:value={message} />
  <button id="send-message-button" type="submit" disabled={!authState.loggedIn}>send</button>
</form>

<style lang="scss">
  #message-send-container {
    display: flex;
    flex-shrink: 1;
    align-items: center;

    margin: 10px 5px 0 5px;
    padding-bottom: 10px;
  }

  #message-input {
    width: auto;
    height: 50px;
    flex-grow: 1;

    background: #333;
    color: white;
    font-size: 1.2em;
    border: 1px solid #666;
    border-radius: 5px;

    margin-right: 10px;
    padding: 10px;
  }

  #send-message-button {
    height: 50px;
    flex-shrink: 0;

    background-color: #444;
    color: #bbb;
    font-size: 1em;
    border: 1px solid #666;
    border-radius: 5px;
    cursor: pointer;

    padding: 10px 20px;

    transition: background-color 0.3s;
  }
</style>
