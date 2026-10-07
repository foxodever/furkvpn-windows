// The connection to FurkVPN.exe (Wails): calls to the service, the service's events, and what only
// the window's own process can do (clipboard, files, programs, autostart). In a plain browser
// (development) the same goes through the dev bridge (cmd/devbridge).
import type { Program, ServiceError } from "./types";

interface NativeApp {
  Call(method: string, params: string): Promise<string>;
  ReadClipboard(): Promise<string>;
  CopyText(text: string): Promise<void>;
  OpenURL(url: string): Promise<void>;
  SaveTextFile(name: string, content: string): Promise<boolean>;
  OpenTextFile(): Promise<string>;
  CopyFileToClipboard(name: string, content: string): Promise<void>;
  Programs(): Promise<Program[]>;
  PickProgram(): Promise<Program | null>;
  ProgramIcons(paths: string[]): Promise<Record<string, string>>;
  Autostart(): Promise<boolean>;
  SetAutostart(on: boolean): Promise<void>;
  Language(): Promise<string>;
  InstallUpdate(path: string): Promise<void>;
  Quit(): Promise<void>;
  Hide(): Promise<void>;
  StartService(): Promise<void>;
  OpenHotspotSettings(): Promise<void>;
  OpenLogFolder(): Promise<void>;
  Ready(): Promise<void>;
}

declare global {
  interface Window {
    go?: { main?: { App?: NativeApp } };
    runtime?: {
      EventsOn(name: string, cb: (...data: unknown[]) => void): () => void;
    };
  }
}

const native = (): NativeApp | undefined => window.go?.main?.App;

export class CallError extends Error {
  code: string;
  constructor(e: ServiceError) {
    super(e.message || e.code);
    this.code = e.code;
  }
}

function toError(e: unknown): CallError {
  if (e instanceof CallError) return e;
  const text = typeof e === "string" ? e : e instanceof Error ? e.message : String(e);
  try {
    const parsed = JSON.parse(text) as ServiceError;
    if (parsed && typeof parsed.code === "string") return new CallError(parsed);
  } catch {
    // not JSON
  }
  return new CallError({ code: "failed", message: text });
}

/** Sends a request to the service. */
export async function call<T = unknown>(method: string, params?: unknown): Promise<T> {
  const body = params === undefined ? "" : JSON.stringify(params);
  const app = native();
  try {
    if (app) {
      const out = await app.Call(method, body);
      return (out ? JSON.parse(out) : undefined) as T;
    }
    const resp = await fetch("/bridge/call", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ method, params: params ?? null }),
    });
    const text = await resp.text();
    if (!resp.ok) throw text;
    return (text ? JSON.parse(text) : undefined) as T;
  } catch (e) {
    throw toError(e);
  }
}

/** Subscribes to an event pushed by FurkVPN.exe: "state", "message", "service", "deeplink", "file". */
export function onEvent(name: string, cb: (data: any) => void): () => void {
  if (window.runtime) return window.runtime.EventsOn(name, (data) => cb(data));
  return devEvents(name, cb);
}

let source: EventSource | null = null;
const listeners = new Map<string, Set<(data: any) => void>>();

function devEvents(name: string, cb: (data: any) => void): () => void {
  if (!source) {
    source = new EventSource("/bridge/events");
    source.onmessage = (m) => {
      const { event, data } = JSON.parse(m.data);
      listeners.get(event)?.forEach((f) => f(data));
    };
  }
  if (!listeners.has(name)) listeners.set(name, new Set());
  listeners.get(name)!.add(cb);
  return () => listeners.get(name)?.delete(cb);
}

async function dev<T>(name: string, ...args: unknown[]): Promise<T> {
  const resp = await fetch("/bridge/sys/" + name, { method: "POST", body: JSON.stringify(args) });
  if (!resp.ok) throw toError(await resp.text());
  const text = await resp.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

function sys<K extends keyof NativeApp>(name: K): NativeApp[K] {
  return ((...args: unknown[]) => {
    const app = native();
    if (app) return (app[name] as (...a: unknown[]) => Promise<unknown>)(...args).catch((e) => Promise.reject(toError(e)));
    return dev(name, ...args);
  }) as NativeApp[K];
}

/** What the window's own process does. */
export const system = {
  readClipboard: sys("ReadClipboard"),
  copyText: sys("CopyText"),
  openURL: sys("OpenURL"),
  saveTextFile: sys("SaveTextFile"),
  openTextFile: sys("OpenTextFile"),
  copyFileToClipboard: sys("CopyFileToClipboard"),
  programs: sys("Programs"),
  pickProgram: sys("PickProgram"),
  programIcons: sys("ProgramIcons"),
  autostart: sys("Autostart"),
  setAutostart: sys("SetAutostart"),
  language: sys("Language"),
  installUpdate: sys("InstallUpdate"),
  quit: sys("Quit"),
  hide: sys("Hide"),
  startService: sys("StartService"),
  openHotspotSettings: sys("OpenHotspotSettings"),
  openLogFolder: sys("OpenLogFolder"),
  ready: sys("Ready"),
};
