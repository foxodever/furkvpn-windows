<script lang="ts">
  // Signing in from a phone: the computer shows a code and its QR code the way a TV with FurkVPN
  // does, and the phone sends its key for it ("Подключить телевизор" in the app, or the page the
  // QR code opens). The code is asked for when the dialog opens and waited on while it is open; a
  // code that ran out is replaced only on request, so a forgotten dialog stops asking the server.
  import { onMount } from "svelte";
  import Dialog from "./Dialog.svelte";
  import Icon from "./Icon.svelte";
  import QrCode from "./QrCode.svelte";
  import Spinner from "./Spinner.svelte";
  import { call, CallError } from "../lib/bridge";
  import { t } from "../lib/i18n";
  import type { StringKey } from "../lib/strings";

  let { onkey, onclose }: { onkey: (key: string) => void; onclose: () => void } = $props();

  type Session = { code: string; token: string; url: string; expiresIn: number };
  type Pairing = { state: "loading" } | { state: "waiting"; session: Session } | { state: "failed"; text: StringKey } | { state: "expired" };

  let pairing = $state<Pairing>({ state: "loading" });
  // The dialog closing, or asking for another code, ends the wait for the one before.
  let current = 0;

  const sleep = (ms: number) => new Promise((done) => setTimeout(done, ms));
  const errorCode = (e: unknown) => (e instanceof CallError ? e.code : "");

  async function start() {
    const mine = ++current;
    pairing = { state: "loading" };
    let session: Session;
    try {
      session = await call<Session>("tvSession");
    } catch (e) {
      if (mine === current) pairing = { state: "failed", text: errorCode(e) === "tv_too_many" ? "tv_send_too_many" : "qr_offline" };
      return;
    }
    if (mine !== current) return;
    pairing = { state: "waiting", session };
    const until = Date.now() + session.expiresIn * 1000;
    while (Date.now() < until) {
      const asked = Date.now();
      let key = "";
      let failure = "";
      try {
        // The server holds the request for about 10 seconds.
        key = (await call<{ key: string }>("tvWait", { code: session.code, token: session.token })).key;
      } catch (e) {
        failure = errorCode(e) || "failed";
      }
      // The phone was told the key went through: it is taken even if the dialog has just closed.
      if (key) return onkey(key);
      if (mine !== current) return;
      if (failure === "tv_expired") break;
      // A network hiccup keeps the code.
      if (failure) await sleep(failure === "tv_too_many" ? 10_000 : 3_000);
      else if (Date.now() - asked < 1000) await sleep(1000);
      if (mine !== current) return;
    }
    pairing = { state: "expired" };
  }

  onMount(() => {
    start();
    return () => {
      current++;
    };
  });

  /** "https://furkapp.net/tv/ABC123" -> "furkapp.net/tv": where the code is typed. */
  const page = (session: Session) => session.url.replace(/^https?:\/\//, "").replace(/\/[^/]*$/, "");
  const spaced = (code: string) => (code.length === 6 ? code.slice(0, 3) + " " + code.slice(3) : code);
</script>

<Dialog title={t("qr_title")} width={660} {onclose}>
  <div class="pairing">
    <div class="steps">
      <div class="step"><span class="number label-large">1</span><span>{t("qr_step_scan")}</span></div>
      <div class="step"><span class="number label-large">2</span><span>{t("qr_step_send")}</span></div>
      {#if pairing.state === "waiting"}
        <div class="or">{t("qr_or_code", page(pairing.session))}</div>
        <div class="code selectable">{spaced(pairing.session.code)}</div>
      {/if}
    </div>
    <div class="box">
      {#if pairing.state === "loading"}
        <Spinner />
      {:else if pairing.state === "waiting"}
        <QrCode text={pairing.session.url} label={t("qr_title")} />
      {:else}
        <span class="status-icon"><Icon name={pairing.state === "expired" ? "history" : "cloud_off"} size={48} /></span>
        <div class="title-medium status">{t(pairing.state === "expired" ? "qr_expired" : pairing.text)}</div>
        <button class="btn tonal interactive" onclick={start}>
          <Icon name="refresh" size={18} />
          {t(pairing.state === "expired" ? "qr_new_code" : "action_retry")}
        </button>
      {/if}
    </div>
  </div>
  {#snippet actions()}
    <button class="btn text interactive" onclick={onclose}>{t("action_close")}</button>
  {/snippet}
</Dialog>

<style>
  .pairing {
    display: flex;
    align-items: center;
    gap: 32px;
    color: var(--on-surface);
  }
  .steps {
    flex: 1 1 0;
    min-width: 0;
  }
  .step {
    display: flex;
    gap: 12px;
    margin-bottom: 12px;
    font-size: 16px;
    line-height: 24px;
  }
  .number {
    flex: none;
    width: 28px;
    height: 28px;
    margin-top: -2px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--secondary-container);
    color: var(--on-secondary-container);
  }
  .or {
    margin-top: 20px;
    color: var(--on-surface-variant);
  }
  .code {
    margin-top: 4px;
    font-size: 36px;
    line-height: 44px;
    font-weight: 500;
    letter-spacing: 4px;
    color: var(--primary);
    width: fit-content;
  }
  .box {
    flex: none;
    width: 208px;
    height: 208px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    text-align: center;
  }
  .status-icon {
    color: var(--primary);
    display: flex;
  }
  .status {
    color: var(--on-surface);
  }
  /* A narrow window: the QR code first, the steps under it. */
  @media (max-width: 640px) {
    .pairing {
      flex-direction: column-reverse;
      align-items: stretch;
      gap: 20px;
    }
    .box {
      align-self: center;
    }
    .or {
      margin-top: 12px;
    }
  }
</style>
