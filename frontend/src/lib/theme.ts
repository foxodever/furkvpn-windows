// The dark theme, as in the Android app: as in Windows, always on or always off. The choice is
// kept by the window's process (it paints the title bar to match); a copy in localStorage lets
// index.html paint the right colors before anything else loads.
import { system } from "./bridge";

export type Theme = "system" | "dark" | "light";

const systemDark = window.matchMedia("(prefers-color-scheme: dark)");
let current: Theme = "system";

function paint() {
  const dark = current === "dark" || (current === "system" && systemDark.matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
}

function remember(theme: Theme) {
  current = theme;
  try {
    localStorage.setItem("theme", theme);
  } catch {
    // no storage: the window's process still remembers
  }
  paint();
}

/** Applies the saved choice and follows Windows while the choice is "as in Windows". */
export async function startTheme(): Promise<Theme> {
  systemDark.addEventListener("change", paint);
  try {
    const saved = await system.theme();
    remember(saved === "dark" || saved === "light" ? saved : "system");
  } catch {
    paint();
  }
  return current;
}

export async function setTheme(theme: Theme) {
  await system.setTheme(theme);
  remember(theme);
}
