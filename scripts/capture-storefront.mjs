#!/usr/bin/env node
// Visual-review screenshots of the storefront, driven by headless Chrome over
// CDP (Node 22 global WebSocket; no npm dependencies), like test-release-flow.mjs.
//
// It never mutates data. With SMOKE_EMAIL/SMOKE_PASSWORD it signs in through the
// same-origin POST /api/auth/session so private pages show the account's state.
//
//   EDGE_ORIGIN    default http://127.0.0.1:8080 (local next dev: http://localhost:3000)
//   LABEL          output folder name, default "next"
//   PAGESET        "next" (default) or "legacy": built-in page lists
//   PAGES          optional override, ";"-separated "name=path" or "name=path>css-selector"
//                  (the selector is clicked after load; that shot is viewport-only)
//   VIEWPORTS      default "desktop,mobile" (desktop 1440x900, mobile 390x844 DPR 2)
//   SMOKE_EMAIL / SMOKE_PASSWORD   optional sign-in before capturing
//   CHROME_BIN     Chrome/Chromium binary (default: first found on PATH)
//
// Output: artifacts/visual/<LABEL>/<viewport>/<name>.png (ignored by git).
import { spawn, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const EDGE = (process.env.EDGE_ORIGIN || "http://127.0.0.1:8080").replace(/\/$/, "");
const LABEL = process.env.LABEL || "next";
if (!/^[a-z0-9][a-z0-9._-]*$/i.test(LABEL)) throw new Error("LABEL may contain only letters, digits, '.', '_' and '-'");
const PORT = Number(process.env.CDP_PORT || 9225);
const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const OUTPUT = join(ROOT, "artifacts", "visual", LABEL);

const PAGESETS = {
  next: "home=/;catalog=/catalog;product=/products/dark-chocolate-sea-salt;cart=/cart;checkout=/checkout;account=/account;orders=/account/orders",
  // Product detail and checkout were overlays in the old storefront.
  legacy: "home=/legacy/#/;catalog=/legacy/#/catalog;product=/legacy/#/catalog>.product-card [data-product];cart=/legacy/#/cart;checkout=/legacy/#/cart>[data-action=checkout];profile=/legacy/#/profile;orders=/legacy/#/orders",
};
const VIEWPORTS = {
  desktop: { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false },
  mobile: { width: 390, height: 844, deviceScaleFactor: 2, mobile: true },
};

const pagesetName = process.env.PAGESET || "next";
if (!process.env.PAGES && !PAGESETS[pagesetName]) throw new Error(`PAGESET must be one of: ${Object.keys(PAGESETS).join(", ")}`);
const pages = (process.env.PAGES || PAGESETS[pagesetName]).split(";").filter(Boolean).map((entry) => {
  const match = /^([a-z0-9-]+)=(\/[^>]*)(?:>(.+))?$/i.exec(entry.trim());
  if (!match) throw new Error(`Invalid page entry: ${entry}`);
  return { name: match[1], path: match[2], click: match[3] };
});
const viewports = (process.env.VIEWPORTS || "desktop,mobile").split(",").map((name) => {
  if (!VIEWPORTS[name]) throw new Error(`Unknown viewport: ${name}`);
  return [name, VIEWPORTS[name]];
});

function findChrome() {
  if (process.env.CHROME_BIN) return process.env.CHROME_BIN;
  for (const name of ["google-chrome", "google-chrome-stable", "chromium", "chromium-browser"]) {
    if (spawnSync("which", [name]).status === 0) return name;
  }
  throw new Error("Chrome/Chromium not found; set CHROME_BIN");
}

const profile = mkdtempSync(join(tmpdir(), "storefront-capture-chrome-"));
const chromeArgs = [
  "--headless=new", `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`, "--hide-scrollbars",
  "--no-first-run", "--no-default-browser-check", "--disable-gpu", "--disable-extensions",
];
if (process.env.CI) chromeArgs.push("--no-sandbox");
const chrome = spawn(findChrome(), [...chromeArgs, "about:blank"], { stdio: "ignore" });
const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

async function connect() {
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      const targets = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
      const page = targets.find((target) => target.type === "page");
      if (page) return page.webSocketDebuggerUrl;
    } catch { /* Chrome is still starting */ }
    await sleep(100);
  }
  throw new Error("Chrome DevTools endpoint did not start");
}

const ws = new WebSocket(await connect());
await new Promise((done, fail) => {
  ws.addEventListener("open", done, { once: true });
  ws.addEventListener("error", fail, { once: true });
});
let sequence = 0;
const pending = new Map();
ws.addEventListener("message", ({ data }) => {
  const message = JSON.parse(data);
  if (message.id && pending.has(message.id)) {
    pending.get(message.id)(message);
    pending.delete(message.id);
  }
});
function send(method, params = {}) {
  const id = ++sequence;
  ws.send(JSON.stringify({ id, method, params }));
  return new Promise((done, fail) => pending.set(id, (message) =>
    message.error ? fail(new Error(`${method}: ${message.error.message}`)) : done(message.result)));
}
async function evaluate(expression) {
  try {
    const { result, exceptionDetails } = await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true });
    return exceptionDetails ? undefined : result.value;
  } catch {
    return undefined;
  }
}
async function waitFor(expression, label, timeout = 20000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await evaluate(expression)) return;
    await sleep(100);
  }
  throw new Error(`timed out waiting for ${label}`);
}

// Settled: loaded, no busy region left, and two stable layout heights.
async function settle() {
  await waitFor("document.readyState === 'complete'", "load");
  try { await waitFor("!document.querySelector('[aria-busy=\"true\"]')", "busy regions", 10000); } catch { /* capture as-is */ }
  let last = -1;
  for (let i = 0; i < 20; i++) {
    await sleep(250);
    const height = await evaluate("document.documentElement.scrollHeight");
    if (height === last) break;
    last = height;
  }
  // The Next dev-mode indicator is not part of the storefront. The CSP blocks
  // injected <style> elements, so hide it through the CSSOM instead.
  await evaluate("document.querySelectorAll('nextjs-portal').forEach((element) => { element.style.display = 'none'; })");
  await evaluate("document.fonts?.ready.then(() => true)");
}

async function signIn() {
  if (!process.env.SMOKE_EMAIL) return;
  await send("Page.navigate", { url: `${EDGE}/healthz` });
  await sleep(300);
  const status = await evaluate(`fetch('/api/auth/session', { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify({ email: ${JSON.stringify(process.env.SMOKE_EMAIL)}, password: ${JSON.stringify(process.env.SMOKE_PASSWORD || "")} }) }).then((r) => r.status)`);
  if (status !== 200) throw new Error(`sign-in failed with status ${status}`);
}

const results = [];
async function run() {
  await send("Page.enable");
  await send("Runtime.enable");
  await send("Network.enable");
  await send("Network.clearBrowserCookies");
  await signIn();
  for (const [viewportName, metrics] of viewports) {
    await send("Emulation.setDeviceMetricsOverride", metrics);
    await send("Emulation.setTouchEmulationEnabled", { enabled: metrics.mobile });
    mkdirSync(join(OUTPUT, viewportName), { recursive: true });
    for (const page of pages) {
      await send("Page.navigate", { url: EDGE + page.path });
      await sleep(300);
      await settle();
      if (page.click) {
        await waitFor(`!!document.querySelector(${JSON.stringify(page.click)})`, page.click);
        await evaluate(`document.querySelector(${JSON.stringify(page.click)}).click()`);
        await sleep(1200);
        await settle();
      }
      const layout = await evaluate(`({ width: document.documentElement.scrollWidth, height: document.documentElement.scrollHeight,
        viewport: window.innerWidth, url: location.pathname + location.search + location.hash })`);
      // Overlay states are viewport shots; a full-page shot would stretch fixed layers.
      const height = page.click ? metrics.height : Math.min(Math.max(layout.height, metrics.height), 16000);
      const { data } = await send("Page.captureScreenshot", {
        format: "png", captureBeyondViewport: !page.click,
        clip: { x: 0, y: 0, width: metrics.width, height, scale: 1 },
      });
      const file = join(OUTPUT, viewportName, `${page.name}.png`);
      writeFileSync(file, Buffer.from(data, "base64"));
      const overflow = layout.width > layout.viewport;
      results.push({ viewport: viewportName, page: page.name, url: layout.url, file, overflow });
      console.log(`${overflow ? "OVERFLOW" : "ok      "}  ${viewportName.padEnd(7)} ${page.name.padEnd(9)} ${layout.url}  → ${file}${overflow ? `  (scrollWidth ${layout.width} > ${layout.viewport})` : ""}`);
    }
  }
}

let failed = false;
try {
  await run();
} catch (error) {
  failed = true;
  console.log(`FAIL  ${error.message}`);
} finally {
  try { ws.close(); } catch { /* already closed */ }
  chrome.kill("SIGTERM");
  await sleep(500);
  rmSync(profile, { recursive: true, force: true });
}
console.log(`\n${results.length} screenshot(s) in ${OUTPUT}; horizontal overflow: ${results.filter((r) => r.overflow).length}`);
process.exit(failed ? 1 : 0);
