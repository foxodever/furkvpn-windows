import { locale, t } from "./i18n";

/** "68,7 МБ", like Android's short file size: kilobytes are 1000 bytes. */
export function formatBytes(bytes: number): string {
  const units = locale() === "ru-RU" ? ["Б", "кБ", "МБ", "ГБ", "ТБ"] : ["B", "kB", "MB", "GB", "TB"];
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 900 && unit < units.length - 1) {
    value /= 1000;
    unit++;
  }
  const digits = unit === 0 || value >= 100 ? 0 : value >= 10 ? 1 : 2;
  const text = value.toLocaleString(locale(), { maximumFractionDigits: digits, minimumFractionDigits: 0 });
  return `${text} ${units[unit]}`;
}

export function formatSpeed(bytesPerSecond: number): string {
  return t("speed_format", formatBytes(bytesPerSecond));
}

/** "01:02:03" since connecting. */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(Math.floor(total / 3600))}:${pad(Math.floor((total % 3600) / 60))}:${pad(total % 60)}`;
}

/** "7 октября 2026 г." */
export function formatDate(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleDateString(locale(), { day: "numeric", month: "long", year: "numeric" });
}

/** "21:05:33" */
export function formatTime(ms: number): string {
  return new Date(ms).toLocaleTimeString(locale(), { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

/** "только что", "5 минут назад", "2 часа назад". */
export function relativeTime(ms: number, now = Date.now()): string {
  const diff = now - ms;
  if (diff < 60_000) return t("time_just_now");
  const rtf = new Intl.RelativeTimeFormat(locale(), { numeric: "auto" });
  const minutes = Math.round(diff / 60_000);
  if (minutes < 60) return rtf.format(-minutes, "minute");
  const hours = Math.round(diff / 3_600_000);
  if (hours < 24) return rtf.format(-hours, "hour");
  return rtf.format(-Math.round(diff / 86_400_000), "day");
}
