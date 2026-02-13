<script lang="ts">
  let { kind, title, color }: { kind: 'verified' | 'moderator'; title: string; color?: string } = $props();
</script>

<span
  class="badge-glyph"
  data-badge-glyph={kind}
  style={`color: ${color ?? 'currentColor'}`}
  role="img"
  aria-label={title}
  title={title}
>
  {#if kind === 'verified'}
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" focusable="false" aria-hidden="true">
      <path transform="scale(0.66)" d="M9 16.2L4.8 12l-1.4 1.4L9 19 21 7l-1.4-1.4L9 16.2z"></path>
    </svg>
  {:else}
    <svg
      xmlns="http://www.w3.org/2000/svg"
      height="24"
      viewBox="0 0 24 24"
      width="24"
      focusable="false"
      aria-hidden="true"
    >
      <path d="M3 4.998v9.857a6 6 0 003.365 5.39L12 23l5.635-2.755A6 6 0 0021 14.855V4.998a1 1 0 00-.656-.938L12 1 3.656 4.06A1 1 0 003 4.998Z"></path>
    </svg>
  {/if}
</span>

<style lang="scss">
  .badge-glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    margin-right: 5px;
    vertical-align: middle;
  }

  svg {
    width: 14px;
    height: 14px;
    fill: currentColor;
  }

  path {
    fill: currentColor;
  }
</style>
