<script lang="ts">
  // Material circular progress indicator, indeterminate.
  let { size = 24, stroke = 3, class: cls = "" }: { size?: number; stroke?: number; class?: string } = $props();
  const r = $derived((size - stroke) / 2);
</script>

<svg class="spinner endless {cls}" width={size} height={size} viewBox="0 0 {size} {size}" role="progressbar">
  <circle cx={size / 2} cy={size / 2} {r} stroke-width={stroke} fill="none" />
</svg>

<style>
  .spinner {
    animation: rotate 1.4s linear infinite;
    color: var(--primary);
    flex-shrink: 0;
  }
  circle {
    stroke: currentColor;
    stroke-linecap: round;
    stroke-dasharray: 1, 200;
    stroke-dashoffset: 0;
    animation: dash 1.4s ease-in-out infinite;
    transform-origin: center;
  }
  @keyframes rotate {
    100% {
      transform: rotate(360deg);
    }
  }
  @keyframes dash {
    0% {
      stroke-dasharray: 1, 200;
      stroke-dashoffset: 0;
    }
    50% {
      stroke-dasharray: 89, 200;
      stroke-dashoffset: -35;
    }
    100% {
      stroke-dasharray: 89, 200;
      stroke-dashoffset: -124;
    }
  }
</style>
