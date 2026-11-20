import "./styles.css";
import { mount } from "svelte";
import App from "./App.svelte";
import { system } from "./lib/bridge";
import { setLanguage } from "./lib/i18n";
import { start, ui } from "./lib/state.svelte";
import { startTheme } from "./lib/theme";

async function main() {
  startTheme().then((theme) => (ui.theme = theme));
  try {
    setLanguage(await system.language());
  } catch {
    setLanguage(navigator.language || "ru");
  }
  // No browser context menu: the app has its own where it makes sense.
  document.addEventListener("contextmenu", (e) => {
    const target = e.target as HTMLElement;
    if (!target.closest("input, textarea, .selectable")) e.preventDefault();
  });
  start();
  mount(App, { target: document.getElementById("app")! });
}

main();
