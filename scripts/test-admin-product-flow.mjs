#!/usr/bin/env node
// Staff product management through the real stack (ADR 013, ADR 014): the
// admin UI in headless Chrome, the admin edge behind a throwaway TLS
// terminator, Go and PostgreSQL, then the customer shop and the staff audit
// table. It edits, archives and restores the demo product p01, so run it
// only against a disposable database (CI or a throwaway Compose project).
//
//   ADMIN_ORIGIN   default https://localhost:8443; run through scripts/with-admin-tls-proxy.sh
//   SHOP_ORIGIN    default http://127.0.0.1:8080
//   ADMIN_SMOKE_EMAIL / ADMIN_SMOKE_PASSWORD   a staff account provisioned with shopctl
//   COMPOSE_PROJECT  default sweetshop-edge-smoke (for reading audit rows with psql)
//
// Uses one staff sign-in attempt of the admin edge bucket.
import { execFileSync } from "node:child_process";
import { rm } from "node:fs/promises";

import { launchChrome, Page } from "./lib/admin-chrome.mjs";

const admin = process.env.ADMIN_ORIGIN ?? "https://localhost:8443";
const shop = process.env.SHOP_ORIGIN ?? "http://127.0.0.1:8080";
const email = process.env.ADMIN_SMOKE_EMAIL;
const password = process.env.ADMIN_SMOKE_PASSWORD;
const project = process.env.COMPOSE_PROJECT ?? "sweetshop-edge-smoke";
const PRODUCT = { id: "p01", slug: "dark-chocolate-sea-salt", search: "морской" };

if (!email || !password) throw new Error("ADMIN_SMOKE_EMAIL and ADMIN_SMOKE_PASSWORD are required");

function fail(message) {
  throw new Error(`admin product flow: ${message}`);
}

function sql(query) {
  return execFileSync("docker", [
    "compose", "-p", project, "--env-file", ".env.example", "exec", "-T", "postgres",
    "psql", "-U", process.env.POSTGRES_USER ?? "sweetshop", "-d", process.env.POSTGRES_DB ?? "sweetshop", "-tA", "-F", "|", "-c", query,
  ], { encoding: "utf8" }).trim();
}

const onPath = (path) => `location.origin === ${JSON.stringify(admin)} && location.pathname === ${JSON.stringify(path)}`;
const hasText = (text) => `document.body.innerText.includes(${JSON.stringify(text)})`;
const revisionIs = (revision) => `document.querySelector("[data-testid=product-revision]")?.textContent === ${JSON.stringify(String(revision))}`;

function clickButton(page, text) {
  return page.evaluate(`(() => {
    const button = [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === ${JSON.stringify(text)});
    if (!button || button.disabled) throw new Error("no enabled button " + ${JSON.stringify(text)});
    button.click();
  })()`);
}

const value = (page, selector) => page.evaluate(`document.querySelector(${JSON.stringify(selector)}).value`);
const revision = async (page) => Number(await page.evaluate(`document.querySelector("[data-testid=product-revision]").textContent`));

async function customerStatus() {
  const response = await fetch(`${shop}/api/products/${PRODUCT.id}`, { cache: "no-store" });
  return { status: response.status, body: response.status === 200 ? await response.json() : null };
}

async function openProduct(page) {
  await page.navigate(`${admin}/products/${PRODUCT.id}`);
  await page.waitFor("the product editor", `${onPath(`/products/${PRODUCT.id}`)} && !!document.querySelector("#product-name")?.value`);
}

async function main() {
  const startedAt = sql("SELECT now()");
  const staffId = sql(`SELECT id FROM staff_users WHERE lower(email)=lower('${email.replaceAll("'", "''")}')`);
  if (!/^stf_[0-9a-f]{32}$/.test(staffId)) fail("the smoke staff account does not exist");
  const chrome = await launchChrome();
  try {
    const a = await Page.open(chrome.port);

    // 1. Sign in, open Products, search and open p01.
    await a.navigate(`${admin}/login`);
    await a.waitFor("the sign-in page", hasText("Вход для сотрудников"));
    await a.type("#staff-email", email);
    await a.type("#staff-password", password);
    await a.click("button[type=submit]");
    await a.waitFor("the shell", `${onPath("/")} && ${hasText("Панель управления")}`);
    await a.click('nav a[href="/products"]');
    await a.waitFor("the product list", `${onPath("/products")} && !!document.querySelector("table")`);
    await a.type("#product-search", PRODUCT.search);
    await clickButton(a, "Найти");
    await a.waitFor("the search result", `location.search.includes("q=") && !!document.querySelector('table a[href="/products/${PRODUCT.id}"]')`);
    await a.click(`table a[href="/products/${PRODUCT.id}"]`);
    await a.waitFor("the product editor", `${onPath(`/products/${PRODUCT.id}`)} && !!document.querySelector("#product-name")?.value`);
    const start = await revision(a);

    // 2. A non-destructive edit; the revision moves by one and the shop shows it.
    const name = `${(await value(a, "#product-name")).replace(/ \(E2E \d+\)$/, "")} (E2E ${start})`;
    await a.type("#product-name", name);
    await clickButton(a, "Сохранить изменения");
    await a.waitFor("the saved revision", `${hasText(`Изменения сохранены. Ревизия ${start + 1}.`)} && ${revisionIs(start + 1)}`);
    const afterEdit = await customerStatus();
    if (afterEdit.status !== 200 || afterEdit.body.name !== name) fail(`the shop does not show the new name: ${afterEdit.status}`);

    // 3. Two tabs on the same revision: A saves, B's save is stale, B reloads.
    const b = await Page.open(chrome.port);
    await openProduct(b);
    if ((await revision(b)) !== start + 1) fail("tab B did not load the current revision");
    const stock = Number(await value(a, "#product-stock"));
    await a.type("#product-stock", String(stock + 1));
    await clickButton(a, "Сохранить изменения");
    await a.waitFor("tab A's save", revisionIs(start + 2));
    await b.type("#product-shortName", "Изменение вкладки B");
    await clickButton(b, "Сохранить изменения");
    await b.waitFor("the conflict", hasText("Товар был изменён другим сотрудником. Ваши изменения не сохранены."));
    if ((await value(b, "#product-shortName")) !== "Изменение вкладки B" || (await revision(b)) !== start + 1) fail("the conflict overwrote tab B's form");
    await clickButton(b, "Загрузить актуальную версию");
    await b.waitFor("the reloaded server version", `${revisionIs(start + 2)} && document.querySelector("#product-stock").value === ${JSON.stringify(String(stock + 1))}`);

    // 4. Archive from B: the shop stops selling it; restore brings it back.
    await clickButton(b, "Снять с продажи…");
    await b.waitFor("the confirmation", hasText("Перенести товар в архив?"));
    await clickButton(b, "Перенести в архив");
    await b.waitFor("the archived state", `${hasText(`Товар перенесён в архив. Ревизия ${start + 3}.`)} && ${hasText("Вернуть в продажу")}`);
    if ((await customerStatus()).status !== 404) fail("the shop still sells the archived product");
    await b.navigate(`${shop}/products/${PRODUCT.slug}`);
    await b.waitFor("the shop's product not-found page", hasText("Товар не найден"));
    await openProduct(b);
    await clickButton(b, "Вернуть в продажу");
    await b.waitFor("the restored state", hasText(`Товар снова в продаже. Ревизия ${start + 4}.`));
    if ((await customerStatus()).status !== 200) fail("the restored product is not for sale");
    b.close();

    // 5. Sign out.
    await clickButton(a, "Выйти");
    await a.waitFor("the sign-in page after sign-out", onPath("/login"));
    a.close();

    // 6. Audit: one row per committed mutation, none for the stale save.
    const rows = sql(`SELECT action, staff_user_id, entity_type, entity_id, request_id IS NOT NULL FROM staff_audit_events
      WHERE entity_id='${PRODUCT.id}' AND created_at >= '${startedAt}' ORDER BY created_at, id`).split("\n").filter(Boolean);
    const expected = ["product.update", "product.update", "product.archive", "product.restore"].map((action) => `${action}|${staffId}|product|${PRODUCT.id}|t`);
    if (JSON.stringify(rows) !== JSON.stringify(expected)) fail(`audit rows:\n${rows.join("\n")}`);
    if (Number(sql(`SELECT revision FROM products WHERE id='${PRODUCT.id}'`)) !== start + 4) fail("the stored revision is not four edits later");
    console.log(`admin product flow passed for ${admin}: revisions ${start}→${start + 4}, ${rows.length} audit events, stale save refused`);
  } finally {
    chrome.child.kill("SIGKILL");
    await rm(chrome.profile, { recursive: true, force: true }).catch(() => {});
  }
}

await main();
