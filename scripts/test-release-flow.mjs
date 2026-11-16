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
import { accessSync, constants, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join, resolve } from "node:path";
import { createConnection } from "node:net";
import { pathToFileURL } from "node:url";

// Startup is import-safe so deterministic tests need neither Chrome nor the shop.
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const TAIL_BYTES = 8192;

function executable(command, env) {
  for (const candidate of command.includes("/") ? [resolve(command)] :
    (env.PATH || "").split(delimiter).map((dir) => resolve(dir, command))) {
    try { accessSync(candidate, constants.X_OK); return candidate; } catch { /* next PATH entry */ }
  }
  return null;
}

function chromeVersion(command, env) {
  const result = spawnSync(command, ["--version"], {
    encoding: "utf8", timeout: 1000, killSignal: "SIGKILL", maxBuffer: TAIL_BYTES, env,
  });
  return {
    output: `${result.stdout || ""}${result.stderr || ""}`.slice(-TAIL_BYTES).trim(),
    status: result.status, signal: result.signal,
    error: result.error?.code || null,
  };
}

function portStatus(host, port) {
  return new Promise((resolveStatus) => {
    const socket = createConnection({ host, port });
    const finish = (status) => { socket.destroy(); resolveStatus(status); };
    socket.once("connect", () => finish("occupied"));
    socket.once("error", (error) => finish(error.code === "ECONNREFUSED" ? "free" : "unknown"));
    socket.setTimeout(200, () => finish("unknown"));
  });
}

async function bounded(promise, milliseconds, onTimeout) {
  let timer;
  try {
    return await Promise.race([promise, new Promise((_, reject) => {
      timer = setTimeout(() => { onTimeout?.(); reject(new Error("timeout/stalled-request")); }, Math.max(1, milliseconds));
    })]);
  } finally { clearTimeout(timer); }
}

// Owns the profile, child and WebSocket from creation through journey completion.
// Injectable boundaries are only for startup tests; production uses Node's APIs.
export async function withChromeStartup(journey, options = {}) {
  const {
    env = process.env, spawnChrome = spawn, fetchReady = fetch,
    WebSocketClass = globalThis.WebSocket, probeVersion = chromeVersion,
    probePort = portStatus, startupMs = 10000, requestMs = 500,
    pollMs = 100, termMs = 5000, killMs = 1000, profileRoot = tmpdir(),
  } = options;
  // Headless smoke needs no desktop bus. Explicit Unix addresses avoid both
  // inherited invalid transports and session discovery/autolaunch on unset.
  // Keep XDG_RUNTIME_DIR and all unrelated settings; never mutate the parent.
  const chromeEnv = {
    ...env,
    DBUS_SESSION_BUS_ADDRESS: "unix:path=/dev/null",
    DBUS_SYSTEM_BUS_ADDRESS: "unix:path=/dev/null",
  };
  const host = "127.0.0.1";
  const port = Number(env.CDP_PORT || 9223);
  const started = performance.now();
  const diagnostic = {
    command: null, executable: null, version: null, pid: null, arguments: [],
    host, port, portBeforeSpawn: "unknown", profile: null, elapsedMs: 0,
    exitCode: null, signal: null, processEvent: null, cleanupSignal: null,
    lastReadinessFailure: null, stdoutTail: "", stderrTail: "", cleanup: null,
  };
  let child, ws, deadline, failure, inJourney = false, dead = false, stopped = false, closed = false;
  let stdout = Buffer.alloc(0), stderr = Buffer.alloc(0);
  let recordDeath, recordStopped, recordClosed;
  const close = new Promise((resolveClose) => { recordClosed = resolveClose; });
  const stop = new Promise((resolveStop) => { recordStopped = resolveStop; });
  const death = new Promise((resolveDeath) => { recordDeath = resolveDeath; });
  const processDied = (event, code = null, signal = null) => {
    if (!event.startsWith("spawn-error") || !child?.pid) {
      stopped = true;
      recordStopped();
    }
    if (dead) {
      if (code !== null || signal !== null) { diagnostic.exitCode = code; diagnostic.signal = signal; }
      return;
    }
    dead = true;
    diagnostic.processEvent = event;
    diagnostic.exitCode = code;
    diagnostic.signal = signal;
    recordDeath();
  };
  const assertAlive = () => {
    if (dead) throw new Error(`Chrome ${diagnostic.processEvent} before DevTools startup completed`);
  };
  const whileAlive = (promise) => Promise.race([
    promise, death.then(() => { assertAlive(); }),
  ]);
  const tail = (previous, chunk) => {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    if (bytes.length >= TAIL_BYTES) return Buffer.from(bytes.subarray(-TAIL_BYTES));
    return Buffer.concat([previous.subarray(Math.max(0, previous.length + bytes.length - TAIL_BYTES)), bytes]);
  };
  const remaining = () => Math.max(1, deadline - performance.now());
  const waitForDeath = async (milliseconds) => {
    if (stopped) return;
    try { await bounded(stop, milliseconds); } catch { /* caller checks death */ }
  };
  try {
    diagnostic.command = env.CHROME_BIN || ["google-chrome", "google-chrome-stable", "chromium", "chromium-browser"]
      .find((name) => executable(name, env));
    if (!diagnostic.command) throw new Error("Chrome/Chromium not found; set CHROME_BIN");
    diagnostic.executable = executable(diagnostic.command, env);
    const command = diagnostic.executable || diagnostic.command;
    try { diagnostic.version = probeVersion(command, chromeEnv); }
    catch (error) { diagnostic.version = { error: error.code || "version probe failed" }; }
    diagnostic.profile = mkdtempSync(join(profileRoot, "release-smoke-chrome-"));
    diagnostic.arguments = [
      "--headless=new", `--remote-debugging-port=${port}`, `--user-data-dir=${diagnostic.profile}`,
      "--no-first-run", "--no-default-browser-check", "--disable-gpu", "--disable-extensions",
    ];
    // Ubuntu 24.04 CI runners restrict unprivileged user namespaces.
    if (env.CI) diagnostic.arguments.push("--no-sandbox");
    diagnostic.arguments.push("about:blank");
    diagnostic.portBeforeSpawn = await probePort(host, port);
    child = spawnChrome(command, diagnostic.arguments, { stdio: ["ignore", "pipe", "pipe"], env: chromeEnv });
    deadline = performance.now() + startupMs;
    diagnostic.pid = child.pid || null;
    child.stdout?.on("data", (chunk) => { if (!inJourney) stdout = tail(stdout, chunk); });
    child.stderr?.on("data", (chunk) => { if (!inJourney) stderr = tail(stderr, chunk); });
    child.on("error", (error) => processDied(`spawn-error (${error.code || "unknown"})`));
    child.once("exit", (code, signal) => processDied("early-exit", code, signal));
    child.once("close", (code, signal) => {
      closed = true;
      recordClosed();
      processDied("early-close", code, signal);
    });

    let endpoint;
    while (performance.now() < deadline) {
      assertAlive();
      const controller = new AbortController();
      try {
        endpoint = await bounded(whileAlive((async () => {
          let response;
          try { response = await fetchReady(`http://${host}:${port}/json/list`, { signal: controller.signal }); }
          catch { throw new Error("connection/fetch-failure"); }
          if (!response.ok) {
            void response.body?.cancel().catch(() => {});
            throw new Error(`http-non-success (${response.status})`);
          }
          let targets;
          try { targets = await response.json(); }
          catch { throw new Error("malformed-json"); }
          const page = Array.isArray(targets) && targets.find((target) =>
            target?.type === "page" && typeof target.webSocketDebuggerUrl === "string" && target.webSocketDebuggerUrl);
          if (!page) throw new Error("response-without-page-target");
          return page.webSocketDebuggerUrl;
        })()), Math.min(requestMs, remaining()), () => controller.abort());
        assertAlive();
        break;
      } catch (error) {
        assertAlive();
        diagnostic.lastReadinessFailure = error.message;
      } finally { controller.abort(); }
      // This is the existing readiness polling interval, bounded by the deadline.
      await whileAlive(sleep(Math.min(pollMs, remaining())));
    }
    if (!endpoint) throw new Error("Chrome DevTools readiness budget exhausted");
    try {
      ws = new WebSocketClass(endpoint);
      await bounded(whileAlive(new Promise((resolveOpen, reject) => {
        ws.addEventListener("open", resolveOpen, { once: true });
        ws.addEventListener("error", () => reject(new Error("websocket-error")), { once: true });
        ws.addEventListener("close", () => reject(new Error("websocket-closed-before-open")), { once: true });
      })), remaining());
      assertAlive();
    } catch (error) { throw new Error(`WebSocket setup failed: ${error.message}`); }
    diagnostic.elapsedMs = Math.round(performance.now() - started);
    inJourney = true;
    await journey(ws, diagnostic);
  } catch (error) {
    failure = error;
    if (!inJourney) diagnostic.elapsedMs = Math.round(performance.now() - started);
  } finally {
    try {
      try { ws?.close(); } catch { /* already closed */ }
      if (child?.pid && !stopped) {
        diagnostic.cleanupSignal = "SIGTERM";
        child.kill("SIGTERM");
        await waitForDeath(termMs);
        if (!stopped) {
          // ChildProcess.kill targets only this exact spawned PID, never a group.
          diagnostic.cleanupSignal = "SIGKILL";
          child.kill("SIGKILL");
          await waitForDeath(killMs);
        }
        if (!stopped) throw new Error("Chrome did not exit after SIGKILL; profile retained");
      }
      // Exit can precede the final stderr bytes and helper-process teardown.
      if (child?.pid && !closed) {
        try { await bounded(close, killMs); }
        catch { throw new Error("Chrome stdio did not close; profile retained"); }
      }
      if (diagnostic.profile) rmSync(diagnostic.profile, { recursive: true, force: true, maxRetries: 3, retryDelay: 300 });
      diagnostic.cleanup = "process stopped; profile removed";
    } catch (error) {
      diagnostic.cleanup = error.message;
      failure = new Error(`${failure ? failure.message + "; " : ""}cleanup failed: ${error.message}`);
      inJourney = false;
    }
  }
  if (failure) {
    if (inJourney) throw failure;
    diagnostic.stdoutTail = stdout.toString("utf8");
    diagnostic.stderrTail = stderr.toString("utf8");
    // Only known startup fields are reported; never dump the process environment.
    let detail = `${failure.message}\nChrome startup diagnostics: ${JSON.stringify(diagnostic)}`;
    for (const secret of [
      env.SMOKE_EMAIL, env.SMOKE_PASSWORD,
      env.DBUS_SESSION_BUS_ADDRESS, env.DBUS_SYSTEM_BUS_ADDRESS, env.XDG_RUNTIME_DIR,
      chromeEnv.DBUS_SESSION_BUS_ADDRESS, chromeEnv.DBUS_SYSTEM_BUS_ADDRESS,
    ].filter(Boolean)) {
      detail = detail.replaceAll(secret, "[REDACTED]").replaceAll(JSON.stringify(secret).slice(1, -1), "[REDACTED]");
    }
    throw new Error(detail);
  }
}

const EDGE = (process.env.EDGE_ORIGIN || "http://127.0.0.1:8080").replace(/\/$/, "");
const EMAIL = process.env.SMOKE_EMAIL || "demo@sugar.ru";
const PASSWORD = process.env.SMOKE_PASSWORD || "Sweet2026!";
const PRODUCT = { id: "p01", slug: "dark-chocolate-sea-salt", name: "Тёмный шоколад 70% с морской солью" };

let ws;
let failures = 0;

let sequence = 0;
const pending = new Map();
const navigations = [];
const requests = [];

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

async function main() {
  try {
    await withChromeStartup(async (socket) => {
      ws = socket;
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
      await run();
    });
  } catch (error) {
    failures++;
    console.log(`FAIL  ${error.message}`);
  }
  console.log(failures ? `\nrelease flow smoke FAILED (${failures})` : `\nrelease flow smoke passed for ${EDGE}`);
  process.exit(failures ? 1 : 0);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  await main();
}
