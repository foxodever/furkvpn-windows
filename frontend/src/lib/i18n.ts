// Texts in the language of Windows: Russian for Russian-speaking systems, English otherwise.
import { en, ru, type StringKey } from "./strings";

export type { StringKey };

let lang: "ru" | "en" = navigator.language?.toLowerCase().startsWith("ru") ? "ru" : "en";
let plurals = new Intl.PluralRules(lang);

/** Sets the language from Windows ("ru-RU", "uk-UA", "en-US"…). Russian-speaking regions get Russian. */
export function setLanguage(tag: string) {
  const t = tag.toLowerCase();
  lang = /^(ru|be|uk|kk|ky|uz|tg|hy|az|ka)\b/.test(t) ? "ru" : "en";
  plurals = new Intl.PluralRules(lang);
  document.documentElement.lang = lang;
}

export function language() {
  return lang;
}

/** The locale for numbers and dates. */
export function locale() {
  return lang === "ru" ? "ru-RU" : "en-US";
}

function format(text: string, args: unknown[]): string {
  return text.replace(/\{(\d)\}/g, (_, i) => String(args[Number(i)] ?? ""));
}

/** A text with its {0}, {1}… arguments. */
export function t(key: StringKey, ...args: unknown[]): string {
  const value = (lang === "ru" ? ru : en)[key] ?? ru[key];
  if (typeof value !== "string") return key;
  return format(value, args);
}

/** A plural text ("3 дня", "5 адресов") for n, with n as {0}. */
export function plural(key: StringKey, n: number, ...args: unknown[]): string {
  const value = (lang === "ru" ? ru : en)[key] as unknown;
  if (typeof value !== "object" || value === null) return t(key, n, ...args);
  const forms = value as Record<string, string>;
  const form = forms[plurals.select(n)] ?? forms.other ?? Object.values(forms)[0];
  return format(form, [n, ...args]);
}
