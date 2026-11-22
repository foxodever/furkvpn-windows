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
  "qr_code_2", "cloud_off",
];
const base = "node_modules/@material-symbols/svg-400/rounded/";
// Drawn elsewhere (the Android app's icon), kept as it is.
const own = {
  dark_mode: "M480-120q-150 0-255-105T120-480q0-150 105-255t255-105q14 0 27.5 1t26.5 3q-41 29-65.5 75.5T444-660q0 90 63 153t153 63q55 0 101-24.5t75-65.5q2 13 3 26.5t1 27.5q0 150-105 255T480-120Zm0-80q88 0 158-48.5T740-375q-20 5-40 8t-40 3q-123 0-209.5-86.5T364-660q0-20 3-40t8-40q-78 32-126.5 102T200-480q0 116 82 198t198 82Zm-10-270Z",
};
const out = {};
for (const name of [...new Set(names)]) {
  const file = existsSync(base + name + "-fill.svg") ? base + name + "-fill.svg" : base + name + ".svg";
  const svg = readFileSync(file, "utf8");
  const d = [...svg.matchAll(/ d="([^"]+)"/g)].map((m) => m[1]).join(" ");
  if (!d) throw new Error("no path in " + file);
  out[name] = d;
}
Object.assign(out, own);
let ts = "// Material Symbols Rounded (Apache License 2.0), collected by scripts/icons.mjs. Do not edit.\n";
ts += "// viewBox: 0 -960 960 960\n";
ts += "export const icons = {\n";
for (const [k, v] of Object.entries(out)) ts += `  ${JSON.stringify(k)}: ${JSON.stringify(v)},\n`;
ts += "} as const;\n\nexport type IconName = keyof typeof icons;\n";
writeFileSync("src/lib/icons.ts", ts);
console.log(`icons: ${Object.keys(out).length}`);
