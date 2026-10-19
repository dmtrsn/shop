#!/usr/bin/env node
// Authenticated release smoke through the public edge, driven by headless Chrome
// over CDP (Node 22 global WebSocket; no npm dependencies).
//
// It places a real order, so run it only against a disposable environment such
// as the CI Compose project or a local in-memory backend. Never production.
//
//   EDGE_ORIGIN    default http://127.0.0.1:8080
//   CHROME_BIN     Chrome/Chromium binary (default: first found on PATH)
//   SMOKE_EMAIL / SMOKE_PASSWORD   default: the demo account (APP_MODE=demo)
import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const EDGE = (process.env.EDGE_ORIGIN || "http://127.0.0.1:8080").replace(/\/$/, "");
const EMAIL = process.env.SMOKE_EMAIL || "demo@sugar.ru";
const PASSWORD = process.env.SMOKE_PASSWORD || "Sweet2026!";
const PRODUCT = { id: "p01", slug: "dark-chocolate-sea-salt", name: "Тёмный шоколад 70% с морской солью" };
const PORT = Number(process.env.CDP_PORT || 9223);

function findChrome() {
  if (process.env.CHROME_BIN) return process.env.CHROME_BIN;
  for (const name of ["google-chrome", "google-chrome-stable", "chromium", "chromium-browser"]) {
    if (spawnSync("which", [name]).status === 0) return name;
  }
  throw new Error("Chrome/Chromium not found; set CHROME_BIN");
}

const profile = mkdtempSync(join(tmpdir(), "release-smoke-chrome-"));
const chromeArgs = [
  "--headless=new", `--remote-debugging-port=${PORT}`, `--user-data-dir=${profile}`,
  "--no-first-run", "--no-default-browser-check", "--disable-gpu", "--disable-extensions",
];
// Ubuntu 24.04 CI runners restrict unprivileged user namespaces.
if (process.env.CI) chromeArgs.push("--no-sandbox");
const chrome = spawn(findChrome(), [...chromeArgs, "about:blank"], { stdio: "ignore" });

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
let ws;
let failures = 0;

// Wait for Chrome to exit before removing its profile, or it recreates files.
async function cleanup() {
  try { ws?.close(); } catch { /* already closed */ }
  if (chrome.exitCode === null && chrome.signalCode === null) {
    const exited = new Promise((resolve) => chrome.once("exit", resolve));
    chrome.kill("SIGTERM");
    await Promise.race([exited, sleep(5000)]);
  }
  // Helper processes can still flush into the profile briefly after exit.
  for (let attempt = 0; attempt < 10; attempt++) {
    try { rmSync(profile, { recursive: true, force: true }); } catch { /* retried below */ }
    await sleep(300);
    if (!existsSync(profile)) break;
  }
}

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

ws = new WebSocket(await connect());
await new Promise((resolve, reject) => {
  ws.addEventListener("open", resolve, { once: true });
  ws.addEventListener("error", reject, { once: true });
});

let sequence = 0;
const pending = new Map();
const navigations = [];
const requests = [];
ws.addEventListener("message", ({ data }) => {
  const message = JSON.parse(data);
  if (message.id && pending.has(message.id)) {
    pending.get(message.id)(message);
    pending.delete(message.id);
  } else if (message.method === "Page.frameNavigated" && !message.params.frame.parentId) {
    navigations.push(message.params.frame.url);
  } else if (message.method === "Network.requestWillBeSent") {
    requests.push({ method: message.params.request.method, url: message.params.request.url });
  }
});

function send(method, params = {}) {
  const id = ++sequence;
  ws.send(JSON.stringify({ id, method, params }));
  return new Promise((resolve, reject) => pending.set(id, (message) =>
    message.error ? reject(new Error(`${method}: ${message.error.message}`)) : resolve(message.result)));
}

// Returns undefined while a navigation replaces the execution context.
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
    const value = await evaluate(expression);
    if (value) return value;
    await sleep(100);
  }
  throw new Error(`timed out waiting for ${label} at ${await evaluate("location.href")}`);
}

function check(name, ok, detail = "") {
  if (!ok) failures++;
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? `  — ${detail}` : ""}`);
}

async function open(path) {
  await send("Page.navigate", { url: EDGE + path });
  await waitFor("document.readyState === 'complete'", `load ${path}`);
}

const here = () => evaluate("location.pathname + location.search + location.hash");
const text = (value) => JSON.stringify(value);
const pageHas = (value) => `document.body.innerText.includes(${text(value)})`;
const mainLink = (name) => `[...document.querySelectorAll('main a')].find((a) => a.textContent.trim() === ${text(name)})`;
const button = (name) => `[...document.querySelectorAll('main button')].find((b) => b.textContent.trim() === ${text(name)})`;
const ready = (selector) => `(() => { const el = ${selector}; return !!el && el.getAttribute('aria-disabled') !== 'true' && !el.disabled; })()`;

// React-controlled fields need real input events.
async function typeInto(selector, value) {
  await waitFor(`!!document.querySelector(${text(selector)})`, selector);
  await evaluate(`document.querySelector(${text(selector)}).focus()`);
  await send("Input.insertText", { text: value });
}

async function run() {
  await send("Page.enable");
  await send("Runtime.enable");
  await send("Network.enable");
  await send("Network.clearBrowserCookies");

  // 1. Signed out on a private route.
  await open("/checkout");
  await waitFor(pageHas("Войдите, чтобы оформить заказ"), "signed-out checkout");
  const loginHref = await waitFor(`(() => { const a = ${mainLink("Войти")}; const h = a && a.getAttribute('href'); return h && h.includes('returnTo') ? h : null; })()`, "sign-in link");
  check("signed-out /checkout links to the Next login page with returnTo", loginHref === "/login?returnTo=%2Fcheckout", loginHref);

  // 2. Next-native login: the form POSTs once to Go's same-origin session endpoint.
  await evaluate(`${mainLink("Войти")}.click()`);
  await waitFor("location.pathname === '/login' && !!document.querySelector('main form #login-email')", "Next login form");
  check("login is the Next /login page on the same origin", (await evaluate("location.origin")) === EDGE && (await here()) === "/login?returnTo=%2Fcheckout", await here());
  check("login page loads no legacy UI", !(await evaluate("!!document.querySelector('#app-main, #login-form')")));
  await waitFor(ready(button("Войти")), "hydrated login form");
  await typeInto("#login-email", EMAIL);
  await typeInto("#login-password", PASSWORD);
  const beforeLogin = requests.length;
  await evaluate(`${button("Войти")}.click()`);

  // 3. Return to the exact Next route.
  await waitFor("location.pathname === '/checkout'", "return to /checkout");
  check("successful login returns to /checkout", (await here()) === "/checkout", await here());
  const loginPosts = requests.slice(beforeLogin).filter((request) => request.method === "POST" && new URL(request.url).pathname.startsWith("/api/auth/"));
  check("login sent exactly one same-origin POST /api/auth/session", loginPosts.length === 1 && loginPosts[0].url === `${EDGE}/api/auth/session`,
    loginPosts.map((request) => request.url).join(" "));
  await waitFor(`document.readyState === 'complete' && !${pageHas("Войдите, чтобы оформить заказ")} && !!document.querySelector('main h1')`, "authenticated checkout");
  check("Next discovers the cookie session on /checkout", !(await evaluate(pageHas("Войдите, чтобы оформить заказ"))));

  const { cookies } = await send("Network.getCookies", { urls: [EDGE] });
  const sessionCookie = cookies.find((cookie) => /sweetshop_session$/.test(cookie.name));
  check("session cookie is HttpOnly", !!sessionCookie && sessionCookie.httpOnly, sessionCookie ? `${sessionCookie.name} sameSite=${sessionCookie.sameSite}` : "missing");
  check("session cookie is not readable by page script", !(await evaluate("document.cookie")).includes("sweetshop_session"));

  // 4. Account shows the session.
  await open("/account");
  await waitFor(pageHas(EMAIL), "account summary");
  check("account shows the signed-in user", true, EMAIL);

  // 5. Favorite add/remove, restoring the original state.
  await open(`/products/${PRODUCT.slug}`);
  const favorite = `document.querySelector('main [data-favorite-product="${PRODUCT.id}"][aria-pressed]')`;
  await waitFor(`!!${favorite} && ${favorite}.getAttribute('aria-disabled') !== 'true'`, "favorite control");
  const original = await evaluate(`${favorite}.getAttribute('aria-pressed')`);
  const flipped = original === "true" ? "false" : "true";
  await evaluate(`${favorite}.click()`);
  await waitFor(`${favorite}.getAttribute('aria-pressed') === ${text(flipped)} && ${favorite}.getAttribute('aria-disabled') !== 'true'`, "favorite toggled");
  await evaluate(`${favorite}.click()`);
  await waitFor(`${favorite}.getAttribute('aria-pressed') === ${text(original)} && ${favorite}.getAttribute('aria-disabled') !== 'true'`, "favorite restored");
  check("favorite toggles and is restored", true, `aria-pressed ${original} → ${flipped} → ${original}`);

  // 6. Cart add.
  const addToCart = `document.querySelector('main button[aria-label=${text(`Добавить в корзину: ${PRODUCT.name}`)}]')`;
  await waitFor(`!!${addToCart} || ${pageHas("Перейти в корзину")}`, "cart control");
  if (await evaluate(`!!${addToCart}`)) {
    await evaluate(`${addToCart}.click()`);
    await waitFor(pageHas("Перейти в корзину"), "product in cart");
  }
  check("product is in the cart", !!(await evaluate(pageHas("Перейти в корзину"))));

  // 7. Checkout.
  await open("/checkout");
  await waitFor(ready(button("Подтвердить заказ")), "checkout ready");
  await typeInto("#checkout-address", "Москва, Тестовая улица, 1 (release smoke)");
  await evaluate(`${button("Подтвердить заказ")}.click()`);
  await waitFor("/^\\/account\\/orders\\/[^/]+$/.test(location.pathname)", "order confirmation");
  const banner = await waitFor("document.querySelector('#order-placed')?.textContent", "order placed banner");
  const number = await evaluate("document.querySelector('main h1')?.textContent.replace(/^Заказ\\s*/, '')");
  check("checkout places an order and opens its detail", /оформлен$/.test(banner) && !!number, banner);

  // 8. Order history.
  await open("/account/orders");
  await waitFor(pageHas(`Заказ ${number}`), "order in history");
  check("order history lists the new order", true, `Заказ ${number}`);

  // 9. Next logout.
  await open("/account");
  await waitFor(ready(button("Выйти")), "logout button");
  await evaluate(`${button("Выйти")}.click()`);
  await waitFor(pageHas("Вы вышли из аккаунта"), "signed-out account");
  check("Next logout signs out", true);

  // 10. Signed out afterwards.
  await open("/checkout");
  await waitFor(pageHas("Войдите, чтобы оформить заказ"), "signed-out checkout after logout");
  const after = (await send("Network.getCookies", { urls: [EDGE] })).cookies.filter((cookie) => /sweetshop_session$/.test(cookie.name));
  check("private routes are signed out after logout", after.length === 0, after.map((cookie) => cookie.name).join(","));

  // Page paths such as /forgot-password (prefetched from /login) are fine; no
  // query string or fragment may carry a credential, and no URL the account's
  // email or password.
  const leaked = [...navigations, ...requests.map((request) => request.url)].filter((url) => {
    const { search, hash } = new URL(url);
    return /token|csrf|password|userid/i.test(decodeURIComponent(search + hash)) ||
      [EMAIL, PASSWORD].some((secret) => decodeURIComponent(url).toLowerCase().includes(secret.toLowerCase()));
  });
  check("no secrets or credentials in any URL", leaked.length === 0, leaked.join(" "));
  const legacy = [...navigations, ...requests.map((request) => request.url)].filter((url) => {
    const { pathname } = new URL(url);
    return /^\/legacy(\/|$)/.test(pathname) || ["/app.js", "/migration.js", "/styles.css"].includes(pathname);
  });
  check("no legacy frontend page or shell file was loaded", legacy.length === 0, legacy.join(" "));
  const foreign = requests.filter((request) => /^https?:/.test(request.url) && !request.url.startsWith(EDGE + "/"));
  check("every request stays on the edge origin", foreign.length === 0, foreign.map((request) => request.url).join(" "));
}

try {
  await run();
} catch (error) {
  failures++;
  console.log(`FAIL  ${error.message}`);
} finally {
  await cleanup();
}
console.log(failures ? `\nrelease flow smoke FAILED (${failures})` : `\nrelease flow smoke passed for ${EDGE}`);
process.exit(failures ? 1 : 0);
