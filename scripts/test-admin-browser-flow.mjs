#!/usr/bin/env node
// Staff sign-in journey in a real headless Chrome against the real stack
// (ADR 014): the admin edge, the admin Next app, Go and PostgreSQL. Drives
// Chrome over the DevTools protocol with Node's built-in WebSocket, so no
// browser-automation package is needed. Disposable environments only.
//
//   ADMIN_ORIGIN   default https://localhost:8443. Browsers accept the
//                  __Host-sweetshop_staff cookie only over https, so run this
//                  through scripts/with-admin-tls-proxy.sh, which sets it and
//                  CHROME_IGNORE_CERTIFICATE_ERRORS for its self-signed cert.
//   SHOP_ORIGIN    default http://127.0.0.1:8080 (a different host, so cookies never mix)
//   ADMIN_SMOKE_EMAIL / ADMIN_SMOKE_PASSWORD   a staff account provisioned with shopctl
//   CHROME         default google-chrome, then chromium
//
// Uses two staff sign-in attempts of the admin edge bucket.
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

const admin = process.env.ADMIN_ORIGIN ?? "https://localhost:8443";
const shop = process.env.SHOP_ORIGIN ?? "http://127.0.0.1:8080";
const email = required("ADMIN_SMOKE_EMAIL");
const password = required("ADMIN_SMOKE_PASSWORD");

function required(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function fail(message) {
  throw new Error(`admin browser flow: ${message}`);
}

async function launchChrome() {
  const profile = await mkdtemp(join(tmpdir(), "admin-flow-chrome-"));
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

class Page {
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
  async type(selector, value) {
    await this.evaluate(`(() => {
      const input = document.querySelector(${JSON.stringify(selector)});
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
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

const onPath = (path) => `location.origin === ${JSON.stringify(admin)} && location.pathname === ${JSON.stringify(path)}`;
const hasText = (text) => `document.body.innerText.includes(${JSON.stringify(text)})`;

async function signIn(page, secret) {
  await page.type("#staff-email", email);
  await page.type("#staff-password", secret);
  await page.click("button[type=submit]");
}

async function main() {
  const chrome = await launchChrome();
  try {
    const page = await Page.open(chrome.port);

    // 1. Anonymous: every admin page leads to sign-in.
    await page.navigate(`${admin}/`);
    await page.waitFor("the sign-in page", `${onPath("/login")} && ${hasText("Вход для сотрудников")}`);
    if (await page.evaluate(`document.querySelector("#staff-password").autocomplete`) !== "current-password") fail("password autocomplete");

    // 2. A wrong password gets the generic message and no session.
    await signIn(page, "wrong password, not the smoke secret");
    await page.waitFor("the generic rejection", `document.querySelector('[role=alert]')?.innerText.includes("Неверный email или пароль.")`);
    if (await page.evaluate(onPath("/login")) !== true) fail("left /login after a wrong password");

    // 3. The right password opens the shell.
    await signIn(page, password);
    await page.waitFor("the staff shell", `${onPath("/")} && ${hasText("Панель управления")} && ${hasText(email)}`);

    // 4. The session is only the HttpOnly __Host- cookie of the admin host.
    const { cookies } = await page.send("Network.getCookies", { urls: [admin] });
    const staffCookie = cookies.find((cookie) => cookie.name === "__Host-sweetshop_staff");
    if (!staffCookie) fail(`no __Host-sweetshop_staff cookie: ${cookies.map((cookie) => cookie.name).join(", ")}`);
    if (!staffCookie.secure || !staffCookie.httpOnly || staffCookie.sameSite !== "Strict" || staffCookie.path !== "/" || staffCookie.domain !== new URL(admin).hostname) {
      fail(`staff cookie attributes: ${JSON.stringify({ ...staffCookie, value: "<redacted>" })}`);
    }
    const storage = await page.evaluate(`JSON.stringify({ cookie: document.cookie, local: Object.keys(localStorage), session: Object.keys(sessionStorage) })`);
    if (storage !== JSON.stringify({ cookie: "", local: [], session: [] })) fail(`browser storage is not empty: ${storage}`);
    if (await page.evaluate(`document.documentElement.outerHTML.includes(${JSON.stringify(staffCookie.value)})`)) fail("staff token in the page");
    const { cookies: shopCookies } = await page.send("Network.getCookies", { urls: [shop] });
    if (shopCookies.some((cookie) => cookie.name.includes("sweetshop_staff"))) fail("the staff cookie reaches the shop host");

    // 5. A reload and a fresh tab restore the session from the cookie alone.
    await page.navigate(`${admin}/`);
    await page.waitFor("the shell after reload", `${onPath("/")} && ${hasText(email)}`);
    const tab = await Page.open(chrome.port);
    await tab.navigate(`${admin}/login`);
    await tab.waitFor("the direct visit to be sent to the shell", `${onPath("/")} && ${hasText("Панель управления")}`);
    tab.close();

    // 6. Sign-out ends the session; protected pages are closed again.
    await page.click("header button");
    await page.waitFor("the sign-in page after sign-out", `${onPath("/login")} && ${hasText("Вход для сотрудников")}`);
    await page.navigate(`${admin}/`);
    await page.waitFor("the protected page to be closed", onPath("/login"));
    const status = await page.evaluate(`fetch("/api/admin/v1/products").then((r) => r.status)`);
    if (status !== 401) fail(`staff API after sign-out: HTTP ${status}`);

    // 7. The shop works on its own host and has no staff API.
    await page.navigate(`${shop}/`);
    await page.waitFor("the storefront home", `document.title.includes("Слой") && !${hasText("Вход для сотрудников")}`);
    const shopAdmin = await page.evaluate(`fetch("/api/admin/v1/auth/session").then((r) => r.status)`);
    if (shopAdmin !== 404) fail(`shop host /api/admin: HTTP ${shopAdmin}`);
    await page.navigate(`${shop}/catalog`);
    await page.waitFor("the storefront catalog", `document.querySelectorAll("main a[href^='/products/']").length > 0`);
    page.close();
    console.log(`admin browser flow passed for ${admin} (shop ${shop})`);
  } finally {
    chrome.child.kill("SIGKILL");
    await rm(chrome.profile, { recursive: true, force: true }).catch(() => {});
  }
}

await main();
