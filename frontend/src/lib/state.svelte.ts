// The app's state: what the service last told us, plus what only the window knows (screens,
// dialogs, snackbars). Every part of the service's state arrives on its own when it changes.
import { call, CallError, onEvent, system } from "./bridge";
import { t, type StringKey } from "./i18n";
import type {
  Account, Exclusions, LanAddress, Meta, PingsView, RemoteConfig, RoutingView, SettingsImport,
  SettingsView, SubscriptionSlice, Traffic, UpdateView, VpnView, WhitelistView,
} from "./types";

export const defaultConfig: RemoteConfig = {
  allowedSubscriptionHosts: [],
  subscriptionMirrors: [],
  telegramBotUrl: "https://t.me/FurkVPNbot?start=app",
  renewUrl: "https://t.me/FurkVPNbot",
  support: [],
  links: [],
  site: { domain: "furkvpn.info", apiDomain: "api.furkvpn.info" },
  telegramProxies: [],
  serverFilters: [],
  whitelist: { blockedProbes: [], allowedProbes: [], lteServer: [], wifiServer: [], bypassServer: [], pingServers: [] },
  delayTestUrl: "",
  windows: { update: null, defaultExcludedApps: [] },
};

export type Screen = "home" | "onboarding" | "changeKey" | "settings" | "routing" | "apps" | "share" | "connlog" | "advanced";

export const app = $state({
  /** The service answers; false until the first state arrives and after the pipe breaks. */
  online: false,
  /** The state has arrived at least once. */
  loaded: false,
  vpn: { status: "idle" } as VpnView,
  traffic: { up: 0, down: 0, totalUp: 0, totalDown: 0 } as Traffic,
  subscription: { info: null, refreshing: false, selected: "", renewUrl: "" } as SubscriptionSlice,
  account: null as Account | null,
  config: defaultConfig,
  settings: null as SettingsView | null,
  exclusions: { manual: [], recommended: [], useRecommended: true, overrides: [], effective: [], onlySelected: false, selected: [] } as Exclusions,
  routing: { enabled: true, lanDirect: true, ruDirect: false, direct: [], proxy: [], block: [] } as RoutingView,
  whitelist: { status: "unknown", checking: false, lastCheck: null } as WhitelistView,
  pings: { pinging: false, results: {}, whitelistOnly: false } as PingsView,
  update: { available: null, progress: null, ready: false } as UpdateView,
  lan: [] as LanAddress[] | null,
  meta: { version: "", versionCode: 0, xray: "", os: "" } as Meta,
});

/** What only the window keeps. */
export const ui = $state({
  stack: [] as Screen[],
  /** Emoji of the selected server tab, "" for "All". */
  serverFilter: "",
  snackbar: null as { text: string; id: number } | null,
  /** A key problem, shown as a dialog with a shortcut to the bot. */
  keyError: null as StringKey | null,
  importing: false,
  confirmReplace: null as string | null,
  /** A TV's link was opened: send it this computer's key? (its code) */
  tvSend: null as string | null,
  pendingImport: null as { file: SettingsImport; content: string } | null,
  autostart: false,
  dragging: false,
});

export function screen(): Screen {
  return ui.stack[ui.stack.length - 1] ?? (app.subscription.info ? "home" : "onboarding");
}

export function open(s: Screen) {
  ui.stack.push(s);
}

export function back() {
  ui.stack.pop();
}

let snackbarId = 0;
let snackbarTimer: ReturnType<typeof setTimeout> | undefined;

export function snackbar(text: string) {
  ui.snackbar = { text, id: ++snackbarId };
  clearTimeout(snackbarTimer);
  snackbarTimer = setTimeout(() => (ui.snackbar = null), 4000);
}

const messageTexts: Record<string, StringKey> = {
  whitelist_switched_on: "whitelist_switched_on",
  whitelist_switched_off: "whitelist_switched_off",
  vpn_error: "vpn_error",
};

/** The settings from furkapp.net over the built-in ones: a field that came as null keeps its default. */
function configOf(value: unknown): RemoteConfig {
  const config: Record<string, unknown> = { ...defaultConfig };
  if (value && typeof value === "object") {
    for (const [key, v] of Object.entries(value)) {
      if (v != null) config[key] = v;
    }
  }
  return config as unknown as RemoteConfig;
}

export function start() {
  onEvent("state", (data: Record<string, unknown>) => {
    for (const [key, value] of Object.entries(data)) {
      (app as Record<string, unknown>)[key] = key === "config" ? configOf(value) : (value ?? null);
    }
    app.online = true;
    app.loaded = true;
  });
  onEvent("message", (m: { code: string; args?: string[] }) => {
    const key = messageTexts[m.code];
    if (key) snackbar(t(key, ...(m.args ?? [])));
  });
  // When the service comes back, its whole state follows right after it accepts the window.
  onEvent("service", (s: { online: boolean }) => (app.online = s.online));
  onEvent("deeplink", (text: string) => submitKey(text));
  onEvent("file", (content: string) => inspectSettingsFile(content));
  system.autostart().then((on) => (ui.autostart = on)).catch(() => {});
  system.ready().catch(() => {});
}

// --- keys --------------------------------------------------------------------------------

const keyErrors: Record<string, StringKey> = {
  empty: "error_empty_clipboard",
  not_a_key: "error_not_a_key",
  foreign_key: "error_foreign_key",
  not_found: "error_not_found",
  bad_format: "error_bad_format",
  no_servers: "error_no_servers",
  download: "error_download",
  tv_need_key: "tv_send_need_key",
};

function keyError(e: unknown): StringKey {
  return e instanceof CallError ? (keyErrors[e.code] ?? "error_download") : "error_download";
}

export async function submitKey(text: string | null | undefined) {
  if (ui.importing) return;
  ui.importing = true;
  try {
    const r = await call<{ result: string; url: string; code?: string }>("submitKey", { text: text ?? "" });
    if (r.result === "confirm") {
      ui.confirmReplace = r.url;
    } else if (r.result === "tv") {
      ui.tvSend = r.code ?? null;
    } else {
      if (r.result === "imported") snackbar(t("key_added"));
      if (r.result === "refreshed") snackbar(t("servers_updated"));
      ui.stack = [];
    }
  } catch (e) {
    ui.keyError = keyError(e);
  } finally {
    ui.importing = false;
  }
}

export async function confirmReplace() {
  const url = ui.confirmReplace;
  ui.confirmReplace = null;
  if (!url) return;
  ui.importing = true;
  try {
    await call("importKey", { url });
    snackbar(t("key_added"));
    ui.stack = [];
  } catch (e) {
    ui.keyError = keyError(e);
  } finally {
    ui.importing = false;
  }
}

const tvErrors: Record<string, StringKey> = {
  tv_no_code: "tv_send_no_code",
  tv_too_many: "tv_send_too_many",
  tv_bad_code: "tv_bad_code",
  tv_need_key: "tv_send_need_key",
  foreign_key: "error_foreign_key",
};

/** Sends this computer's key to the TV showing code (Settings → «Подключить телевизор», or its link). */
export async function sendTvKey(code: string) {
  try {
    await call("sendTvKey", { code });
    snackbar(t("tv_sent"));
  } catch (e) {
    snackbar(t(e instanceof CallError ? (tvErrors[e.code] ?? "tv_send_failed") : "tv_send_failed"));
  }
}

export async function pasteKey() {
  const text = await system.readClipboard().catch(() => "");
  await submitKey(text);
}

export async function refreshSubscription(quiet = false) {
  try {
    await call("refreshSubscription");
    if (!quiet) snackbar(t("servers_updated"));
  } catch (e) {
    snackbar(t(keyError(e)));
  }
}

// --- settings files ----------------------------------------------------------------------

export async function inspectSettingsFile(content: string) {
  try {
    const file = await call<SettingsImport>("inspectSettingsFile", { content });
    ui.pendingImport = { file, content };
  } catch {
    snackbar(t("import_error_format"));
  }
}

export async function applyImport(replace: boolean) {
  const pending = ui.pendingImport;
  ui.pendingImport = null;
  if (!pending) return;
  try {
    await call("importSettingsFile", { content: pending.content, replace });
    snackbar(t(pending.file.kind === "routing" ? "import_done_routing" : "import_done_apps"));
  } catch {
    snackbar(t("import_error_format"));
  }
}

/** Runs a request and shows its failure in a snackbar. */
export async function act(method: string, params?: unknown) {
  try {
    return await call(method, params);
  } catch (e) {
    snackbar(t("error_generic", e instanceof Error ? e.message : String(e)));
  }
}

export function setSetting(key: string, value: unknown) {
  return act("set", { key, value });
}
