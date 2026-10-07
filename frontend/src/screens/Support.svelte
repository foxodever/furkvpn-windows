<script lang="ts">
  import Dialog from "../components/Dialog.svelte";
  import Icon from "../components/Icon.svelte";
  import TelegramIcon from "../components/TelegramIcon.svelte";
  import { app } from "../lib/state.svelte";
  import { system } from "../lib/bridge";
  import { t } from "../lib/i18n";

  let { onclose }: { onclose: () => void } = $props();
</script>

<Dialog sheet title={t("support_title")} {onclose}>
  <div class="body-medium muted hint">{t("support_hint")}</div>
  {#each app.config.support as contact}
    <button class="list-item interactive" onclick={() => system.openURL(contact.url)}>
      <span class="lead primary">
        {#if contact.type === "telegram"}<TelegramIcon />{:else if contact.type === "vk"}<Icon name="forum" />{:else if contact.type === "email"}<Icon name="mail" />{:else}<Icon name="link" />{/if}
      </span>
      <span class="body-large grow">{contact.title}</span>
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
</style>
