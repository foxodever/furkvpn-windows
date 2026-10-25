<script lang="ts">
  // "Прокси для Telegram": proxies from the API. A click hands the link to Telegram (tg://) or to the
  // browser (https://t.me/…, whose page opens Telegram), which offers to add the proxy.
  import Dialog from "../components/Dialog.svelte";
  import TelegramIcon from "../components/TelegramIcon.svelte";
  import { app } from "../lib/state.svelte";
  import { system } from "../lib/bridge";
  import { t } from "../lib/i18n";

  let { onclose }: { onclose: () => void } = $props();
</script>

<Dialog sheet title={t("tg_proxies_title")} {onclose}>
  <div class="body-medium muted hint">{t("tg_proxies_hint")}</div>
  {#each app.config.telegramProxies as proxy}
    <button class="list-item interactive" onclick={() => system.openURL(proxy.url)}>
      <span class="lead flag">
        {#if proxy.flag}<span class="title-large">{proxy.flag}</span>{:else}<span class="primary-text"><TelegramIcon /></span>{/if}
      </span>
      <span class="body-large grow">{proxy.title}</span>
      <span class="label-large primary-text">{t("tg_proxies_add")}</span>
    </button>
  {/each}
</Dialog>

<style>
  .hint {
    padding: 0 24px 8px;
  }
  .list-item {
    padding: 8px 24px;
  }
  .flag {
    justify-content: center;
  }
</style>
