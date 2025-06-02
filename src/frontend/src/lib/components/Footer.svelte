<script lang="ts">
  import SendMessage from './SendMessage.svelte';
  import ChatSettings from './ChatSettings.svelte';
</script>

<footer>
  <SendMessage />
  <ChatSettings />
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
