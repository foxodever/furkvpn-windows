// What the service sends (internal/engine/state.go and friends).

export type VpnStatus = "idle" | "connecting" | "connected" | "stopping";

export interface VpnView {
  status: VpnStatus;
  since?: number;
  serverKey?: string;
  share?: { httpPort: number; socksPort: number; auth: boolean };
  /** Without TUN: the port of the proxy on 127.0.0.1. */
  localPort?: number;
}

export interface Traffic {
  up: number;
  down: number;
  totalUp: number;
  totalDown: number;
}

export type ServerKind = "server" | "auto" | "separator";

export interface ServerEntry {
  key: string;
  remarks: string;
  flag?: string;
  title: string;
  kind: ServerKind;
  summary?: string;
}

export interface SubscriptionInfo {
  url: string;
  title?: string;
  announce?: string;
  supportUrl?: string;
  updateIntervalHours?: number;
  upload?: number;
  download?: number;
  total?: number;
  expire?: number;
  fetchedAt: number;
  servers: ServerEntry[];
}

export interface SubscriptionSlice {
  info: SubscriptionInfo | null;
  refreshing: boolean;
  selected: string;
  /** "Продлить" / "Купить тариф": the site's payment page for this subscription. */
  renewUrl: string;
}

export interface Account {
  found: boolean;
  planId: number | null;
  planName?: string;
  expiresAt: number | null;
  isFree: boolean;
  status?: string;
  trafficUsed: number | null;
  trafficLimit: number | null;
  fetchedAt: number;
}

export interface SupportContact {
  type: string;
  title: string;
  url: string;
}

export interface AppLink {
  type: string;
  title?: string;
  url: string;
}

export interface UpdateInfo {
  versionCode: number;
  versionName: string;
  url: string;
  sha256: string;
  changelog?: string;
}

/** A proxy Telegram adds from its link (https://t.me/… or tg://…). */
export interface TelegramProxy {
  title: string;
  url: string;
  flag?: string;
}

export interface RemoteConfig {
  allowedSubscriptionHosts: string[];
  subscriptionMirrors: string[];
  telegramBotUrl: string;
  renewUrl: string;
  support: SupportContact[];
  links: AppLink[];
  site: { domain: string; apiDomain: string };
  telegramProxies: TelegramProxy[];
  serverFilters: { emoji: string; title: string }[];
  whitelist: {
    blockedProbes: string[];
    allowedProbes: string[];
    lteServer: string[];
    wifiServer: string[];
    bypassServer: string[];
    pingServers: string[];
  };
  delayTestUrl: string;
  windows: { update: UpdateInfo | null; defaultExcludedApps: string[] };
}

export interface ShareSettings {
  enabled: boolean;
  httpPort: number;
  socksPort: number;
  auth: boolean;
  user: string;
  password: string;
}

/** Settings → Расширенные настройки. */
export interface TunSettings {
  enabled: boolean;
  mtu: number;
  ipv6: boolean;
  strictDns: boolean;
  proxyPort: number;
  systemProxy: boolean;
}

export interface SettingsView {
  selectedServer: string;
  serversOnLeft: boolean;
  subscriptionUpdateHours: number;
  whitelistDetect: boolean;
  connectionLog: boolean;
  skippedUpdate: number;
  autoConnect: boolean;
  share: ShareSettings;
  tun: TunSettings;
}

export interface Exclusions {
  manual: string[];
  recommended: string[];
  useRecommended: boolean;
  overrides: string[];
  effective: string[];
  /** Only the programs in selected go through the VPN; the lists above wait for the usual mode. */
  onlySelected: boolean;
  selected: string[];
}

export type RuleKind = "domain" | "keyword" | "full" | "regexp" | "ip";
export type RouteAction = "direct" | "proxy" | "block";

export interface RuleView {
  entry: string;
  display: string;
  kind: RuleKind;
  editable: string;
}

export interface RoutingView {
  enabled: boolean;
  lanDirect: boolean;
  ruDirect: boolean;
  direct: RuleView[];
  proxy: RuleView[];
  block: RuleView[];
}

export type WhitelistStatus = "unknown" | "all_ok" | "whitelist" | "no_internet";
export type NetworkKind = "wifi" | "ethernet" | "mobile" | "hotspot" | "other" | "none";

export interface ProbeView {
  url: string;
  host: string;
  ms: number | null;
  error?: "timeout" | "dns" | "connect" | "tls" | "other";
}

export interface WhitelistView {
  status: WhitelistStatus;
  checking: boolean;
  lastCheck: {
    time: number;
    status: WhitelistStatus;
    network: NetworkKind;
    blocked: ProbeView[];
    allowed: ProbeView[];
  } | null;
}

export interface Ping {
  state: "testing" | "ok" | "failed";
  ms?: number;
}

export interface PingsView {
  pinging: boolean;
  results: Record<string, Ping>;
  whitelistOnly: boolean;
}

export interface UpdateView {
  available: UpdateInfo | null;
  progress: number | null;
  ready: boolean;
  error?: string;
}

export interface LanAddress {
  ip: string;
  kind: NetworkKind;
  name: string;
}

export interface Meta {
  version: string;
  versionCode: number;
  xray: string;
  os: string;
}

export interface LogEntry {
  host: string;
  port: number;
  udp: boolean;
  route: "vpn" | "direct" | "blocked";
  count: number;
  firstSeen: number;
  lastSeen: number;
}

export interface LogApp {
  key: string;
  path?: string;
  label: string;
  kind: "program" | "system" | "device" | "unknown";
  entries: LogEntry[];
  firstSeen: number;
}

export interface SettingsImport {
  kind: "routing" | "apps";
  routing?: { enabled: boolean; lanDirect: boolean; ruDirect: boolean; direct: string[]; proxy: string[]; block: string[] };
  apps?: { useRecommended: boolean; excluded: string[]; returnedToVpn: string[]; onlySelected: boolean; vpnApps: string[] };
  skipped: number;
}

/** A program on this computer, for "Programs without VPN". */
export interface Program {
  name: string; // exe file name, the key of the setting
  title: string;
  path: string;
  running: boolean;
}

export interface ServiceError {
  code: string;
  message?: string;
}
