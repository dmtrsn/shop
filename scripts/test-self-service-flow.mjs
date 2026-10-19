#!/usr/bin/env node
// Self-service journey through the public edge, driven by headless Chrome over
// CDP (Node 22 global WebSocket; no npm dependencies): registration with email
// confirmation, then password recovery. Emails are read from Mailpit's API.
//
// It creates a customer account, so run it only against a disposable
// environment whose app sends email to Mailpit (the CI Compose project with
// the mail profile). Never production. It waits about a minute between the
// two emails: the per-address allowance permits one account email a minute.
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
const NEW_PASSWORD = `reset ${randomBytes(6).toString("hex")} pass`;
const FAVORITE = "p01";

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

// The newest message to EMAIL with subject, polled from Mailpit.
async function mailWithSubject(subject) {
  const end = Date.now() + 30000;
  while (Date.now() < end) {
    const search = await (await fetch(`${MAILPIT}/api/v1/search?query=${encodeURIComponent(`to:${EMAIL}`)}`)).json();
    const found = (search.messages || []).find((message) => message.Subject === subject);
    if (found) return (await fetch(`${MAILPIT}/api/v1/message/${found.ID}`)).json();
    await sleep(300);
  }
  throw new Error(`no "${subject}" email in Mailpit`);
}

// A request from outside the browser with an explicit credential.
async function edge(path, init = {}) {
  return fetch(EDGE + path, { ...init, headers: { Origin: EDGE, "Content-Type": "application/json", ...(init.headers || {}) } });
}

async function run() {
  await send("Page.enable");
  await send("Runtime.enable");
  await send("Network.enable");
  await send("Network.clearBrowserCookies");

  // 1. The login page offers registration and recovery because Go advertises them.
  await open("/login");
  await waitFor(`!!${mainLink("Создать аккаунт")}`, "Create account link");
  check("/login shows «Создать аккаунт» and «Забыли пароль?»", !!(await evaluate(`!!${mainLink("Забыли пароль?")}`)));
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
  const mail = await mailWithSubject("Подтвердите регистрацию в магазине «Слой»");
  const firstMailAt = Date.now();
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

  await recover(firstMailAt, token);
  // Paths such as /reset-password are fine; no query string may carry a
  // credential, and no URL a token or password.
  const leaked = requests.filter((r) => /password|token|csrf/i.test(decodeURIComponent(new URL(r.url).search)) ||
    [token, resetToken, PASSWORD, NEW_PASSWORD].some((secret) => secret && decodeURIComponent(r.url).includes(secret)));
  check("no secret in any request URL", leaked.length === 0, leaked.map((r) => r.url).join(" "));
  check("nothing was logged to the console with a token or password",
    consoleText.every((line) => !line.includes(token) && !line.includes(PASSWORD) && !line.includes(NEW_PASSWORD) && !(resetToken && line.includes(resetToken))));
  const foreign = requests.filter((r) => /^https?:/.test(r.url) && !r.url.startsWith(EDGE + "/"));
  check("every request stays on the edge origin", foreign.length === 0, foreign.map((r) => r.url).join(" "));
}

let resetToken = "";

// Password recovery for the account just created, while it is signed in with
// a cookie (this browser) and a bearer token (elsewhere).
async function recover(firstMailAt, registrationToken) {
  // 8. Customer state that recovery must keep, and sessions it must end.
  const state = await evaluate(`fetch("/api/auth/session").then((r) => r.json())`);
  const added = await evaluate(`fetch("/api/favorites/${FAVORITE}", { method: "PUT", headers: { "X-CSRF-Token": ${text(state?.csrfToken || "")} } }).then((r) => r.status)`);
  check("the new customer adds a favorite", added === 200, String(added));
  const cookie = (await send("Network.getCookies", { urls: [EDGE] })).cookies.find((c) => /sweetshop_session$/.test(c.name));
  const bearer = (await (await edge("/api/auth/login", { method: "POST", body: JSON.stringify({ email: EMAIL, password: PASSWORD }) })).json()).token;
  check("a cookie and a bearer session exist before recovery", !!cookie && !!bearer);

  // 9. Request a reset; the per-address allowance needs a minute since the
  //    confirmation email.
  const wait = firstMailAt + 62000 - Date.now();
  if (wait > 0) await sleep(wait);
  await open("/forgot-password");
  await waitFor(ready(button("Отправить ссылку")), "hydrated forgot-password form");
  await typeInto("#forgot-email", EMAIL);
  await evaluate(`${button("Отправить ссылку")}.click()`);
  await waitFor(pageHas("Проверьте почту"), "generic recovery answer");
  check("the recovery answer is generic", !!(await evaluate(pageHas("Если аккаунт с адресом"))));

  // 10. The reset email and its link.
  const mail = await mailWithSubject("Восстановление пароля в магазине «Слой»");
  const link = (mail.Text.match(/https?:\/\/\S+\/reset-password#t=[A-Za-z0-9_-]{43}/) || [])[0];
  check("the reset email links to PUBLIC_SHOP_ORIGIN/reset-password#t=… and states 1 hour",
    !!link && link.startsWith(`${EDGE}/reset-password#t=`) && mail.Text.includes("1 час"), link ? link.replace(/#t=.*/, "#t=…") : "missing");
  check("the reset email contains no password", !mail.Text.includes(PASSWORD) && !mail.Text.includes(NEW_PASSWORD));
  resetToken = link.split("#t=")[1];
  check("the reset token differs from the registration token", resetToken !== registrationToken);

  // 11. Open it: the fragment disappears; set the new password.
  const before = requests.length;
  await open(link);
  await waitFor("!!document.querySelector('#reset-password')", "reset form");
  check("the reset token is gone from the address bar", !(await evaluate("location.href")).includes("#"));
  const { entries } = await send("Page.getNavigationHistory");
  check("no history entry keeps the reset token", entries.every((entry) => !entry.url.includes(resetToken)));
  await waitFor(ready(button("Сменить пароль")), "hydrated reset form");
  await typeInto("#reset-password", NEW_PASSWORD);
  await evaluate(`${button("Сменить пароль")}.click()`);
  await waitFor(pageHas("Пароль изменён"), "reset done");
  const posts = requests.slice(before).filter((r) => r.method === "POST");
  check("the reset token left the browser only in one confirmation POST body",
    posts.length === 1 && posts[0].url === `${EDGE}/api/auth/password-reset/confirm` && posts[0].body.includes(resetToken) &&
    requests.slice(before).every((r) => !r.url.includes(resetToken) && !r.referrer.includes(resetToken)));

  // 12. Every earlier session is dead, and the browser is signed out.
  const oldCookie = await (await edge("/api/auth/session", { headers: { Cookie: `${cookie.name}=${cookie.value}` } })).json();
  const oldBearer = (await edge("/api/me", { headers: { Authorization: `Bearer ${bearer}` } })).status;
  const browserCookies = (await send("Network.getCookies", { urls: [EDGE] })).cookies.filter((c) => /sweetshop_session$/.test(c.name));
  check("the old cookie session is revoked", oldCookie.authenticated === false);
  check("the old bearer session is revoked", oldBearer === 401, String(oldBearer));
  check("recovery signed nobody in", browserCookies.length === 0);
  const replay = (await edge("/api/auth/password-reset/confirm", { method: "POST", body: JSON.stringify({ token: resetToken, password: "another password 99" }) })).status;
  check("the reset link cannot be replayed", replay === 410, String(replay));

  // 13. Sign in: the old password fails, the new one works, the data stayed.
  await evaluate(`${mainLink("Войти")}.click()`);
  await waitFor("location.pathname === '/login' && !!document.querySelector('#login-email')", "/login");
  await waitFor(ready(button("Войти")), "login form");
  await typeInto("#login-email", EMAIL);
  await typeInto("#login-password", PASSWORD);
  await evaluate(`${button("Войти")}.click()`);
  await waitFor(pageHas("Неверный email или пароль"), "old password rejected");
  check("the old password is rejected", true);
  await evaluate("document.querySelector('#login-password').select()");
  await send("Input.insertText", { text: NEW_PASSWORD });
  await evaluate(`${button("Войти")}.click()`);
  await waitFor("location.pathname !== '/login'", "signed in with the new password");
  const favorites = await evaluate(`fetch("/api/favorites").then((r) => r.json())`);
  check("the new password signs in and the favorite is still there",
    !!favorites?.items?.some((item) => item.id === FAVORITE), (favorites?.items || []).map((item) => item.id).join(","));
}

try {
  await run();
} catch (error) {
  failures++;
  console.log(`FAIL  ${error.message}`);
} finally {
  await cleanup();
}
console.log(failures ? `\nself-service flow smoke FAILED (${failures})` : `\nself-service flow smoke passed for ${EDGE}`);
process.exit(failures ? 1 : 0);
