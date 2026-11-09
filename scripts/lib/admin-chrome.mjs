// Headless Chrome over the DevTools protocol for the admin browser journeys,
// using Node's built-in WebSocket (no browser-automation package). Shared by
// scripts/test-admin-browser-flow.mjs and scripts/test-admin-product-flow.mjs.
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function fail(message) {
  throw new Error(`browser: ${message}`);
}

export async function launchChrome() {
  const profile = await mkdtemp(join(tmpdir(), "admin-chrome-"));
  const args = [
    "--headless=new", "--remote-debugging-port=0", `--user-data-dir=${profile}`, "--no-first-run",
    "--no-default-browser-check", "--disable-extensions", "--disable-gpu", "--window-size=1280,900", "about:blank",
  ];
  // CI runners restrict unprivileged user namespaces; the browser only visits this stack.
  if (process.env.CI) args.unshift("--no-sandbox");
  // Only for the throwaway self-signed certificate of with-admin-tls-proxy.sh.
  if (process.env.CHROME_IGNORE_CERTIFICATE_ERRORS === "1") args.unshift("--ignore-certificate-errors");
  const candidates = process.env.CHROME ? [process.env.CHROME] : ["google-chrome", "chromium", "chromium-browser"];
  for (const binary of candidates) {
    const child = spawn(binary, args, { stdio: "ignore" });
    const started = await new Promise((resolve) => {
      child.once("error", () => resolve(false));
      child.once("spawn", () => resolve(true));
    });
    if (!started) continue;
    for (let i = 0; i < 100; i++) {
      try {
        const [port] = (await readFile(join(profile, "DevToolsActivePort"), "utf8")).split("\n");
        return { child, profile, port: Number(port) };
      } catch {
        await sleep(100);
      }
    }
    child.kill("SIGKILL");
  }
  await rm(profile, { recursive: true, force: true });
  fail("could not start Chrome");
}

// Bounds of what one page remembers for a timeout report.
const MAX_EVENTS = 20;
const MAX_IN_FLIGHT = 50;
const MAX_TEXT = 300;

// Keeps only the origin and path: queries and fragments can carry tokens.
function safeUrl(value) {
  try {
    const url = new URL(value);
    return /^https?:$/.test(url.protocol) ? `${url.origin}${url.pathname}` : `[${url.protocol} URL]`;
  } catch {
    return "[unparsable URL]";
  }
}

function safeText(value) {
  return String(value)
    .replace(/https?:\/\/[^\s"'<>]+/g, (url) => safeUrl(url))
    .replace(/\bbearer\s+[\w.~+/=-]+/gi, "Bearer [redacted]")
    .replace(/\b(password|passwd|token|csrf|authorization|cookie|secret|session|sweetshop_\w*)([\w-]*["']?\s*[:=]\s*["']?)(?!Bearer \[redacted\])[^\s"',;&]+/gi, "$1$2[redacted]")
    .slice(0, MAX_TEXT);
}

// What a page saw that explains a timeout: failures and unfinished requests
// only, never headers or bodies. Recorded as events arrive, printed on timeout.
class PageDiagnostics {
  constructor() {
    this.startedAt = Date.now();
    this.navigatedAt = null;
    this.exceptions = [];
    this.console = [];
    this.failed = [];
    this.http = [];
    this.inFlight = new Map();
  }

  age() {
    return Date.now() - this.startedAt;
  }

  add(list, text) {
    list.push(`+${this.age()}ms ${text}`);
    if (list.length > MAX_EVENTS) list.shift();
  }

  record({ method, params }) {
    switch (method) {
      case "Runtime.exceptionThrown":
        this.add(this.exceptions, safeText(params.exceptionDetails.exception?.description ?? params.exceptionDetails.text));
        break;
      case "Runtime.consoleAPICalled":
        if (params.type === "error" || params.type === "assert") {
          this.add(this.console, safeText(params.args.map((arg) => arg.value ?? arg.description ?? arg.type).join(" ")));
        }
        break;
      case "Log.entryAdded":
        if (params.entry.level === "error") {
          this.add(this.console, safeText(`${params.entry.source}: ${params.entry.text}${params.entry.url ? ` (${params.entry.url})` : ""}`));
        }
        break;
      case "Page.frameNavigated":
        if (!params.frame.parentId) this.navigatedAt = this.age();
        break;
      case "Network.requestWillBeSent":
        this.inFlight.set(params.requestId, { at: this.age(), method: params.request.method, type: params.type, url: safeUrl(params.request.url) });
        if (this.inFlight.size > MAX_IN_FLIGHT) this.inFlight.delete(this.inFlight.keys().next().value);
        break;
      case "Network.responseReceived": {
        const request = this.inFlight.get(params.requestId);
        if (request) request.status = params.response.status;
        if (params.response.status >= 400) this.add(this.http, `${params.response.status} ${params.type} ${safeUrl(params.response.url)}`);
        break;
      }
      case "Network.loadingFinished":
        this.inFlight.delete(params.requestId);
        break;
      case "Network.loadingFailed": {
        const request = this.inFlight.get(params.requestId);
        this.inFlight.delete(params.requestId);
        // A cancelled request is a navigation or an aborted fetch, not a failure.
        if (!params.canceled) {
          this.add(this.failed, `${request?.method ?? "?"} ${params.type} ${request?.url ?? "[URL not observed]"} ${safeText(params.errorText)}${params.blockedReason ? ` (${params.blockedReason})` : ""}`);
        }
        break;
      }
    }
  }

  report(state) {
    const now = this.age();
    const section = (title, entries) => [`  ${title}: ${entries.length || "none"}`, ...entries.map((entry) => `    ${entry}`)];
    const inFlight = [...this.inFlight.values()].slice(-MAX_EVENTS).map((request) => `${now - request.at}ms ago ${request.method} ${request.type} ${request.url} (${request.status ? `response ${request.status}, body unfinished` : "no response"})`);
    return [
      `browser diagnostics (at +${now}ms, last navigation +${this.navigatedAt ?? "?"}ms, page ${state ?? "state unavailable"}):`,
      ...section("uncaught exceptions", this.exceptions),
      ...section("console and browser errors", this.console),
      ...section("failed requests", this.failed),
      ...section("HTTP errors", this.http),
      ...section("unfinished requests", inFlight),
    ].join("\n");
  }
}

// Evaluated only after a timeout; reports whether React hydrated anything.
const PAGE_STATE = `JSON.stringify({
  url: location.origin + location.pathname,
  readyState: document.readyState,
  visibility: document.visibilityState,
  focused: document.hasFocus(),
  nextScripts: document.querySelectorAll('script[src*="/_next/"]').length,
  flightChunks: self.__next_f?.length ?? null,
  hydratedElements: [...document.querySelectorAll("body *")].filter((element) => Object.keys(element).some((key) => key.startsWith("__reactProps"))).length,
})`;

export class Page {
  constructor(socket) {
    this.socket = socket;
    this.nextId = 1;
    this.pending = new Map();
    this.listeners = new Set();
    this.diagnostics = new PageDiagnostics();
    socket.addEventListener("message", (event) => {
      const message = JSON.parse(event.data);
      if (message.id && this.pending.has(message.id)) {
        const { resolve, reject } = this.pending.get(message.id);
        this.pending.delete(message.id);
        if (message.error) reject(new Error(message.error.message));
        else resolve(message.result);
      } else if (message.method) {
        // A report must never be able to disturb the journey it observes.
        try {
          this.diagnostics.record(message);
        } catch {}
        for (const listener of this.listeners) listener(message);
      }
    });
  }

  static async open(port) {
    const target = await (await fetch(`http://127.0.0.1:${port}/json/new?about:blank`, { method: "PUT" })).json();
    const socket = new WebSocket(target.webSocketDebuggerUrl);
    await new Promise((resolve, reject) => {
      socket.addEventListener("open", resolve, { once: true });
      socket.addEventListener("error", reject, { once: true });
    });
    const page = new Page(socket);
    await page.send("Page.enable");
    await page.send("Runtime.enable");
    await page.send("Network.enable");
    await page.send("Log.enable");
    return page;
  }

  send(method, params = {}) {
    const id = this.nextId++;
    this.socket.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }));
  }

  async evaluate(expression) {
    const result = await this.send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
    if (result.exceptionDetails) fail(`script failed: ${result.exceptionDetails.text}`);
    return result.result.value;
  }

  async navigate(url) {
    const loaded = new Promise((resolve) => {
      const listener = (message) => {
        if (message.method === "Page.loadEventFired") {
          this.listeners.delete(listener);
          resolve();
        }
      };
      this.listeners.add(listener);
    });
    await this.send("Page.navigate", { url });
    await loaded;
  }

  async waitFor(description, expression, timeoutMs = 15000) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      if (await this.evaluate(expression).catch(() => false)) return;
      await sleep(100);
    }
    fail(`timed out waiting for ${description}; at ${safeUrl(await this.evaluate("location.href"))}: ${await this.evaluate("document.body?.innerText.slice(0, 300)")}\n${this.diagnostics.report(await this.evaluate(PAGE_STATE).catch(() => null))}`);
  }

  // Sets a React-controlled input the way typing does.
  // Waits until React has hydrated the control: an input event dispatched
  // into server-rendered HTML before that is lost, and the form state stays empty.
  async type(selector, value) {
    await this.waitFor(`${selector} to be interactive`, `(() => {
      const input = document.querySelector(${JSON.stringify(selector)});
      return !!input && Object.keys(input).some((key) => key.startsWith("__reactProps"));
    })()`);
    await this.evaluate(`(() => {
      const input = document.querySelector(${JSON.stringify(selector)});
      const prototype = input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(prototype, "value").set;
      setter.call(input, ${JSON.stringify(value)});
      input.dispatchEvent(new Event("input", { bubbles: true }));
    })()`);
  }

  // Chooses an option of a React-controlled select, once hydrated.
  async select(selector, value) {
    await this.waitFor(`${selector} to be interactive`, `(() => {
      const select = document.querySelector(${JSON.stringify(selector)});
      return !!select && !select.disabled && Object.keys(select).some((key) => key.startsWith("__reactProps"))
        && [...select.options].some((option) => option.value === ${JSON.stringify(value)});
    })()`);
    await this.evaluate(`(() => {
      const select = document.querySelector(${JSON.stringify(selector)});
      Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value").set.call(select, ${JSON.stringify(value)});
      select.dispatchEvent(new Event("change", { bubbles: true }));
    })()`);
  }

  click(selector) {
    return this.evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);
  }

  close() {
    this.socket.close();
  }
}
