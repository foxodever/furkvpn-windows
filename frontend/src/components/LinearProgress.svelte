<script lang="ts">
  /** value 0..1, or null for an indeterminate bar. */
  let { value = null }: { value?: number | null } = $props();
</script>

<div class="track" role="progressbar" aria-valuenow={value === null ? undefined : Math.round(value * 100)}>
  {#if value === null}
    <div class="bar indeterminate endless"></div>
  {:else}
    <div class="bar" style="width: {Math.max(0, Math.min(1, value)) * 100}%"></div>
  {/if}
</div>

<style>
  .track {
    height: 4px;
    border-radius: 2px;
    background: var(--surface-container-highest);
    overflow: hidden;
    position: relative;
  }
  .bar {
    height: 100%;
    border-radius: 2px;
    background: var(--primary);
    transition: width 200ms;
  }
  .indeterminate {
    position: absolute;
    width: 40%;
    animation: slide 1.6s ease-in-out infinite;
  }
  @keyframes slide {
    0% {
      left: -40%;
    }
    100% {
      left: 100%;
    }
  }
</style>
