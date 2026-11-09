#!/usr/bin/env node
// C3: legacy media migration through the real stack (ADR 021). Runs shopctl
// dry-run, apply and a second apply, and a real Chrome admin + storefront +
// checkout journey before and after. Disposable data only: it inserts c3-*
// fixture products, migrates the whole catalog and places an order.
//
//   ADMIN_ORIGIN, SHOP_ORIGIN, ADMIN_SMOKE_EMAIL, ADMIN_SMOKE_PASSWORD as in
//   test-admin-media-flow.mjs. COMPOSE_PROJECT selects the Compose stack.
//   C3_SHOPCTL (optional) runs a local shopctl binary instead of the app
//   container; C2_PG_SOCKET selects a local PostgreSQL as in the C2 journey.
import { execFileSync, spawnSync } from "node:child_process";
import { rm } from "node:fs/promises";
import { resolve } from "node:path";
import { launchChrome, Page } from "./lib/admin-chrome.mjs";

const admin = process.env.ADMIN_ORIGIN ?? "https://localhost:8443";
const shop = process.env.SHOP_ORIGIN ?? "http://127.0.0.1:8080";
const project = process.env.COMPOSE_PROJECT ?? "sweetshop-edge-smoke";
const email = process.env.ADMIN_SMOKE_EMAIL;
const password = process.env.ADMIN_SMOKE_PASSWORD;
if (!email || !password) throw new Error("Staff smoke credentials are required");
const compose = ["compose", "-p", project, "--env-file", ".env.example"];
const q = JSON.stringify;
const assert = (value, message) => { if (!value) throw new Error(`legacy media flow: ${message}`); };
const sql = (query) => process.env.C2_PG_SOCKET
  ? execFileSync("psql", ["-h", process.env.C2_PG_SOCKET, "-U", "shop_test_admin", "-d", process.env.C2_PG_DATABASE ?? "postgres", "-tA", "-c", query], { encoding: "utf8" }).trim()
  : execFileSync("docker", [...compose, "exec", "-T", "postgres", "psql", "-U", "sweetshop", "-d", "sweetshop", "-tA", "-c", query], { encoding: "utf8" }).trim();

// Returns the exit status and the parsed JSON report of one shopctl media call.
function shopctl(...args) {
  const [command, argv] = process.env.C3_SHOPCTL
    ? [process.env.C3_SHOPCTL, ["media", ...args]]
    : ["docker", [...compose, "exec", "-T", "app", "/app/shopctl", "media", ...args]];
  const result = spawnSync(command, argv, { encoding: "utf8", timeout: 120_000 });
  if (result.error) throw result.error;
  const line = result.stdout.trim().split("\n").filter(Boolean).pop() ?? "null";
  let report = null;
  try { report = JSON.parse(line); } catch { /* verify prints the report on stdout too */ }
  return { status: result.status, stdout: result.stdout, stderr: result.stderr, report };
}
const migrate = (...args) => shopctl("migrate-legacy", ...args);
const applyFlags = ["--apply", "--staff-email", email];
const entry = (report, id) => report.products.find((item) => item.id === id);
const state = () => sql(`SELECT (SELECT count(*) FROM media_assets)||'|'||(SELECT count(*) FROM product_media)||'|'||(SELECT count(*) FROM staff_audit_events)`);
const orders = () => sql(`SELECT string_agg(order_id||':'||position||':'||image,',' ORDER BY order_id,position) FROM order_items`);

const text = (value) => `document.body.innerText.includes(${q(value)})`;
const revision = (page) => page.evaluate('Number(document.querySelector("[data-testid=product-revision]").textContent)');
async function navigate(page, url) {
  await page.send("Page.bringToFront");
  await page.navigate(url);
}
async function button(page, name, scope = "document") {
  await page.send("Page.bringToFront");
  const expression = `[...${scope}.querySelectorAll('button')].find(b => b.textContent.trim() === ${q(name)})`;
  await page.waitFor(`enabled ${name}`, `!!${expression} && !${expression}.matches(':disabled')`);
  await page.evaluate(`${expression}.click()`);
}
const candidate = (id) => `document.querySelector('[aria-label="Доступные изображения"] [data-media-id="${id}"]')`;
const loaded = (selector) => `(() => { const image = document.querySelector(${q(selector)}); return !!image && image.complete && image.naturalWidth > 0; })()`;
async function openProduct(page, id) {
  await navigate(page, `${admin}/products/${id}`);
  await page.waitFor("the media editor", `!!document.querySelector('#product-media-upload') && document.querySelector('#product-name')?.value !== ''`);
}
async function saveMedia(page) {
  const before = await revision(page);
  await button(page, "Сохранить изображения");
  await page.waitFor("the saved media revision", `document.querySelector('[data-testid=product-revision]').textContent === ${q(String(before + 1))}`);
}
async function storefront(page, slug, primary, gallery) {
  await navigate(page, `${shop}/products/${slug}`);
  await page.waitFor("the managed primary", loaded(`main img[src="/media/${primary}"]`));
  const thumbnails = await page.evaluate(`[...document.querySelectorAll('[aria-label="Фотографии товара"] img')].map(i=>i.getAttribute('src'))`);
  const expected = gallery.length ? [primary, ...gallery].map((id) => `/media/${id}`) : [];
  assert(JSON.stringify(thumbnails) === JSON.stringify(expected), `gallery order ${JSON.stringify(thumbnails)}`);
}

const fixture = (id, name, image, gallery) => `
  INSERT INTO products(id,slug,sku,name,short_name,price_minor,unit,category_id,manufacturer_id,brand_id,image,gallery,stock)
  SELECT '${id}','${id}',upper('${id}'),'${name}','${name}',price_minor,unit,category_id,manufacturer_id,brand_id,'${image}',ARRAY[${gallery.map((path) => `'${path}'`).join(",")}]::text[],50 FROM products WHERE id='p02'`;

const customerEmail = process.env.SMOKE_EMAIL ?? "demo@sugar.ru";
const chocolate = "/assets/product-chocolate.webp", giftbox = "/assets/product-giftbox.webp", marshmallow = "/assets/product-marshmallow.webp";
const chrome = await launchChrome();
const pages = [];
try {
  sql(fixture("c3-legacy", "Проба C3 легаси", chocolate, [chocolate, giftbox, marshmallow]));
  sql(fixture("c3-managed", "Проба C3 управляемый", chocolate, [giftbox]));
  sql(fixture("c3-empty", "Проба C3 без фото", "", []));
  sql(fixture("c3-missing", "Проба C3 нет файла", "/assets/c3-missing.webp", []));

  const customer = await Page.open(chrome.port); pages.push(customer);
  await navigate(customer, `${shop}/login`);
  await customer.type("#login-email", customerEmail);
  await customer.type("#login-password", process.env.SMOKE_PASSWORD ?? "Sweet2026!");
  await button(customer, "Войти");
  await customer.waitFor("the customer session", "location.pathname !== '/login'");
  const staff = await Page.open(chrome.port); pages.push(staff);
  await navigate(staff, `${admin}/login`);
  await staff.type("#staff-email", email); await staff.type("#staff-password", password);
  await staff.click('button[type="submit"]');
  await staff.waitFor("staff sign in", text("Панель управления"));

  // 1–2: before the migration the legacy product still renders from /assets.
  await navigate(customer, `${shop}/products/c3-legacy`);
  await customer.waitFor("the legacy image", loaded(`main img[src="${chocolate}"]`));
  await navigate(customer, `${shop}/account/orders/ord-2026-002`);
  await customer.waitFor("the historical legacy snapshot", loaded('img[src^="/assets/"]'));

  // A product staff already configured (C2): must not be overwritten.
  await openProduct(staff, "c3-managed");
  const { root } = await staff.send("DOM.getDocument");
  const { nodeId } = await staff.send("DOM.querySelector", { nodeId: root.nodeId, selector: "#product-media-upload" });
  await staff.send("DOM.setFileInputFiles", { nodeId, files: [resolve("web/assets/product-giftbox.webp")] });
  await staff.waitFor("the uploaded candidate", `!!document.querySelector('[aria-label="Доступные изображения"] [data-media-id]')`);
  const managedId = sql("SELECT id FROM media_assets ORDER BY created_at DESC,id LIMIT 1");
  await button(staff, "Основное", candidate(managedId));
  await saveMedia(staff);
  const managedRevision = Number(sql("SELECT revision FROM products WHERE id='c3-managed'"));

  // 3: dry run reports everything and changes nothing.
  const before = state();
  const historical = orders();
  const dry = migrate();
  assert(dry.status === 3 && dry.report?.mode === "dry-run", `dry run: ${dry.status} ${dry.stderr}`);
  assert(state() === before && orders() === historical, "dry run changed the database");
  for (const [id, status] of [["c3-legacy", "needs_migration"], ["c3-managed", "already_managed"], ["c3-empty", "no_media"], ["c3-missing", "failed"]]) {
    assert(entry(dry.report, id)?.status === status, `dry run ${id}: ${q(entry(dry.report, id))}`);
  }
  assert(entry(dry.report, "c3-missing").problems[0].kind === "missing_file", "missing file not classified");
  assert(entry(dry.report, "c3-legacy").gallery.length === 2, "dry run mapping");
  assert(dry.report.missingFiles.includes("/assets/c3-missing.webp"), "dry run missing report");
  const applied = migrate(...applyFlags);
  assert(applied.status === 3 && applied.report?.mode === "apply", `apply: ${applied.status} ${applied.stderr}`);

  // 4–5: every non-broken product is now managed.
  assert(entry(applied.report, "c3-legacy").status === "migrated", "c3-legacy not migrated");
  assert(entry(applied.report, "c3-missing").status === "failed", "c3-missing not reported");
  const media = entry(applied.report, "c3-legacy");
  const [primary, first, second] = [media.primary.assetId, media.gallery[0].assetId, media.gallery[1].assetId];
  await storefront(customer, "c3-legacy", primary, [first, second]);
  assert(Number(sql("SELECT revision FROM products WHERE id='c3-managed'")) === managedRevision, "already-managed revision moved");
  assert(sql("SELECT count(*) FROM product_media WHERE product_id='c3-managed' AND role='primary' AND media_id='" + managedId + "'") === "1", "already-managed media changed");
  assert(sql("SELECT image FROM order_items WHERE order_id='ord-2026-002' AND position=1").startsWith("/assets/"), "seeded snapshot rewritten");

  // Customer flows read the migrated images.
  await navigate(customer, `${shop}/products/c3-empty`);
  await customer.waitFor("the no-image placeholder", text("Изображение товара пока недоступно"));
  await navigate(customer, `${shop}/search?q=${encodeURIComponent("Проба C3 легаси")}`);
  await customer.waitFor("search result image", loaded(`img[src="/media/${primary}"]`));
  const added = await customer.evaluate(`fetch("/api/auth/session").then(r=>r.json()).then(s=>fetch("/api/favorites/c3-legacy",{method:"PUT",headers:{"X-CSRF-Token":s.csrfToken}})).then(r=>r.status)`);
  assert(added === 200, `favorite: ${added}`);
  await navigate(customer, `${shop}/favorites`);
  await customer.waitFor("favorite image", loaded(`img[src="/media/${primary}"]`));
  sql(`DELETE FROM cart_items WHERE user_id=(SELECT id FROM users WHERE lower(email)=lower('${customerEmail.replaceAll("'", "''")}'))`);
  await navigate(customer, `${shop}/products/c3-legacy`);
  const add = `document.querySelector('button[aria-label^="Добавить в корзину:"]')`;
  await customer.waitFor("the purchase control", `!!${add} && ${add}.getAttribute('aria-disabled') !== 'true'`);
  await customer.click('button[aria-label^="Добавить в корзину:"]');
  await customer.waitFor("cart added", text("Перейти в корзину"));
  await navigate(customer, `${shop}/cart`);
  await customer.waitFor("cart image", loaded(`img[src="/media/${primary}"]`));

  // 6–8: admin shows migrated media, reorder reaches the storefront.
  await openProduct(staff, "c3-legacy");
  await staff.waitFor("managed primary preview", `!!document.querySelector('[data-testid=media-primary] img[src="/media/${primary}"]')`);
  assert(!(await staff.evaluate(text("/assets/"))), "admin still shows a legacy path");
  await staff.click('button[aria-label="Фото 2: вверх"]');
  await saveMedia(staff);
  await storefront(customer, "c3-legacy", primary, [second, first]);
  // Stale revision: a second tab holds the old state and is refused.
  const stale = await Page.open(chrome.port); pages.push(stale);
  await openProduct(stale, "c3-legacy");
  await staff.click('button[aria-label="Фото 2: вверх"]');
  await saveMedia(staff);
  await stale.click('button[aria-label="Фото 1: вниз"]');
  await button(stale, "Сохранить изображения");
  await stale.waitFor("the stale conflict", text("Товар был изменён другим сотрудником. Ваши изменения не сохранены."));
  await storefront(customer, "c3-legacy", primary, [first, second]);

  // 10: a new order snapshots the managed primary.
  await navigate(customer, `${shop}/checkout`);
  await customer.type("#checkout-address", "Москва, Тестовая улица, 1 (C3 smoke)");
  await button(customer, "Подтвердить заказ");
  await customer.waitFor("order placed", "/^\\/account\\/orders\\/[^/]+$/.test(location.pathname)");
  const newOrder = await customer.evaluate("location.pathname");
  await customer.waitFor("order managed snapshot", loaded(`img[src="/media/${primary}"]`));
  const newOrderId = newOrder.split("/").pop();
  assert(sql(`SELECT count(*) FROM order_items WHERE order_id='${newOrderId}' AND image='/media/${primary}'`) === "1", "new order snapshot not managed");

  // 9: the historical order keeps its original legacy snapshot and repeat order
  // still works from it.
  await navigate(customer, `${shop}/account/orders/ord-2026-002`);
  await customer.waitFor("the historical legacy snapshot", loaded('img[src^="/assets/"]'));
  const legacyStatus = (await fetch(`${shop}/assets/product-chocolate.webp`, { cache: "no-store" })).status;
  assert(legacyStatus === 200, `historical asset unavailable: ${legacyStatus}`);
  await navigate(customer, `${shop}/account/orders/ord-2026-002/repeat`);
  await customer.waitFor("the repeat preview", `${text("Повторить заказ")} || ${text("Повтор заказа")}`);
  await navigate(customer, `${shop}${newOrder}`);
  await customer.waitFor("new order still managed", loaded(`img[src="/media/${primary}"]`));

  // Second apply: only the broken fixture is still reported; nothing moves.
  const settled = state();
  const again = migrate(...applyFlags);
  assert(again.status === 3 && again.report.summary.migrated === 0 && again.report.summary.failed === 1, `second apply: ${q(again.report?.summary)}`);
  assert(state() === settled, "second apply changed the database");
  assert(entry(again.report, "c3-legacy").status === "already_migrated", "second apply reclassified");

  // 11: with the broken fixture resolved no active product depends on /assets.
  sql("UPDATE products SET image='' WHERE id='c3-missing'");
  const finalReport = migrate();
  assert(finalReport.status === 0, `final dry run: ${finalReport.status} ${q(finalReport.report?.summary)}`);
  assert(finalReport.report.activeLegacyProducts.length === 0, `active legacy: ${finalReport.report.activeLegacyProducts}`);
  assert(finalReport.report.safeToDelete.length === 0, `safe to delete: ${finalReport.report.safeToDelete}`);
  const verify = shopctl("verify");
  assert(verify.status === 0, `media verify ${verify.status}: ${verify.stdout} ${verify.stderr}`);
  console.log(q({ result: "PASS", migrated: applied.report.summary.migrated, applied: applied.report.summary, retention: finalReport.report.files.map((file) => [file.path, file.category]) }));
} finally {
  for (const page of pages) page.close();
  const exited = new Promise((done) => chrome.child.once("exit", done));
  chrome.child.kill("SIGTERM"); await exited;
  await rm(chrome.profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
}
