<script lang="ts">
  let { checked }: { checked: boolean } = $props();
</script>

<span class="radio" class:on={checked} aria-hidden="true"><span class="dot"></span></span>

<style>
  .radio {
    width: 20px;
    height: 20px;
    border-radius: 50%;
    border: 2px solid var(--on-surface-variant);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .radio.on {
    border-color: var(--primary);
  }
  .dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--primary);
    transform: scale(0);
    transition: transform 120ms;
  }
  .on .dot {
    transform: scale(1);
  }
</style>
