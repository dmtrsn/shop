#!/usr/bin/env node
// Self-registration journey through the public edge, driven by headless Chrome
// over CDP (Node 22 global WebSocket; no npm dependencies). The confirmation
// email is read from Mailpit's API.
//
// It creates a customer account, so run it only against a disposable
// environment whose app sends email to Mailpit (the CI Compose project with
// the mail profile). Never production.
//
//   EDGE_ORIGIN    default http://127.0.0.1:8080 (must be PUBLIC_SHOP_ORIGIN)
//   MAILPIT_API    default http://127.0.0.1:8025
//   CHROME_BIN     Chrome/Chromium binary (default: first found on PATH)
import { spawn, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const EDGE = (process.env.EDGE_ORIGIN || "http://127.0.0.1:8080").replace(/\/$/, "");
const MAILPIT = (process.env.MAILPIT_API || "http://127.0.0.1:8025").replace(/\/$/, "");
const PORT = Number(process.env.CDP_PORT || 9224);
const EMAIL = `smoke-${randomBytes(6).toString("hex")}@example.com`;
const NAME = "Смоук Регистрация";
const PASSWORD = `smoke ${randomBytes(6).toString("hex")} pass`;

function findChrome() {
  if (process.env.CHROME_BIN) return process.env.CHROME_BIN;
  for (const name of ["google-chrome", "google-chrome-stable", "chromium", "chromium-browser"]) {
    if (spawnSync("which", [name]).status === 0) return name;
  }
  throw new Error("Chrome/Chromium not found; set CHROME_BIN");
}

const profile = mkdtempSync(join(tmpdir(), "registration-smoke-chrome-"));
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

async function cleanup() {
  try { ws?.close(); } catch { /* already closed */ }
  if (chrome.exitCode === null && chrome.signalCode === null) {
    const exited = new Promise((resolve) => chrome.once("exit", resolve));
    chrome.kill("SIGTERM");
    await Promise.race([exited, sleep(5000)]);
  }
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
const requests = [];
const consoleText = [];
ws.addEventListener("message", ({ data }) => {
  const message = JSON.parse(data);
  if (message.id && pending.has(message.id)) {
    pending.get(message.id)(message);
    pending.delete(message.id);
  } else if (message.method === "Network.requestWillBeSent") {
    const { request } = message.params;
    requests.push({ method: request.method, url: request.url, referrer: request.headers.Referer || "", body: request.postData || "" });
  } else if (message.method === "Runtime.consoleAPICalled") {
    consoleText.push(JSON.stringify(message.params.args.map((arg) => arg.value ?? arg.description ?? "")));
  }
});

function send(method, params = {}) {
  const id = ++sequence;
  ws.send(JSON.stringify({ id, method, params }));
  return new Promise((resolve, reject) => pending.set(id, (message) =>
    message.error ? reject(new Error(`${method}: ${message.error.message}`)) : resolve(message.result)));
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
    const value = await evaluate(expression);
    if (value) return value;
    await sleep(100);
  }
  throw new Error(`timed out waiting for ${label} at ${await evaluate("location.pathname")}`);
}

function check(name, ok, detail = "") {
  if (!ok) failures++;
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? `  — ${detail}` : ""}`);
}

async function open(url) {
  await send("Page.navigate", { url: url.startsWith("http") ? url : EDGE + url });
  await waitFor("document.readyState === 'complete'", `load ${url.replace(/#.*/, "#…")}`);
}

const text = (value) => JSON.stringify(value);
const pageHas = (value) => `document.body.innerText.includes(${text(value)})`;
const mainLink = (name) => `[...document.querySelectorAll('main a')].find((a) => a.textContent.trim() === ${text(name)})`;
const button = (name) => `[...document.querySelectorAll('main button')].find((b) => b.textContent.trim() === ${text(name)})`;
const ready = (selector) => `(() => { const el = ${selector}; return !!el && !el.disabled; })()`;

async function typeInto(selector, value) {
  await waitFor(`!!document.querySelector(${text(selector)})`, selector);
  await evaluate(`document.querySelector(${text(selector)}).focus()`);
  await send("Input.insertText", { text: value });
}

// The newest message to EMAIL, polled from Mailpit.
async function confirmationMail() {
  const end = Date.now() + 30000;
  while (Date.now() < end) {
    const search = await (await fetch(`${MAILPIT}/api/v1/search?query=${encodeURIComponent(`to:${EMAIL}`)}`)).json();
    if (search.messages?.length) {
      return (await fetch(`${MAILPIT}/api/v1/message/${search.messages[0].ID}`)).json();
    }
    await sleep(300);
  }
  throw new Error("no confirmation email in Mailpit");
}

async function run() {
  await send("Page.enable");
  await send("Runtime.enable");
  await send("Network.enable");
  await send("Network.clearBrowserCookies");

  // 1. The login page offers registration because Go advertises it.
  await open("/login");
  await waitFor(`!!${mainLink("Создать аккаунт")}`, "Create account link");
  check("/login shows «Создать аккаунт»", true);
  await evaluate(`${mainLink("Создать аккаунт")}.click()`);
  await waitFor("location.pathname === '/register' && !!document.querySelector('#register-email')", "/register form");
  check("the link opens the Next /register page", true);

  // 2. Register.
  await waitFor(ready(button("Создать аккаунт")), "hydrated registration form");
  await typeInto("#register-name", NAME);
  await typeInto("#register-email", EMAIL);
  await typeInto("#register-password", PASSWORD);
  const beforeRegister = requests.length;
  await evaluate(`${button("Создать аккаунт")}.click()`);
  await waitFor(pageHas("Проверьте почту"), "check-email state");
  const registerPosts = requests.slice(beforeRegister).filter((r) => r.method === "POST");
  check("registration sent one same-origin POST /api/auth/registration", registerPosts.length === 1 && registerPosts[0].url === `${EDGE}/api/auth/registration`,
    registerPosts.map((r) => r.url).join(" "));
  check("the check-email state names no account state", !(await evaluate("/уже зарегистрирован|создан аккаунт/i.test(document.body.innerText)")));
  const cookiesAfterRegister = (await send("Network.getCookies", { urls: [EDGE] })).cookies.filter((c) => /sweetshop_session$/.test(c.name));
  check("registration alone signs nobody in", cookiesAfterRegister.length === 0);

  // 3. The email.
  const mail = await confirmationMail();
  check("the confirmation email has the fixed subject", mail.Subject === "Подтвердите регистрацию в магазине «Слой»", mail.Subject);
  const link = (mail.Text.match(/https?:\/\/\S+\/register\/confirm#t=[A-Za-z0-9_-]{43}/) || [])[0];
  check("the email links to PUBLIC_SHOP_ORIGIN/register/confirm#t=…", !!link && link.startsWith(`${EDGE}/register/confirm#t=`), link ? link.replace(/#t=.*/, "#t=…") : "missing");
  check("the email does not contain the password or the name", !mail.Text.includes(PASSWORD) && !mail.Text.includes(NAME));
  const token = link.split("#t=")[1];

  // 4. Open the link: the fragment disappears before the visitor does anything.
  const beforeConfirm = requests.length;
  await open(link);
  await waitFor("!!document.querySelector('#confirm-password')", "confirmation form");
  const href = await evaluate("location.href");
  check("the token is gone from the address bar", !href.includes(token) && !href.includes("#"), href);
  const { entries } = await send("Page.getNavigationHistory");
  check("no history entry keeps the token", entries.every((entry) => !entry.url.includes(token)), entries.map((e) => e.url.replace(token, "<token>")).join(" "));
  check("the token is in neither storage", !(await evaluate(`JSON.stringify({ ...localStorage, ...sessionStorage }).includes(${text(token)})`)));

  // 5. Confirm with the chosen password, then the account opens.
  await waitFor(ready(button("Подтвердить и войти")), "hydrated confirmation form");
  await typeInto("#confirm-password", PASSWORD);
  await evaluate(`${button("Подтвердить и войти")}.click()`);
  await waitFor("location.pathname === '/account'", "/account after confirmation");
  await waitFor(pageHas(EMAIL), "account summary");
  check("confirmation signs in and opens /account", true, EMAIL);
  const sent = requests.slice(beforeConfirm);
  const tokenRequests = sent.filter((r) => r.url.includes(token) || r.referrer.includes(token));
  const confirmPosts = sent.filter((r) => r.method === "POST" && r.url === `${EDGE}/api/auth/registration/confirm`);
  check("the token left the browser only in the body of one confirmation POST",
    tokenRequests.length === 0 && confirmPosts.length === 1 && confirmPosts[0].body.includes(token),
    tokenRequests.map((r) => r.url).join(" "));
  const sessionCookie = (await send("Network.getCookies", { urls: [EDGE] })).cookies.find((c) => /sweetshop_session$/.test(c.name));
  check("the session cookie is HttpOnly", !!sessionCookie && sessionCookie.httpOnly && sessionCookie.value !== token);

  // 6. The link is single use.
  const replay = await evaluate(`fetch("/api/auth/registration/confirm", { method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token: ${text(token)}, password: ${text(PASSWORD)} }) }).then((r) => r.status)`);
  check("the link cannot be replayed", replay === 410, String(replay));

  // 7. Log out, then sign in normally with the new account.
  await open("/account");
  await waitFor(ready(button("Выйти")), "logout button");
  await evaluate(`${button("Выйти")}.click()`);
  await waitFor(pageHas("Вы вышли из аккаунта"), "signed out");
  await open("/login?returnTo=%2Faccount");
  await waitFor(ready(button("Войти")), "login form");
  await typeInto("#login-email", EMAIL);
  await typeInto("#login-password", PASSWORD);
  await evaluate(`${button("Войти")}.click()`);
  await waitFor("location.pathname === '/account'", "/account after login");
  await waitFor(pageHas(EMAIL), "account after login");
  check("the new account signs in through the normal /login", true);

  const leaked = requests.filter((r) => /password|token=|csrf/i.test(decodeURIComponent(r.url)) || r.url.includes(token));
  check("no secret in any request URL", leaked.length === 0, leaked.map((r) => r.url).join(" "));
  check("nothing was logged to the console with the token or password",
    consoleText.every((line) => !line.includes(token) && !line.includes(PASSWORD)));
  const foreign = requests.filter((r) => /^https?:/.test(r.url) && !r.url.startsWith(EDGE + "/"));
  check("every request stays on the edge origin", foreign.length === 0, foreign.map((r) => r.url).join(" "));
}

try {
  await run();
} catch (error) {
  failures++;
  console.log(`FAIL  ${error.message}`);
} finally {
  await cleanup();
}
console.log(failures ? `\nregistration flow smoke FAILED (${failures})` : `\nregistration flow smoke passed for ${EDGE}`);
process.exit(failures ? 1 : 0);
