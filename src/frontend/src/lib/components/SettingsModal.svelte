<script lang="ts">
  import { tick } from 'svelte';
  import { settings } from '$lib/stores/settings';

  let dialog: HTMLDivElement | null = null;
  export let open = false;

  $: if (open) {
    tick().then(() => {
      dialog?.focus();
    });
  }

  function close() {
    open = false;
  }

  function toggleExportPanel(event: Event) {
    const checked = (event.target as HTMLInputElement).checked;
    settings.update((current) => ({ ...current, showExportPanel: checked }));
  }

  function handleOverlayKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      close();
    }
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (!open) {
      return;
    }

    if (event.key === 'Escape') {
      event.preventDefault();
      close();
    }
  }
</script>

<svelte:window on:keydown={handleWindowKeydown} />

{#if open}
  <div
    aria-label="Close settings"
    class="backdrop"
    on:click={close}
    on:keydown={handleOverlayKeydown}
    role="button"
    tabindex="0"
  ></div>
  <div
    aria-labelledby="settings-modal-title"
    aria-modal="true"
    bind:this={dialog}
    class="modal"
    role="dialog"
    tabindex="-1"
  >
    <header class="modal__header">
      <h2 class="modal__title" id="settings-modal-title">Settings</h2>
      <button class="modal__close" on:click={close} type="button" aria-label="Close settings">
        ✕
      </button>
    </header>

    <div class="modal__content">
      <label class="toggle">
        <input
          checked={$settings.showExportPanel}
          class="toggle__input"
          on:change={toggleExportPanel}
          type="checkbox"
        />
        <span class="toggle__label">Show export panel</span>
      </label>
    </div>
  </div>
{/if}

<style lang="scss">
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    z-index: 40;
  }

  .modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(92vw, 520px);
    border-radius: 1.5rem;
    background: #111;
    border: 1px solid #3f3f46;
    padding: 1.5rem;
    z-index: 50;
    color: #f4f4f5;
  }

  .modal__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
  }

  .modal__title {
    font-size: 1.125rem;
    font-weight: 600;
    margin: 0;
  }

  .modal__close {
    border: none;
    border-radius: 0.5rem;
    background: #1f2937;
    color: inherit;
    padding: 0.25rem 0.75rem;
    cursor: pointer;
  }

  .modal__close:hover,
  .modal__close:focus-visible {
    background: #374151;
  }

  .modal__content {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .toggle {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    font-size: 0.95rem;
  }

  .toggle__input {
    width: 1.25rem;
    height: 1.25rem;
  }

  .toggle__label {
    user-select: none;
  }
</style>
