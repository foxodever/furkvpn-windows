<script lang="ts">
  import Icon from "./Icon.svelte";
  import type { ServerKind } from "../lib/types";

  let { flag = "", kind = "server", size = 44 }: { flag?: string; kind?: ServerKind; size?: number } = $props();
</script>

<span class="avatar" style="width:{size}px;height:{size}px;font-size:{size * 0.5}px">
  {#if flag}
    <span class="flag">{flag}</span>
  {:else if kind === "auto"}
    <span class="primary-text"><Icon name="autorenew" size={size * 0.55} /></span>
  {:else}
    <span class="muted"><Icon name="public" size={size * 0.55} /></span>
  {/if}
</span>

<style>
  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    background: var(--surface-container-highest);
    flex-shrink: 0;
    line-height: 1;
  }
  .flag {
    line-height: 1;
  }
</style>
