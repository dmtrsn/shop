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

export class Page {
  constructor(socket) {
    this.socket = socket;
    this.nextId = 1;
    this.pending = new Map();
    this.listeners = new Set();
    socket.addEventListener("message", (event) => {
      const message = JSON.parse(event.data);
      if (message.id && this.pending.has(message.id)) {
        const { resolve, reject } = this.pending.get(message.id);
        this.pending.delete(message.id);
        if (message.error) reject(new Error(message.error.message));
        else resolve(message.result);
      } else if (message.method) {
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
    fail(`timed out waiting for ${description}; at ${await this.evaluate("location.href")}: ${await this.evaluate("document.body?.innerText.slice(0, 300)")}`);
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

  click(selector) {
    return this.evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);
  }

  close() {
    this.socket.close();
  }
}
