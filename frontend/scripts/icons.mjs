// Collects the Material Symbols (rounded, filled where available) the app uses into
// src/lib/icons.ts, so the app ships only these few paths: npm run icons
import { readFileSync, writeFileSync, existsSync } from "node:fs";

const names = [
  "power_settings_new", "settings", "arrow_downward", "arrow_upward", "chevron_right", "speed", "refresh",
  "swap_horiz", "check", "language", "link", "support_agent", "install_desktop", "campaign", "cancel",
  "check_circle", "info", "close", "more_vert", "search", "add", "delete_sweep", "keyboard_arrow_down",
  "keyboard_arrow_up", "arrow_back", "key", "logout", "schedule", "apps", "wand_stars", "alt_route",
  "fact_check", "wifi_tethering", "receipt_long", "lock", "visibility", "visibility_off", "content_copy",
  "content_paste", "open_in_browser", "mail", "forum", "lan", "public", "shield", "block", "devices",
  "help", "desktop_windows", "laptop_mac", "mobile", "tv", "android", "save", "file_open", "folder_open",
  "computer", "bolt", "autorenew", "warning", "wifi", "settings_ethernet", "signal_cellular_alt", "router",
  "history", "minimize", "delete", "edit", "upload_file", "download", "hub", "error", "deployed_code",
  "keyboard", "dns", "remove", "drag_indicator", "open_in_new", "palette", "restart_alt", "lan",
  "tune", "vpn_lock", "straighten", "looks_6", "travel_explore", "connected_tv", "policy", "description",
];
const base = "node_modules/@material-symbols/svg-400/rounded/";
const out = {};
for (const name of [...new Set(names)]) {
  const file = existsSync(base + name + "-fill.svg") ? base + name + "-fill.svg" : base + name + ".svg";
  const svg = readFileSync(file, "utf8");
  const d = [...svg.matchAll(/ d="([^"]+)"/g)].map((m) => m[1]).join(" ");
  if (!d) throw new Error("no path in " + file);
  out[name] = d;
}
let ts = "// Material Symbols Rounded (Apache License 2.0), collected by scripts/icons.mjs. Do not edit.\n";
ts += "// viewBox: 0 -960 960 960\n";
ts += "export const icons = {\n";
for (const [k, v] of Object.entries(out)) ts += `  ${JSON.stringify(k)}: ${JSON.stringify(v)},\n`;
ts += "} as const;\n\nexport type IconName = keyof typeof icons;\n";
writeFileSync("src/lib/icons.ts", ts);
console.log(`icons: ${Object.keys(out).length}`);
