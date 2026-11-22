<script lang="ts">
  // Welcome screen (and "Сменить ключ"): sign in with Telegram, the website or a phone (its QR
  // code), paste or type the key.
  import Icon from "../components/Icon.svelte";
  import Logo from "../components/Logo.svelte";
  import TelegramIcon from "../components/TelegramIcon.svelte";
  import Dialog from "../components/Dialog.svelte";
  import TopBar from "../components/TopBar.svelte";
  import PhoneSignIn from "../components/PhoneSignIn.svelte";
  import { app, pasteKey, submitKey } from "../lib/state.svelte";
  import { system } from "../lib/bridge";
  import { t } from "../lib/i18n";

  let { canGoBack = false }: { canGoBack?: boolean } = $props();
  let showManual = $state(false);
  let showQr = $state(false);
  let manual = $state("");

  // The account page on the site: its "Войти в приложение FurkVPN" button adds the key here.
  const installUrl = $derived(`https://${app.config.site.domain}/dashboard/install`);
</script>

<div class="page">
  {#if canGoBack}
    <TopBar title="" />
  {/if}
  <div class="scroll">
    <!-- Under the top bar of "Сменить ключ" the logo is smaller, so the buttons fit without scrolling. -->
    <div class="wrap" class:compact={canGoBack}>
      <div class="hero">
        <Logo width={canGoBack ? 104 : 130} height={canGoBack ? 96 : 120} />
        <div class="headline-medium title">{t("onboarding_title")}</div>
        <div class="body-large muted">{t("onboarding_subtitle")}</div>
      </div>
      <div class="buttons">
        <button class="btn filled big wide interactive" onclick={() => system.openURL(app.config.telegramBotUrl)}>
          <TelegramIcon size={22} />
          {t("onboarding_telegram")}
        </button>
        <div class="body-small muted center tg-hint">{t("onboarding_telegram_hint")}</div>
        <button class="btn tonal large wide interactive" onclick={() => system.openURL(installUrl)}>
          <Icon name="language" />
          {t("onboarding_site")}
        </button>
        <button class="btn tonal large wide interactive" onclick={pasteKey}>
          <Icon name="content_paste" />
          {t("onboarding_paste")}
        </button>
        <button class="btn tonal large wide interactive" onclick={() => (showQr = true)}>
          <Icon name="qr_code_2" />
          {t("onboarding_qr")}
        </button>
        <button class="btn text interactive" onclick={() => (showManual = true)}>{t("onboarding_manual")}</button>
      </div>
    </div>
  </div>
</div>

{#if showQr}
  <PhoneSignIn
    onclose={() => (showQr = false)}
    onkey={(key) => {
      showQr = false;
      submitKey(key);
    }}
  />
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
    font-weight: 600;
  }
  .compact {
    padding-top: 0;
  }
  .compact .hero {
    padding: 0 0 20px;
  }
  .compact .title {
    margin-top: 20px;
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
</style>
