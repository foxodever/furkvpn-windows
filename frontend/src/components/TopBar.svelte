<script lang="ts">
  import type { Snippet } from "svelte";
  import Icon from "./Icon.svelte";
  import { back } from "../lib/state.svelte";
  import { t } from "../lib/i18n";

  let { title, onback = back, actions }: { title: string; onback?: (() => void) | null; actions?: Snippet } = $props();
</script>

<div class="topbar">
  {#if onback}
    <button class="icon-btn interactive" title={t("back")} onclick={() => onback?.()}>
      <Icon name="arrow_back" />
    </button>
  {/if}
  <div class="title">{title}</div>
  {@render actions?.()}
</div>
