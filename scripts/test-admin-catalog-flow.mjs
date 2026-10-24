#!/usr/bin/env node
// Staff catalog dictionary management through the real stack (ADR 018): the
// admin UI in headless Chrome, the admin edge behind a throwaway TLS
// terminator, Go and PostgreSQL, then the customer shop and the staff audit
// table. It creates dictionary entries, moves the demo product p01 between
// categories and archives the demo collection col-gifts, so run it only
// against a disposable database (CI or a throwaway Compose project).
//
//   ADMIN_ORIGIN   default https://localhost:8443; run through scripts/with-admin-tls-proxy.sh
//   SHOP_ORIGIN    default http://127.0.0.1:8080
//   ADMIN_SMOKE_EMAIL / ADMIN_SMOKE_PASSWORD   a staff account provisioned with shopctl
//   COMPOSE_PROJECT  default sweetshop-edge-smoke (for reading audit rows with psql)
//
// Uses one staff sign-in attempt of the admin edge bucket. Each run uses
// fresh slugs, so it can be repeated on the same database.
import { execFileSync } from "node:child_process";
import { rm } from "node:fs/promises";

import { launchChrome, Page } from "./lib/admin-chrome.mjs";

const admin = process.env.ADMIN_ORIGIN ?? "https://localhost:8443";
const shop = process.env.SHOP_ORIGIN ?? "http://127.0.0.1:8080";
const email = process.env.ADMIN_SMOKE_EMAIL;
const password = process.env.ADMIN_SMOKE_PASSWORD;
const project = process.env.COMPOSE_PROJECT ?? "sweetshop-edge-smoke";
const run = Date.now().toString(36);

if (!email || !password) throw new Error("ADMIN_SMOKE_EMAIL and ADMIN_SMOKE_PASSWORD are required");

function fail(message) {
  throw new Error(`admin catalog flow: ${message}`);
}

function sql(query) {
  return execFileSync("docker", [
    "compose", "-p", project, "--env-file", ".env.example", "exec", "-T", "postgres",
    "psql", "-U", process.env.POSTGRES_USER ?? "sweetshop", "-d", process.env.POSTGRES_DB ?? "sweetshop", "-tA", "-F", "|", "-c", query,
  ], { encoding: "utf8" }).trim();
}

const onPath = (path) => `location.origin === ${JSON.stringify(admin)} && location.pathname === ${JSON.stringify(path)}`;
const hasText = (text) => `document.body.innerText.includes(${JSON.stringify(text)})`;
const revisionIs = (revision) => `document.querySelector("[data-testid=dictionary-revision]")?.textContent === ${JSON.stringify(String(revision))}`;

function clickButton(page, text) {
  return page.evaluate(`(() => {
    const button = [...document.querySelectorAll("button")].find((b) => b.textContent.trim() === ${JSON.stringify(text)});
    if (!button || button.disabled) throw new Error("no enabled button " + ${JSON.stringify(text)});
    button.click();
  })()`);
}

const value = (page, selector) => page.evaluate(`document.querySelector(${JSON.stringify(selector)}).value`);
const options = (page, selector) => page.evaluate(`[...document.querySelectorAll(${JSON.stringify(selector + " option")})].map((o) => o.value)`);
const entryId = (page) => page.evaluate(`decodeURIComponent(location.pathname.split("/").pop())`);

async function shopJson(path) {
  const response = await fetch(`${shop}${path}`, { cache: "no-store" });
  if (response.status !== 200) fail(`shop ${path}: ${response.status}`);
  return response.json();
}

const listed = async (path, id) => (await shopJson(path)).items.some((item) => item.id === id);

async function openEntry(page, kind, id) {
  await page.navigate(`${admin}/dictionaries/${kind}/${encodeURIComponent(id)}`);
  await page.waitFor(`the ${kind} editor`, `${onPath(`/dictionaries/${kind}/${id}`)} && !!document.querySelector("#dictionary-name")?.value`);
}

async function openProduct(page) {
  await page.navigate(`${admin}/products/p01`);
  await page.waitFor("the product editor", `${onPath("/products/p01")} && !!document.querySelector("#product-name")?.value`);
}

async function create(page, kind, fields, selects = {}) {
  await page.navigate(`${admin}/dictionaries/${kind}/new`);
  await page.waitFor(`the new ${kind} form`, `${onPath(`/dictionaries/${kind}/new`)} && !!document.querySelector("#dictionary-name")`);
  for (const [field, text] of Object.entries(fields)) await page.type(`#dictionary-${field}`, text);
  for (const [field, option] of Object.entries(selects)) await page.select(`#dictionary-${field}`, option);
  await clickButton(page, "Создать");
  await page.waitFor(`the created ${kind}`, `location.pathname.startsWith("/dictionaries/${kind}/") && !location.pathname.endsWith("/new") && ${hasText("Запись создана.")} && ${revisionIs(1)}`);
  return entryId(page);
}

async function main() {
  const startedAt = sql("SELECT now()");
  const staffId = sql(`SELECT id FROM staff_users WHERE lower(email)=lower('${email.replaceAll("'", "''")}')`);
  if (!/^stf_[0-9a-f]{32}$/.test(staffId)) fail("the smoke staff account does not exist");
  const originalCategory = sql("SELECT category_id FROM products WHERE id='p01'");
  const giftMembers = sql("SELECT count(*) FROM collection_products WHERE collection_id='col-gifts'");
  const chrome = await launchChrome();
  try {
    const a = await Page.open(chrome.port);

    // 1. Sign in and open the dictionaries section from the navigation.
    await a.navigate(`${admin}/login`);
    await a.waitFor("the sign-in page", hasText("Вход для сотрудников"));
    await a.type("#staff-email", email);
    await a.type("#staff-password", password);
    await a.click("button[type=submit]");
    await a.waitFor("the shell", `${onPath("/")} && ${hasText("Панель управления")}`);
    await a.click('nav a[href="/dictionaries/categories"]');
    await a.waitFor("the category list", `${onPath("/dictionaries/categories")} && !!document.querySelector('table a[href="/dictionaries/categories/cat-bars"]')`);

    // 2. A subcategory cannot be a parent; a new top-level category appears in the shop.
    await a.navigate(`${admin}/dictionaries/categories/new`);
    await a.waitFor("the category form", "!!document.querySelector('#dictionary-parentId option')");
    const parents = await options(a, "#dictionary-parentId");
    if (parents.includes("cat-bars") || !parents.includes("cat-chocolate")) fail(`parent options: ${parents}`);
    const category = await create(a, "categories", { name: `E2E полка ${run}`, slug: `e2e-shelf-${run}` });
    if (!(await listed("/api/categories", category))) fail("the shop does not list the new category");

    // 3. A manufacturer and its brand.
    const manufacturer = await create(a, "manufacturers", { name: `E2E фабрика ${run}`, slug: `e2e-maker-${run}`, country: "Россия" });
    const brand = await create(a, "brands", { name: `E2E бренд ${run}`, slug: `e2e-brand-${run}` }, { manufacturerId: manufacturer });

    // 4. p01 moves into the new category; the category then refuses to be archived.
    await openProduct(a);
    await a.select("#product-categoryId", category);
    await clickButton(a, "Сохранить изменения");
    await a.waitFor("the product save", hasText("Изменения сохранены."));
    await openEntry(a, "categories", category);
    await clickButton(a, "Перенести в архив…");
    await clickButton(a, "Перенести в архив");
    await a.waitFor("the reference conflict", hasText("Нельзя перенести в архив: запись используется (товаров — 1)"));
    if (sql(`SELECT archived_at IS NULL FROM categories WHERE id='${category}'`) !== "t") fail("a used category was archived");

    // 5. p01 moves back; now the category archives, leaves the shop and the product form.
    await openProduct(a);
    await a.select("#product-categoryId", originalCategory);
    await clickButton(a, "Сохранить изменения");
    await a.waitFor("the product save back", hasText("Изменения сохранены."));
    await openEntry(a, "categories", category);
    await clickButton(a, "Перенести в архив…");
    await clickButton(a, "Перенести в архив");
    await a.waitFor("the archived category", `${hasText("Запись перенесена в архив. Ревизия 2.")} && ${hasText("Восстановить")}`);
    if (await listed("/api/categories", category)) fail("the shop still lists the archived category");
    await openProduct(a);
    if ((await options(a, "#product-categoryId")).includes(category)) fail("the product form offers the archived category");

    // 6. Two tabs on one collection: A saves, B's save is stale, B reloads.
    await openEntry(a, "collections", "col-new");
    const b = await Page.open(chrome.port);
    await openEntry(b, "collections", "col-new");
    const start = Number(await b.evaluate(`document.querySelector("[data-testid=dictionary-revision]").textContent`));
    await a.type("#dictionary-sortOrder", String(Number(await value(a, "#dictionary-sortOrder")) + 1));
    await clickButton(a, "Сохранить изменения");
    await a.waitFor("tab A's save", revisionIs(start + 1));
    await b.type("#dictionary-description", "Изменение вкладки B");
    await clickButton(b, "Сохранить изменения");
    await b.waitFor("the conflict", hasText("Запись была изменена другим сотрудником. Ваши изменения не сохранены."));
    if ((await value(b, "#dictionary-description")) !== "Изменение вкладки B") fail("the conflict overwrote tab B's form");
    await clickButton(b, "Загрузить актуальную версию");
    await b.waitFor("the reloaded server version", `${revisionIs(start + 1)} && ${hasText(`Загружена актуальная версия. Ревизия ${start + 1}.`)}`);

    // 7. Archiving a collection keeps its products; it leaves the shop until restored.
    await openEntry(b, "collections", "col-gifts");
    const giftsRevision = Number(await b.evaluate(`document.querySelector("[data-testid=dictionary-revision]").textContent`));
    await clickButton(b, "Перенести в архив…");
    await b.waitFor("the confirmation", hasText("Товары останутся в ней"));
    await clickButton(b, "Перенести в архив");
    await b.waitFor("the archived collection", hasText(`Запись перенесена в архив. Ревизия ${giftsRevision + 1}.`));
    if (await listed("/api/collections", "col-gifts")) fail("the shop still lists the archived collection");
    if ((await shopJson("/api/catalog?collection=gifts-under-2000")).total !== 0) fail("the shop still filters by the archived collection");
    if (sql("SELECT count(*) FROM collection_products WHERE collection_id='col-gifts'") !== giftMembers) fail("archiving dropped memberships");
    await clickButton(b, "Восстановить");
    await b.waitFor("the restored collection", hasText(`Запись восстановлена. Ревизия ${giftsRevision + 2}.`));
    if (!(await listed("/api/collections", "col-gifts"))) fail("the restored collection is not listed");
    b.close();

    // 8. The restored category is offered again; the brand belongs to its manufacturer.
    await openEntry(a, "categories", category);
    await clickButton(a, "Восстановить");
    await a.waitFor("the restored category", hasText("Запись восстановлена. Ревизия 3."));
    await openEntry(a, "brands", brand);
    if ((await value(a, "#dictionary-manufacturerId")) !== manufacturer) fail("the brand lost its manufacturer");

    // 9. Sign out.
    await clickButton(a, "Выйти");
    await a.waitFor("the sign-in page after sign-out", onPath("/login"));
    a.close();

    // 10. Audit: one row per committed dictionary mutation, none for the refused archive or the stale save.
    const rows = sql(`SELECT action, staff_user_id, entity_id FROM staff_audit_events
      WHERE created_at >= '${startedAt}' AND entity_type IN ('category','manufacturer','brand','collection') ORDER BY created_at, id`).split("\n").filter(Boolean);
    const expected = [
      ["category.create", category], ["manufacturer.create", manufacturer], ["brand.create", brand], ["category.archive", category],
      ["collection.update", "col-new"], ["collection.archive", "col-gifts"], ["collection.restore", "col-gifts"], ["category.restore", category],
    ].map(([action, id]) => `${action}|${staffId}|${id}`);
    if (JSON.stringify(rows) !== JSON.stringify(expected)) fail(`audit rows:\n${rows.join("\n")}`);
    if (sql("SELECT category_id FROM products WHERE id='p01'") !== originalCategory) fail("p01 is not back in its category");
    console.log(`admin catalog flow passed for ${admin}: ${rows.length} dictionary audit events, reference conflict and stale save refused`);
  } finally {
    chrome.child.kill("SIGKILL");
    await rm(chrome.profile, { recursive: true, force: true }).catch(() => {});
  }
}

await main();
