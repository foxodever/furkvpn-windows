<script lang="ts">
  import Home from "./screens/Home.svelte";
  import Onboarding from "./screens/Onboarding.svelte";
  import Settings from "./screens/Settings.svelte";
  import Routing from "./screens/Routing.svelte";
  import Apps from "./screens/Apps.svelte";
  import Share from "./screens/Share.svelte";
  import ConnectionLog from "./screens/ConnectionLog.svelte";
  import Advanced from "./screens/Advanced.svelte";
  import Dialog from "./components/Dialog.svelte";
  import Spinner from "./components/Spinner.svelte";
  import Icon from "./components/Icon.svelte";
  import Logo from "./components/Logo.svelte";
  import { app, ui, screen, back, confirmReplace, applyImport, inspectSettingsFile, sendTvKey } from "./lib/state.svelte";
  import { system } from "./lib/bridge";
  import { t } from "./lib/i18n";

  const current = $derived(screen());
  let startingService = $state(false);
  let serviceFailed = $state(false);

  // Adding or removing the key switches between onboarding and the main screen.
  $effect(() => {
    if (app.loaded && !app.subscription.info && ui.stack.some((s) => s !== "changeKey")) {
      ui.stack = ui.stack.filter((s) => s === "changeKey");
    }
  });

  const offerBot = $derived(ui.keyError === "error_foreign_key" || ui.keyError === "error_not_found");

  async function startService() {
    startingService = true;
    serviceFailed = false;
    try {
      await system.startService();
    } catch {
      serviceFailed = true;
    } finally {
      setTimeout(() => (startingService = false), 3000);
    }
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === "Escape" && ui.stack.length > 0 && !document.querySelector("[role=dialog]")) back();
  }

  function mouseup(e: MouseEvent) {
    // The mouse's "back" button.
    if (e.button === 3 && ui.stack.length > 0) {
      e.preventDefault();
      back();
    }
  }

  // Settings files dropped onto the window.
  function drop(e: DragEvent) {
    e.preventDefault();
    ui.dragging = false;
    const file = e.dataTransfer?.files?.[0];
    if (!file || file.size > 5 * 1024 * 1024) return;
    file.text().then(inspectSettingsFile).catch(() => {});
  }
</script>

<svelte:window onkeydown={keydown} onmouseup={mouseup} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<main
  ondragover={(e) => {
    if (e.dataTransfer?.types.includes("Files")) {
      e.preventDefault();
      ui.dragging = true;
    }
  }}
  ondragleave={(e) => {
    if (e.target === e.currentTarget) ui.dragging = false;
  }}
  ondrop={drop}
>
  {#if !app.loaded}
    <div class="splash">
      <Logo width={130} height={120} />
      {#if !app.online}{@render serviceDown()}{:else}<Spinner />{/if}
    </div>
  {:else if current === "home"}
    <Home />
  {:else if current === "onboarding"}
    <Onboarding />
  {:else if current === "changeKey"}
    <Onboarding canGoBack />
  {:else if current === "settings"}
    <Settings />
  {:else if current === "routing"}
    <Routing />
  {:else if current === "apps"}
    <Apps />
  {:else if current === "share"}
    <Share />
  {:else if current === "connlog"}
    <ConnectionLog />
  {:else if current === "advanced"}
    <Advanced />
  {/if}

  {#if app.loaded && !app.online}
    <div class="offline">{@render serviceDown()}</div>
  {/if}

  {#if ui.dragging}
    <div class="drop"><Icon name="upload_file" size={48} /><span class="title-medium">{t("import_drop")}</span></div>
  {/if}

  {#if ui.snackbar}
    {#key ui.snackbar.id}
      <div class="snackbar body-medium" role="status">{ui.snackbar.text}</div>
    {/key}
  {/if}
</main>

{#snippet serviceDown()}
  <div class="service card pad">
    <div class="row gap"><span class="error-text"><Icon name="warning" /></span><span class="title-medium">{t("service_down_title")}</span></div>
    <div class="body-medium muted">{t("service_down_text")}</div>
    {#if serviceFailed}<div class="body-small error-text">{t("service_start_failed")}</div>{/if}
    <button class="btn filled interactive" disabled={startingService} onclick={startService}>
      {#if startingService}<Spinner size={18} stroke={2} />{t("service_starting")}{:else}{t("service_start")}{/if}
    </button>
  </div>
{/snippet}

{#if ui.importing}
  <Dialog persistent>
    <div class="row gap loading"><Spinner size={28} /><span class="body-large">{t("importing")}</span></div>
  </Dialog>
{/if}

{#if ui.tvSend}
  <Dialog title={t("tv_send_title")} onclose={() => (ui.tvSend = null)}>
    {t("tv_send_text", ui.tvSend.slice(0, 3) + " " + ui.tvSend.slice(3))}
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (ui.tvSend = null)}>{t("action_cancel")}</button>
      <button
        class="btn text interactive"
        onclick={() => {
          const code = ui.tvSend; // read before the dialog closes
          ui.tvSend = null;
          if (code) sendTvKey(code);
        }}>{t("tv_send_action")}</button
      >
    {/snippet}
  </Dialog>
{/if}

{#if ui.confirmReplace}
  <Dialog title={t("replace_key_title")} onclose={() => (ui.confirmReplace = null)}>
    {t("replace_key_text")}
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (ui.confirmReplace = null)}>{t("action_cancel")}</button>
      <button class="btn text interactive" onclick={confirmReplace}>{t("action_replace")}</button>
    {/snippet}
  </Dialog>
{/if}

{#if ui.keyError}
  <Dialog onclose={() => (ui.keyError = null)}>
    <span class="body-medium">{t(ui.keyError)}</span>
    {#snippet actions()}
      {#if offerBot}
        <button class="btn text interactive" onclick={() => (ui.keyError = null)}>{t("action_close")}</button>
        <button
          class="btn text interactive"
          onclick={() => {
            ui.keyError = null;
            system.openURL(app.config.telegramBotUrl);
          }}>{t("action_open_bot")}</button
        >
      {:else}
        <button class="btn text interactive" onclick={() => (ui.keyError = null)}>{t("action_close")}</button>
      {/if}
    {/snippet}
  </Dialog>
{/if}

{#if ui.pendingImport}
  {@const f = ui.pendingImport.file}
  <Dialog title={t(f.kind === "routing" ? "import_routing_title" : "import_apps_title")} onclose={() => (ui.pendingImport = null)}>
    <p>
      {#if f.kind === "routing" && f.routing}
        {t("import_routing_text", f.routing.direct.length + f.routing.proxy.length + f.routing.block.length, f.routing.direct.length, f.routing.proxy.length, f.routing.block.length)}
      {:else if f.apps}
        {t("import_apps_text", f.apps.excluded.length)}
      {/if}
    </p>
    {#if f.skipped > 0}<p>{t("import_skipped", f.skipped)}</p>{/if}
    <p>{t("import_question")}</p>
    {#snippet actions()}
      <button class="btn text interactive" onclick={() => (ui.pendingImport = null)}>{t("action_cancel")}</button>
      <button class="btn text interactive" onclick={() => applyImport(false)}>{t("import_merge")}</button>
      <button class="btn text interactive" onclick={() => applyImport(true)}>{t("import_replace")}</button>
    {/snippet}
  </Dialog>
{/if}

<style>
  main {
    height: 100%;
    position: relative;
  }
  .splash {
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 32px;
    padding: 24px;
  }
  .offline {
    position: absolute;
    inset: 0;
    background: color-mix(in srgb, var(--background) 85%, transparent);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    z-index: 50;
  }
  .service {
    max-width: 420px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    align-items: flex-start;
  }
  .gap {
    gap: 12px;
  }
  .loading {
    padding: 4px 0 24px;
    color: var(--on-surface);
  }
  .drop {
    position: absolute;
    inset: 12px;
    border: 2px dashed var(--primary);
    border-radius: 28px;
    background: color-mix(in srgb, var(--primary-container) 85%, transparent);
    color: var(--on-primary-container);
    display: flex;
    flex-direction: column;
    gap: 12px;
    align-items: center;
    justify-content: center;
    z-index: 300;
    pointer-events: none;
  }
  .snackbar {
    position: fixed;
    left: 50%;
    bottom: 16px;
    transform: translateX(-50%);
    max-width: min(560px, calc(100% - 32px));
    padding: 14px 16px;
    border-radius: 4px;
    background: var(--inverse-surface);
    color: var(--inverse-on-surface);
    box-shadow: 0 3px 8px rgba(0, 0, 0, 0.3);
    z-index: 400;
    animation: rise 200ms cubic-bezier(0.2, 0, 0, 1);
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translate(-50%, 8px);
    }
  }
  p {
    margin: 0 0 12px;
  }
</style>
