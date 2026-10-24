#!/usr/bin/env node
// Staff order management through the real stack (ADR 016): the customer places
// orders through the shop API, staff ship, deliver and cancel them in the
// admin UI (headless Chrome, admin edge behind a throwaway TLS terminator),
// then stock, customer history and the staff audit table are checked. It
// places and changes orders of the demo customer, so run it only against a
// disposable database (CI or a throwaway Compose project, APP_MODE=demo).
//
//   ADMIN_ORIGIN   default https://localhost:8443; run through scripts/with-admin-tls-proxy.sh
//   SHOP_ORIGIN    default http://127.0.0.1:8080
//   ADMIN_SMOKE_EMAIL / ADMIN_SMOKE_PASSWORD   a staff account provisioned with shopctl
//   SMOKE_EMAIL / SMOKE_PASSWORD               the customer (default: the demo account)
//   COMPOSE_PROJECT  default sweetshop-edge-smoke (for reading audit rows with psql)
//
// Uses one staff sign-in attempt of the admin edge bucket and one customer sign-in.
import { execFileSync } from "node:child_process";
import { rm } from "node:fs/promises";

import { launchChrome, Page } from "./lib/admin-chrome.mjs";

const admin = process.env.ADMIN_ORIGIN ?? "https://localhost:8443";
const shop = process.env.SHOP_ORIGIN ?? "http://127.0.0.1:8080";
const staffEmail = process.env.ADMIN_SMOKE_EMAIL;
const staffPassword = process.env.ADMIN_SMOKE_PASSWORD;
const customerEmail = process.env.SMOKE_EMAIL ?? "demo@sugar.ru";
const customerPassword = process.env.SMOKE_PASSWORD ?? "Sweet2026!";
const project = process.env.COMPOSE_PROJECT ?? "sweetshop-edge-smoke";
// Order-time contact (ADR 017): the application cannot change a customer's
// name, email or phone, so this disposable run changes the account with SQL,
// the way a future profile feature would, and restores it at the end.
const CONTACT_A = { name: "Анна Сквозная", email: "anna.e2e@example.com", phone: "+7 900 555-00-01" };
const CONTACT_B = { name: "Борис Сквозной", email: "boris.e2e@example.com", phone: "+7 900 555-00-02" };
const quote = (value) => `'${value.replaceAll("'", "''")}'`;
const setAccount = (c) => sql(`UPDATE users SET name=${quote(c.name)},email=${quote(c.email)},phone=${quote(c.phone)} WHERE lower(email)=lower(${quote(currentEmail)}) RETURNING id`);
let currentEmail = customerEmail;
// Products the other journeys do not touch: one to ship, one to cancel (quantity > 1).
const SHIPPED = { product: "p07", quantity: 1 };
const CANCELLED = { product: "p06", quantity: 3 };

if (!staffEmail || !staffPassword) throw new Error("ADMIN_SMOKE_EMAIL and ADMIN_SMOKE_PASSWORD are required");

function fail(message) {
  throw new Error(`admin order flow: ${message}`);
}

function sql(query) {
  return execFileSync("docker", [
    "compose", "-p", project, "--env-file", ".env.example", "exec", "-T", "postgres",
    "psql", "-U", process.env.POSTGRES_USER ?? "sweetshop", "-d", process.env.POSTGRES_DB ?? "sweetshop", "-tA", "-F", "|", "-c", query,
  ], { encoding: "utf8" }).trim();
}

// The customer's side, through the shop edge with a cookie session.
const customer = { cookie: "", csrf: "" };

async function shopRequest(method, path, body) {
  const headers = { Accept: "application/json", Origin: shop };
  if (customer.cookie) headers.Cookie = customer.cookie;
  if (method !== "GET") headers["X-CSRF-Token"] = customer.csrf;
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const response = await fetch(`${shop}${path}`, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await response.text();
  if (!response.ok) fail(`${method} ${path}: HTTP ${response.status} ${text.slice(0, 200)}`);
  return text ? JSON.parse(text) : null;
}

async function signInCustomer() {
  const response = await fetch(`${shop}/api/auth/session`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Origin: shop },
    body: JSON.stringify({ email: customerEmail, password: customerPassword }),
  });
  if (!response.ok) fail(`customer sign-in: HTTP ${response.status}`);
  customer.cookie = response.headers.getSetCookie().find((cookie) => /sweetshop_session=/.test(cookie))?.split(";")[0] ?? "";
  customer.csrf = (await response.json()).csrfToken;
  if (!customer.cookie || !customer.csrf) fail("customer sign-in returned no session");
}

async function placeOrder({ product, quantity }) {
  await shopRequest("DELETE", "/api/cart");
  await shopRequest("POST", "/api/cart/items", { productId: product, quantity, selected: true });
  return shopRequest("POST", "/api/orders", { deliveryAddress: "Москва, ул. Тестовая, 1, подъезд 2, этаж 5, кв. 48 — домофон не работает", comment: "Сквозная проверка" });
}

const stock = async (product) => (await shopRequest("GET", `/api/products/${product}`)).stock;

const onPath = (path) => `location.origin === ${JSON.stringify(admin)} && location.pathname === ${JSON.stringify(path)}`;
const hasText = (text) => `document.body.innerText.includes(${JSON.stringify(text)})`;
const revisionIs = (revision) => `document.querySelector("[data-testid=order-revision]")?.textContent === ${JSON.stringify(String(revision))}`;

function clickButton(page, text) {
  return page.evaluate(`(() => {
    const button = [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === ${JSON.stringify(text)});
    if (!button || button.disabled) throw new Error("no enabled button " + ${JSON.stringify(text)});
    button.click();
  })()`);
}

async function openOrder(page, order) {
  await page.navigate(`${admin}/orders/${encodeURIComponent(order.id)}`);
  await page.waitFor(`order ${order.number}`, `${hasText(`Заказ ${order.number}`)} && !!document.querySelector("[data-testid=order-revision]")`);
}

async function main() {
  const startedAt = sql("SELECT now()");
  const staffId = sql(`SELECT id FROM staff_users WHERE lower(email)=lower('${staffEmail.replaceAll("'", "''")}')`);
  if (!/^stf_[0-9a-f]{32}$/.test(staffId)) fail("the smoke staff account does not exist");

  // 1. The customer orders as A; checkout takes the stock. Then the account
  //    becomes B and a third order is placed.
  await signInCustomer();
  const [originalName, originalEmail, originalPhone] = sql(`SELECT name,email,phone FROM users WHERE lower(email)=lower(${quote(customerEmail)})`).split("|");
  setAccount(CONTACT_A);
  currentEmail = CONTACT_A.email;
  const before = await stock(CANCELLED.product);
  const shipped = await placeOrder(SHIPPED);
  const cancelled = await placeOrder(CANCELLED);
  if ((await stock(CANCELLED.product)) !== before - CANCELLED.quantity) fail("checkout did not take the ordered stock");
  setAccount(CONTACT_B);
  currentEmail = CONTACT_B.email;
  const later = await placeOrder({ product: SHIPPED.product, quantity: 1 });

  const chrome = await launchChrome();
  try {
    const a = await Page.open(chrome.port);
    // 2. Sign in, open Orders, find the order by number.
    await a.navigate(`${admin}/login`);
    await a.waitFor("the sign-in page", hasText("Вход для сотрудников"));
    await a.type("#staff-email", staffEmail);
    await a.type("#staff-password", staffPassword);
    await a.click("button[type=submit]");
    await a.waitFor("the shell", `${onPath("/")} && ${hasText("Панель управления")}`);
    await a.click('nav a[href="/orders"]');
    await a.waitFor("the order list", `${onPath("/orders")} && !!document.querySelector("table")`);
    await a.type("#order-search", shipped.number);
    await clickButton(a, "Найти");
    const link = `table a[href="/orders/${encodeURIComponent(shipped.id)}"]`;
    await a.waitFor("the search result", `location.search.includes("q=") && !!document.querySelector(${JSON.stringify(link)})`);
    await a.click(link);
    await a.waitFor("the order detail", `${revisionIs(1)} && ${hasText("Собираем заказ")}`);

    // 3. Ship it: one valid transition, one revision.
    await clickButton(a, "Передать в доставку");
    await a.waitFor("the transit status", `${hasText("Статус изменён: «В пути». Ревизия 2.")} && ${revisionIs(2)}`);

    // 4. Two tabs at revision 2: A delivers, B's delivery is stale; B reloads.
    const b = await Page.open(chrome.port);
    await openOrder(b, shipped);
    await clickButton(a, "Отметить доставленным");
    await a.waitFor("the delivered status", `${revisionIs(3)} && ${hasText("Заказ доставлен — дальнейших действий нет.")}`);
    await clickButton(b, "Отметить доставленным");
    await b.waitFor("the conflict", hasText("Заказ был изменён другим сотрудником. Действие не выполнено."));
    if (!(await b.evaluate(revisionIs(2)))) fail("the conflict replaced tab B's order");
    await clickButton(b, "Загрузить актуальную версию");
    await b.waitFor("the reloaded order", `${revisionIs(3)} && ${hasText("Заказ доставлен — дальнейших действий нет.")}`);
    b.close();

    // 4b. The orders keep whom they were placed for; the older seeded order says
    //     it has no recorded contact and sets today's account apart.
    const recipient = `document.querySelector("#order-recipient")?.closest("section")?.innerText ?? ""`;
    const shows = (text) => `(${recipient}).includes(${JSON.stringify(text)})`;
    await a.waitFor("A on the delivered order", `${shows(CONTACT_A.name)} && ${shows(CONTACT_A.email)} && ${shows(CONTACT_A.phone)} && !${shows(CONTACT_B.name)} && !${shows("Текущие данные аккаунта")}`);
    await openOrder(a, later);
    await a.waitFor("B on the later order", `${shows(CONTACT_B.name)} && ${shows(CONTACT_B.email)} && !${shows(CONTACT_A.name)}`);
    await openOrder(a, { id: "ord-2026-002", number: "#10482" });
    await a.waitFor("the honest pre-snapshot order", `${shows("Контактные данные на момент заказа не сохранены")} && ${shows("Текущие данные аккаунта")} && ${shows("Могут отличаться от данных на момент заказа.")} && ${shows(CONTACT_B.name)}`);

    // 5. Cancel the second order after confirming; its stock comes back exactly.
    await openOrder(a, cancelled);
    await clickButton(a, "Отменить заказ…");
    await a.waitFor("the confirmation", hasText(`Отменить заказ ${cancelled.number}?`));
    await clickButton(a, "Отменить заказ");
    await a.waitFor("the cancelled status", `${hasText("Заказ отменён, товары возвращены на склад. Ревизия 2.")} && ${hasText("Отменён")}`);
    if ((await stock(CANCELLED.product)) !== before) fail(`stock after cancel: ${await stock(CANCELLED.product)}, expected ${before}`);

    // 6. The customer sees both statuses; lines and totals are unchanged.
    const history = await shopRequest("GET", "/api/orders");
    const shown = Object.fromEntries(history.items.map((order) => [order.id, order]));
    if (shown[shipped.id]?.statusLabel !== "Доставлен" || shown[cancelled.id]?.statusLabel !== "Отменён") fail("customer history statuses");
    if (shown[cancelled.id].totalMinor !== cancelled.totalMinor || shown[cancelled.id].items[0].quantity !== CANCELLED.quantity) fail("customer history changed the snapshot");

    // The delivered and cancelled orders still carry contact A.
    for (const order of [shipped, cancelled]) {
      if (sql(`SELECT customer_name||'|'||customer_email||'|'||customer_phone FROM orders WHERE id=${quote(order.id)}`) !== `${CONTACT_A.name}|${CONTACT_A.email}|${CONTACT_A.phone}`) {
        fail(`${order.number} lost its order-time contact`);
      }
    }
    setAccount({ name: originalName, email: originalEmail, phone: originalPhone });
    currentEmail = originalEmail;

    // 7. Sign out.
    await clickButton(a, "Выйти");
    await a.waitFor("the sign-in page after sign-out", onPath("/login"));
    a.close();
  } finally {
    chrome.child.kill("SIGKILL");
    await rm(chrome.profile, { recursive: true, force: true }).catch(() => {});
  }

  // 8. Audit: one row per committed change, none for the stale delivery.
  const rows = sql(`SELECT entity_id, action, staff_user_id, request_id IS NOT NULL FROM staff_audit_events
    WHERE entity_type='order' AND created_at >= '${startedAt}' ORDER BY created_at, id`).split("\n").filter(Boolean);
  const expected = [
    `${shipped.id}|order.status_change|${staffId}|t`,
    `${shipped.id}|order.status_change|${staffId}|t`,
    `${cancelled.id}|order.cancel|${staffId}|t`,
  ];
  if (JSON.stringify(rows) !== JSON.stringify(expected)) fail(`audit rows:\n${rows.join("\n")}`);
  const leaked = sql(`SELECT count(*) FROM staff_audit_events e WHERE row_to_json(e)::text LIKE ${quote(`%${CONTACT_A.email}%`)}`);
  if (leaked !== "0") fail("an audit row carries order contact data");
  console.log(`admin order flow passed for ${admin}: ${shipped.number} shipped and delivered, ${cancelled.number} cancelled with ${CANCELLED.quantity} units restocked, stale delivery refused, ${rows.length} audit events`);
}

await main();
