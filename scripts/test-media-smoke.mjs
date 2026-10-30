#!/usr/bin/env node
// Managed media through the real stack (ADR 019): admin edge → Go → media
// volume and PostgreSQL, public delivery through the shop and admin edges,
// persistence across an app container replacement, reference-safe deletion,
// audit rows and `shopctl media verify`. No browser: C1 has no media UI.
// It attaches the upload to the demo product p01 through SQL (the attach API
// arrives in C2) and detaches it again, so run it only against a disposable
// database (CI or a throwaway Compose project).
//
//   ADMIN_ORIGIN   default http://localhost:8081 (the admin edge)
//   SHOP_ORIGIN    default http://127.0.0.1:8080 (the shop edge)
//   ADMIN_SMOKE_EMAIL / ADMIN_SMOKE_PASSWORD   a staff account provisioned with shopctl
//   COMPOSE_PROJECT  default sweetshop-edge-smoke
//
// Uses one staff sign-in attempt of the admin edge bucket.
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";

const admin = process.env.ADMIN_ORIGIN ?? "http://localhost:8081";
const shop = process.env.SHOP_ORIGIN ?? "http://127.0.0.1:8080";
const email = process.env.ADMIN_SMOKE_EMAIL;
const password = process.env.ADMIN_SMOKE_PASSWORD;
const project = process.env.COMPOSE_PROJECT ?? "sweetshop-edge-smoke";
if (!email || !password) throw new Error("ADMIN_SMOKE_EMAIL and ADMIN_SMOKE_PASSWORD are required");

function fail(message) {
  throw new Error(`media smoke: ${message}`);
}

function compose(...args) {
  return execFileSync("docker", ["compose", "-p", project, "--env-file", ".env.example", ...args], { encoding: "utf8" }).trim();
}

const sql = (query) => compose("exec", "-T", "postgres", "psql", "-U", process.env.POSTGRES_USER ?? "sweetshop", "-d", process.env.POSTGRES_DB ?? "sweetshop", "-tA", "-F", "|", "-c", query);

// The width and height of a baseline or progressive JPEG, from its SOF segment.
function jpegSize(bytes) {
  for (let offset = 2; offset + 9 < bytes.length; ) {
    if (bytes[offset] !== 0xff) fail("not a JPEG marker stream");
    const marker = bytes[offset + 1];
    const length = bytes.readUInt16BE(offset + 2);
    if (marker >= 0xc0 && marker <= 0xc3) return { height: bytes.readUInt16BE(offset + 5), width: bytes.readUInt16BE(offset + 7) };
    offset += 2 + length;
  }
  fail("no JPEG frame header");
}

async function main() {
  const startedAt = sql("SELECT now()");
  const staffId = sql(`SELECT id FROM staff_users WHERE lower(email)=lower('${email.replaceAll("'", "''")}')`);
  if (!/^stf_[0-9a-f]{32}$/.test(staffId)) fail("the smoke staff account does not exist");

  // 1. Sign in through the admin edge.
  const login = await fetch(`${admin}/api/admin/v1/auth/session`, {
    method: "POST", headers: { Origin: admin, "Content-Type": "application/json" }, body: JSON.stringify({ email, password }),
  });
  if (login.status !== 200) fail(`sign-in ${login.status}`);
  const cookie = login.headers.getSetCookie().find((c) => /sweetshop_staff=/.test(c))?.split(";")[0];
  const { csrfToken } = await login.json();
  if (!cookie || !csrfToken) fail("no staff cookie or CSRF token");
  const staff = (method, path, init = {}) =>
    fetch(`${admin}${path}`, { method, ...init, headers: { Cookie: cookie, Origin: admin, "X-CSRF-Token": csrfToken, "X-Request-ID": "media-smoke", ...init.headers } });

  // 2. Upload the catalog's own WebP; Go stores a normalized JPEG.
  const source = await readFile("web/assets/product-giftbox.webp");
  const form = new FormData();
  form.append("file", new Blob([source], { type: "text/plain" }), "../../not-a-path.svg");
  const upload = await staff("POST", "/api/admin/v1/media", { body: form });
  if (upload.status !== 201) fail(`upload ${upload.status} ${await upload.text()}`);
  const asset = await upload.json();
  if (!/^med_[0-9a-f]{32}$/.test(asset.id) || asset.contentType !== "image/jpeg" || asset.width !== 1254 || asset.height !== 1254 || asset.url !== `/media/${asset.id}`) {
    fail(`metadata ${JSON.stringify(asset)}`);
  }

  // 3. The bytes survive replacing the app container: they live in the volume.
  compose("up", "-d", "--no-deps", "--force-recreate", "--wait", "--wait-timeout", "120", "app");

  // 4. Public delivery through both edges: exact bytes, server-derived headers.
  let etag = "";
  for (const origin of [shop, admin]) {
    const response = await fetch(`${origin}${asset.url}`);
    const bytes = Buffer.from(await response.arrayBuffer());
    const digest = createHash("sha256").update(bytes).digest("hex");
    const size = response.status === 200 ? jpegSize(bytes) : null;
    if (response.status !== 200 || digest !== asset.sha256 || bytes.length !== asset.sizeBytes || size.width !== 1254 || size.height !== 1254) {
      fail(`${origin} GET ${response.status}`);
    }
    const headers = response.headers;
    if (headers.get("content-type") !== "image/jpeg" || headers.get("x-content-type-options") !== "nosniff" || headers.get("etag") !== `"${asset.sha256}"` ||
      headers.get("cache-control") !== "public, max-age=31536000, immutable" || headers.get("content-disposition")) {
      fail(`${origin} headers ${JSON.stringify([...headers])}`);
    }
    etag = headers.get("etag");
    const conditional = await fetch(`${origin}${asset.url}`, { headers: { "If-None-Match": etag } });
    if (conditional.status !== 304 || (await conditional.arrayBuffer()).byteLength !== 0) fail(`${origin} conditional ${conditional.status}`);
    const head = await fetch(`${origin}${asset.url}`, { method: "HEAD" });
    if (head.status !== 200 || head.headers.get("content-length") !== String(asset.sizeBytes)) fail(`${origin} HEAD ${head.status}`);
  }

  // 5. While a product references it, it cannot be deleted.
  sql(`INSERT INTO product_media(product_id,media_id,role,position) VALUES ('p01','${asset.id}','gallery',0)`);
  const blocked = await staff("DELETE", `/api/admin/v1/media/${asset.id}`);
  const conflict = await blocked.json();
  if (blocked.status !== 409 || conflict.error.code !== "reference_conflict" || conflict.error.references.products !== 1) fail(`referenced delete ${blocked.status}`);
  if ((await fetch(`${shop}${asset.url}`)).status !== 200) fail("a refused delete unpublished the asset");
  sql(`DELETE FROM product_media WHERE media_id='${asset.id}'`);

  // 6. Delete: 204, then 404 on both edges and on the staff read.
  const removed = await staff("DELETE", `/api/admin/v1/media/${asset.id}`);
  if (removed.status !== 204) fail(`delete ${removed.status}`);
  for (const origin of [shop, admin]) {
    if ((await fetch(`${origin}${asset.url}`, { headers: { "If-None-Match": etag } })).status !== 404) fail(`${origin} still serves the deleted asset`);
  }
  if ((await staff("GET", `/api/admin/v1/media/${asset.id}`)).status !== 404) fail("the staff read still shows the deleted asset");

  // 7. Audit: one row per committed mutation, none for the refused delete.
  const rows = sql(`SELECT action, staff_user_id, entity_type, entity_id, request_id FROM staff_audit_events
    WHERE entity_type='media' AND created_at >= '${startedAt}' ORDER BY created_at, id`).split("\n").filter(Boolean);
  const expected = ["media.upload", "media.delete"].map((action) => `${action}|${staffId}|media|${asset.id}|media-smoke`);
  if (JSON.stringify(rows) !== JSON.stringify(expected)) fail(`audit rows:\n${rows.join("\n")}`);

  // 8. The store and the metadata agree.
  const report = JSON.parse(compose("run", "--rm", "--no-deps", "-T", "--entrypoint", "/app/shopctl", "app", "media", "verify"));
  if (report.missing.length || report.deleted.length || report.orphans.length) fail(`verify ${JSON.stringify(report)}`);
  console.log(`media smoke passed: ${asset.id} uploaded, served by both edges after an app replacement, delete refused while referenced, deleted, 2 audit events, store consistent`);
}

await main();
