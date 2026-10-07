<script lang="ts">
  import { icons, type IconName } from "../lib/icons";

  let { name, size = 24, class: cls = "" }: { name: IconName; size?: number; class?: string } = $props();
</script>

<svg class={cls} width={size} height={size} viewBox="0 -960 960 960" aria-hidden="true" fill="currentColor">
  <path d={icons[name]} />
</svg>

<style>
  svg {
    flex-shrink: 0;
    display: block;
  }
</style>
