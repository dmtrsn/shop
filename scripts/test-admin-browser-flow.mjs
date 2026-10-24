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
import { rm } from "node:fs/promises";

import { launchChrome, Page } from "./lib/admin-chrome.mjs";

const admin = process.env.ADMIN_ORIGIN ?? "https://localhost:8443";
const shop = process.env.SHOP_ORIGIN ?? "http://127.0.0.1:8080";
const email = required("ADMIN_SMOKE_EMAIL");
const password = required("ADMIN_SMOKE_PASSWORD");

function required(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required`);
  return value;
}

function fail(message) {
  throw new Error(`admin browser flow: ${message}`);
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
