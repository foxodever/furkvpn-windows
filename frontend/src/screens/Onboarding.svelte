<script lang="ts">
  // Welcome screen (and "Сменить ключ"): sign in with Telegram or the website, paste or type the key.
  import Icon from "../components/Icon.svelte";
  import Logo from "../components/Logo.svelte";
  import TelegramIcon from "../components/TelegramIcon.svelte";
  import Dialog from "../components/Dialog.svelte";
  import TopBar from "../components/TopBar.svelte";
  import { app, pasteKey, submitKey } from "../lib/state.svelte";
  import { system } from "../lib/bridge";
  import { t } from "../lib/i18n";

  let { canGoBack = false }: { canGoBack?: boolean } = $props();
  let showSite = $state(false);
  let showManual = $state(false);
  let emailTapped = $state(false);
  let manual = $state("");

  const installUrl = $derived(`https://${app.config.site.domain}/dashboard/install`);
</script>

<div class="page">
  {#if canGoBack}
    <TopBar title="" />
  {/if}
  <div class="scroll">
    <div class="wrap">
      <div class="hero">
        <Logo width={130} height={120} />
        <div class="headline-medium title">{t("onboarding_title")}</div>
        <div class="body-large muted">{t("onboarding_subtitle")}</div>
      </div>
      <div class="buttons">
        <button class="btn filled big wide interactive" onclick={() => system.openURL(app.config.telegramBotUrl)}>
          <TelegramIcon size={22} />
          {t("onboarding_telegram")}
        </button>
        <div class="body-small muted center tg-hint">{t("onboarding_telegram_hint")}</div>
        <button class="btn tonal large wide interactive" onclick={() => (showSite = true)}>
          <Icon name="language" />
          {t("onboarding_site")}
        </button>
        <button class="btn tonal large wide interactive" onclick={pasteKey}>
          <Icon name="content_paste" />
          {t("onboarding_paste")}
        </button>
        <button class="btn text interactive" onclick={() => (showManual = true)}>{t("onboarding_manual")}</button>
      </div>
    </div>
  </div>
</div>

{#if showSite}
  <Dialog sheet title={t("onboarding_site")} onclose={() => (showSite = false)}>
    <div class="body-medium muted hint">{t("site_login_hint")}</div>
    <button
      class="list-item three interactive"
      onclick={() => {
        system.openURL(installUrl);
        showSite = false;
      }}
    >
      <span class="lead primary"><Icon name="open_in_browser" /></span>
      <span class="grow col">
        <span class="body-large">{t("site_login_browser")}</span>
        <span class="body-medium muted">{t("site_login_browser_hint", app.config.site.domain)}</span>
      </span>
    </button>
    <button class="list-item interactive" onclick={() => (emailTapped = true)}>
      <span class="lead primary"><Icon name="mail" /></span>
      <span class="body-large grow">{t("site_login_email")}</span>
      {#if emailTapped}<span class="pill label-large" aria-live="polite">{t("in_development")}</span>{/if}
    </button>
  </Dialog>
{/if}

{#if showManual}
  <Dialog title={t("manual_title")} onclose={() => (showManual = false)}>
    <form
      onsubmit={(e) => {
        e.preventDefault();
        if (!manual.trim()) return;
        showManual = false;
        submitKey(manual);
      }}
    >
      <label class="field">
        <span class="field-label">{t("manual_label")}</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input bind:value={manual} placeholder={t("manual_placeholder")} autofocus spellcheck="false" />
      </label>
    </form>
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (showManual = false)}>{t("action_cancel")}</button>
      <button
        class="btn text interactive"
        disabled={!manual.trim()}
        onclick={() => {
          showManual = false;
          submitKey(manual);
        }}>{t("action_add")}</button
      >
    {/snippet}
  </Dialog>
{/if}

<style>
  .wrap {
    min-height: 100%;
    max-width: 480px;
    margin: 0 auto;
    padding: 24px;
    display: flex;
    flex-direction: column;
    justify-content: center;
  }
  .hero {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 24px 0 32px;
    color: var(--on-background);
  }
  .title {
    margin: 36px 0 8px;
  }
  .buttons {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
  }
  .tg-hint {
    margin: -4px 0 8px;
  }
  .center {
    text-align: center;
  }
  .hint {
    padding: 0 24px 8px;
  }
  .list-item {
    padding: 8px 24px;
  }
  .pill {
    padding: 4px 10px;
    border-radius: 8px;
    background: var(--secondary-container);
    color: var(--on-secondary-container);
  }
</style>
