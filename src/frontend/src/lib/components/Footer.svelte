<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import SendMessage from './SendMessage.svelte';
  import ChatSettings from './ChatSettings.svelte';

  const dispatch = createEventDispatcher();

  function handleOpenSettings() {
    dispatch('open-settings');
  }
</script>

<footer>
  <SendMessage />
  <ChatSettings on:open-settings={handleOpenSettings} />
</footer>

<style lang="scss">
  footer {
    display: flex;
    align-items: center;
    justify-content: center;
    margin: 10px 5px 5px;
    padding-bottom: 10px;
    gap: 10px;
  }
</style>
