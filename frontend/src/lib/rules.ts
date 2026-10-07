import type { RuleKind } from "./types";
import type { StringKey } from "./i18n";

/** Labels the buttons put into the text, so people only type the word itself. */
export const PREFIXES: { kind: RuleKind; prefix: string }[] = [
  { kind: "keyword", prefix: "keyword:" },
  { kind: "full", prefix: "full:" },
  { kind: "regexp", prefix: "regexp:" },
];

/**
 * The text with the prefix put over the selection start..end (on a new line unless the cursor
 * already starts an entry), and where the cursor goes: right after the prefix.
 */
export function insertPrefix(text: string, start: number, end: number, prefix: string): [string, number] {
  const before = text.slice(0, start);
  const separator = before === "" || "\n ,;\t".includes(before[before.length - 1]) ? "" : "\n";
  const inserted = separator + prefix;
  return [before + inserted + text.slice(end), start + inserted.length];
}

/** "слово" for keyword:, "точно" for full:, "regexp" for regexp:; nothing for sites and IPs. */
export function kindLabel(kind: RuleKind): StringKey | null {
  switch (kind) {
    case "keyword":
      return "routing_kind_keyword";
    case "full":
      return "routing_kind_full";
    case "regexp":
      return "routing_kind_regexp";
  }
  return null;
}
