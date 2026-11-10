// Storefront image expectations shared by the real-stack journeys (ADR 022).
// Small surfaces (cards, cart rows, gallery controls) draw a managed image's
// thumbnail when it has one and its original otherwise; the product page's main
// image is always the original. Items are managed media IDs or legacy URLs.

const GALLERY = '[aria-label="Фотографии товара"]';
const MAIN = `[...document.querySelectorAll('main img')].find((image) => !image.closest(${JSON.stringify(GALLERY)}))`;
const CONTROLS = `[...document.querySelectorAll(${JSON.stringify(`${GALLERY} img`)})]`;

// The thumbnail row is the oracle, read independently of the storefront.
export function mediaUrls(sql, item) {
  if (item.startsWith("/")) return { original: item, small: item };
  if (!/^med_[0-9a-f]{32}$/.test(item)) throw new Error(`not a media ID: ${item}`);
  const original = `/media/${item}`;
  const sha256 = sql(`SELECT sha256 FROM media_thumbnails WHERE media_id='${item}'`);
  return { original, small: sha256 ? `${original}/thumbnail/${sha256}` : original };
}

export const smallImage = (sql, item) => mediaUrls(sql, item).small;

/**
 * On an open product page: the main image is the primary's original, the
 * controls show every item's small image in order, and selecting each control
 * shows that item's original as the main image, never its thumbnail.
 */
export async function assertProductGallery(page, sql, primary, gallery) {
  const items = [primary, ...gallery].map((item) => mediaUrls(sql, item));
  const main = (src) => `(() => { const image = ${MAIN}; return !!image && image.getAttribute('src') === ${JSON.stringify(src)} && image.complete && image.naturalWidth > 0; })()`;
  await page.waitFor("the original primary image", main(items[0].original));
  // Clicks before hydration are lost.
  await page.waitFor("the hydrated gallery", `Object.keys(${MAIN}).some((key) => key.startsWith('__reactProps'))`);

  // A single image renders no control strip.
  const expected = gallery.length ? items.map((item) => item.small) : [];
  const controls = `${CONTROLS}.map((image) => image.getAttribute('src'))`;
  await page.waitFor("gallery controls", `JSON.stringify(${controls}) === ${JSON.stringify(JSON.stringify(expected))}`, 5000)
    .catch(async () => { throw new Error(`gallery order ${JSON.stringify(await page.evaluate(controls))}, expected ${JSON.stringify(expected)}`); });
  await page.waitFor("loaded gallery controls", `${CONTROLS}.every((image) => image.complete && image.naturalWidth > 0)`);

  // Visit every control, ending back on the primary.
  const order = items.map((_, index) => index).slice(1);
  if (gallery.length) order.push(0);
  for (const index of order) {
    const label = `Показать фото ${index + 1} из ${items.length}`;
    await page.click(`${GALLERY} button[aria-label="${label}"]`);
    await page.waitFor(`the original of photo ${index + 1}`, `${main(items[index].original)} && document.querySelector(${JSON.stringify(`${GALLERY} button[aria-label="${label}"]`)}).getAttribute('aria-pressed') === 'true'`);
  }
}
