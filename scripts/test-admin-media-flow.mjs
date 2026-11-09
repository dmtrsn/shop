#!/usr/bin/env node
// C2: real Chrome admin + storefront + checkout journey. Disposable Compose
// data only. Run under with-admin-tls-proxy.sh, with the same environment as
// test-admin-product-flow.mjs. No legacy rows are updated or files migrated.
import { execFileSync } from "node:child_process";
import { mkdir, rm, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { launchChrome, Page } from "./lib/admin-chrome.mjs";

const admin = process.env.ADMIN_ORIGIN ?? "https://localhost:8443";
const shop = process.env.SHOP_ORIGIN ?? "http://127.0.0.1:8080";
const project = process.env.COMPOSE_PROJECT ?? "sweetshop-edge-smoke";
const email = process.env.ADMIN_SMOKE_EMAIL;
const password = process.env.ADMIN_SMOKE_PASSWORD;
if (!email || !password) throw new Error("Staff smoke credentials are required");
const q = JSON.stringify;
const assert = (value, message) => { if (!value) throw new Error(message); };
const sql = (query) => process.env.C2_PG_SOCKET
  ? execFileSync("psql",["-h",process.env.C2_PG_SOCKET,"-U","shop_test_admin","-d",process.env.C2_PG_DATABASE ?? "postgres","-tA","-c",query],{encoding:"utf8"}).trim()
  : execFileSync("docker", ["compose", "-p", project, "--env-file", ".env.example", "exec", "-T", "postgres", "psql", "-U", "sweetshop", "-d", "sweetshop", "-tA", "-c", query], { encoding: "utf8" }).trim();
const text = (value) => `document.body.innerText.includes(${q(value)})`;
const revision = (page) => page.evaluate('Number(document.querySelector("[data-testid=product-revision]").textContent)');
// Streamed React content can defer paint while its Chrome tab is backgrounded.
// Model the operator actually switching tabs before navigation/interactions.
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
async function openProduct(page) {
  await navigate(page,`${admin}/products/p01`);
  await page.waitFor("media editor", "!!document.querySelector('#product-media-upload') && !!document.querySelector('#product-name')?.value");
}
async function save(page) {
  const before = await revision(page);
  await button(page, "Сохранить изображения");
  await page.waitFor("saved media revision", `document.querySelector('[data-testid=product-revision]').textContent === ${q(String(before + 1))}`);
}
async function upload(page, filename) {
  const before = sql("SELECT count(*) FROM media_assets");
  const oldIds = await page.evaluate(`[...document.querySelectorAll('[aria-label="Доступные изображения"] [data-media-id]')].map(e=>e.dataset.mediaId)`);
  const { root } = await page.send("DOM.getDocument");
  const { nodeId } = await page.send("DOM.querySelector", { nodeId: root.nodeId, selector: "#product-media-upload" });
  await page.send("DOM.setFileInputFiles", { nodeId, files: [resolve(filename)] });
  await page.waitFor("upload preview", `[...document.querySelectorAll('[aria-label="Доступные изображения"] [data-media-id]')].some(e=>!${q(oldIds)}.includes(e.dataset.mediaId)) && !${text("Загружаем изображение…")}`);
  // Query only identifiers; no user/customer data enters the browser logs.
  const id = sql("SELECT id FROM media_assets ORDER BY created_at DESC,id LIMIT 1");
  await page.waitFor("new uploaded candidate", `!!${candidate(id)} && ${candidate(id)}.querySelector('img').naturalWidth > 0`);
  assert(Number(sql("SELECT count(*) FROM media_assets")) === Number(before) + 1, "upload did not create exactly one asset");
  return id;
}
async function storefront(page, primary, gallery) {
  await navigate(page,`${shop}/products/dark-chocolate-sea-salt`);
  await page.waitFor("storefront managed primary", `!!document.querySelector('main img[src="/media/${primary}"]') && document.querySelector('main img[src="/media/${primary}"]').naturalWidth > 0`);
  await page.waitFor("hydrated storefront gallery", `(() => { const image = document.querySelector('main img[src="/media/${primary}"]'); return !!image && image.getClientRects().length > 0 && Object.keys(image).some(key=>key.startsWith('__reactProps')); })()`);
  const thumbnails = await page.evaluate(`[...document.querySelectorAll('[aria-label="Фотографии товара"] img')].map(i=>i.getAttribute('src'))`);
  // A single image renders no thumbnail strip.
  const expected = gallery.length ? [primary,...gallery].map(id=>`/media/${id}`) : [];
  assert(JSON.stringify(thumbnails) === JSON.stringify(expected), `gallery order ${JSON.stringify(thumbnails)}`);
}

const chrome = await launchChrome();
const pages = [];
try {
  const started = sql("SELECT now()");
  const a = await Page.open(chrome.port); pages.push(a);
  await navigate(a,`${admin}/login`);
  await a.type("#staff-email", email); await a.type("#staff-password", password);
  await a.click('button[type="submit"]');
  await a.waitFor("staff sign in", text("Панель управления"));
  await openProduct(a);
  const startRevision = await revision(a);
  const imageA = await upload(a,"web/assets/product-chocolate.webp");
  await button(a,"Основное",candidate(imageA));
  const imageB = await upload(a,"web/assets/product-giftbox.webp");
  await button(a,"В галерею",candidate(imageB));
  const imageC = await upload(a,"web/assets/product-chocolate.webp");
  await button(a,"В галерею",candidate(imageC));
  await save(a);
  const customer = await Page.open(chrome.port); pages.push(customer);
  await customer.send("Log.enable");
  customer.listeners.add((message) => {
    if (message.method === "Log.entryAdded" && message.params.entry.level === "error") console.error("Customer browser log:", message.params.entry.text);
    if (message.method === "Runtime.exceptionThrown") console.error("Customer browser exception:", message.params.exceptionDetails.exception?.description ?? message.params.exceptionDetails.text);
  });
  await storefront(customer,imageA,[imageB,imageC]);
  await a.click('button[aria-label="Фото 2: вверх"]'); await save(a);
  await storefront(customer,imageA,[imageC,imageB]);

  const b = await Page.open(chrome.port); pages.push(b); await openProduct(b);
  await a.click('button[aria-label="Фото 2: вверх"]'); await save(a);
  await b.click('button[aria-label="Убрать фото 1 из товара"]');
  await button(b,"Сохранить изображения");
  await b.waitFor("explicit stale conflict",text("Товар был изменён другим сотрудником. Ваши изменения не сохранены."));
  assert(await revision(b) === startRevision + 2,"stale save changed local revision");
  await button(b,"Загрузить актуальную версию");
  await b.waitFor("reloaded revision",`document.querySelector('[data-testid=product-revision]').textContent === ${q(String(startRevision+3))}`);

  // Detach is local until saved: explicit delete is refused while the server
  // still references B. After save B is free and global deletion succeeds.
  await b.click('button[aria-label="Убрать фото 1 из товара"]');
  await button(b,"Удалить файл…",candidate(imageB)); await button(b,"Удалить файл навсегда");
  await b.waitFor("reference conflict",text("Удаление заблокировано"));
  await save(b);
  await storefront(customer,imageA,[imageC]);
  await button(b,"Удалить файл…",candidate(imageB)); await button(b,"Удалить файл навсегда");
  await b.waitFor("asset removed from picker",`!${candidate(imageB)}`);
  assert((await fetch(`${shop}/media/${imageB}`,{cache:"no-store"})).status === 404,"safe delete still publicly served");

  // Customer signs in and checks out in the real storefront UI. Earlier journeys
  // on a shared disposable stack may leave lines in this cart; start it empty.
  const customerEmail = process.env.SMOKE_EMAIL ?? "demo@sugar.ru";
  sql(`DELETE FROM cart_items WHERE user_id=(SELECT id FROM users WHERE lower(email)=lower('${customerEmail.replaceAll("'","''")}'))`);
  await navigate(customer,`${shop}/login`);
  await customer.type("#login-email",customerEmail);
  await customer.type("#login-password",process.env.SMOKE_PASSWORD ?? "Sweet2026!");
  await button(customer,"Войти");
  await customer.waitFor("customer signed in","location.pathname !== '/login'");
  await navigate(customer,`${shop}/products/dark-chocolate-sea-salt`);
  const add = `document.querySelector('button[aria-label^="Добавить в корзину:"]')`;
  await customer.waitFor("settled purchase control", `${text("Перейти в корзину")} || (!!${add} && ${add}.getAttribute('aria-disabled') !== 'true')`);
  if (!(await customer.evaluate(text("Перейти в корзину")))) {
    await customer.click('button[aria-label^="Добавить в корзину:"]');
    await customer.waitFor("cart added",text("Перейти в корзину"));
  }
  await navigate(customer,`${shop}/checkout`);
  await customer.type("#checkout-address","Москва, Тестовая улица, 1 (C2 smoke)");
  await button(customer,"Подтвердить заказ");
  await customer.waitFor("order placed","/^\\/account\\/orders\\/[^/]+$/.test(location.pathname)");
  const orderPath = await customer.evaluate("location.pathname");
  await customer.waitFor("order original snapshot",`!!document.querySelector('img[src="/media/${imageA}"]')`);
  // Checkout bumps the product revision; explicitly load it before editing.
  await openProduct(b);
  await button(b,"Основное",candidate(imageC)); await save(b);
  await storefront(customer,imageC,[]);
  await navigate(customer,`${shop}${orderPath}`);
  await customer.waitFor("historical image survives primary change",`!!document.querySelector('img[src="/media/${imageA}"]') && document.querySelector('img[src="/media/${imageA}"]').naturalWidth > 0`);
  await button(b,"Удалить файл…",candidate(imageA)); await button(b,"Удалить файл навсегда");
  await b.waitFor("historical reference refusal",text("заказов — 1"));
  assert((await fetch(`${shop}/media/${imageA}`,{cache:"no-store"})).status === 200,"order snapshot unpublished");

  // A new legacy-only fixture copies data into a NEW row. Existing legacy
  // image/gallery and historical snapshot rows are never rewritten.
  sql(`INSERT INTO products(id,slug,sku,name,price_minor,unit,category_id,manufacturer_id,brand_id,image,gallery,stock) SELECT 'c2-legacy','c2-legacy','C2-LEGACY',name,price_minor,unit,category_id,manufacturer_id,brand_id,image,gallery,stock FROM products WHERE id='p02'`);
  await navigate(customer,`${shop}/products/c2-legacy`);
  await customer.waitFor("legacy image",`!!document.querySelector('main img[src^="/assets/"]') && document.querySelector('main img[src^="/assets/"]').naturalWidth > 0`);

  // Capture and measure the media section at desktop and a narrow viewport.
  await mkdir("artifacts/c2",{recursive:true});
  // CDP screenshots never complete for a background tab.
  await b.send("Page.bringToFront");
  for (const width of [1440,768,375]) {
    await b.send("Emulation.setDeviceMetricsOverride",{width,height:1000,deviceScaleFactor:1,mobile:false});
    await b.evaluate("document.querySelector('#product-media').scrollIntoView()");
    assert(await b.evaluate("document.documentElement.scrollWidth <= innerWidth"),`horizontal overflow at ${width}`);
    const shot=await b.send("Page.captureScreenshot",{format:"png"});
    await writeFile(`artifacts/c2/admin-media-${width}.png`,Buffer.from(shot.data,"base64"));
  }
  const events = sql(`SELECT action||'|'||entity_type||'|'||entity_id FROM staff_audit_events WHERE created_at >= '${started}' ORDER BY created_at,id`).split("\n");
  assert(events.filter(e=>e.startsWith("media.upload|")).length===3,"upload audit count");
  assert(events.filter(e=>e.startsWith("product.media_update|product|p01")).length===5,"committed media audit count");
  assert(events.filter(e=>e.startsWith("media.delete|")).length===1,"delete audit count");
  assert(sql(`SELECT count(*) FROM staff_audit_events WHERE created_at >= '${started}' AND (request_id='' OR staff_user_id IS NULL)`) === "0","audit actor/request missing");
  console.log(JSON.stringify({ result:"PASS",startRevision,media:[imageA,imageB,imageC],orderPath,auditEvents:events,viewports:[1440,768,375] },null,2));
} finally {
  for (const page of pages) page.close();
  const exited = new Promise(resolve=>chrome.child.once("exit",resolve));
  chrome.child.kill("SIGTERM"); await exited;
  await rm(chrome.profile,{recursive:true,force:true,maxRetries:5,retryDelay:200});
}
