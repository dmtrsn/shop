"use client";

import { useEffect, useState } from "react";
import { failureMessages, type ApiFailure, type References } from "@/lib/admin-api";
import { deleteMedia, listMedia, MAX_UPLOAD_BYTES, saveProductMedia, uploadMedia, type MediaAsset } from "@/lib/media-api";
import type { AdminProduct, ManagedMedia, MediaItem } from "@/lib/products-api";
import { useStaffApi } from "./use-staff-api";
import styles from "./admin-pages.module.css";

type Props = {
  product: AdminProduct;
  disabled: boolean;
  onDirty: (dirty: boolean) => void;
  onBusy: (busy: boolean) => void;
  onStale: () => void;
  onSaved: (revision: number, media: ManagedMedia) => void;
};

function referenceConflictMessage({ products, orders = 0 }: References) {
  if (products > 0 && orders > 0) return `Удаление заблокировано: файл используется в товарах (товаров — ${products}) и хранится в истории заказов (заказов — ${orders}). Отсоединение от товаров не снимет запрет: файл из истории заказов удалить нельзя.`;
  if (products > 0) return `Удаление заблокировано: файл используется в товарах (товаров — ${products}). Сначала отсоедините его от этих товаров и сохраните изменения.`;
  if (orders > 0) return `Удаление заблокировано: файл хранится в истории заказов (заказов — ${orders}), поэтому его нельзя удалить.`;
  return "Удаление заблокировано: файл всё ещё используется.";
}

export function ProductMediaEditor({ product, disabled, onDirty, onBusy, onStale, onSaved }: Props) {
  const { read, mutate } = useStaffApi();
  const [primary, setPrimary] = useState<MediaItem | null>(product.managedMedia?.primary ?? null);
  const [gallery, setGallery] = useState<MediaItem[]>(product.managedMedia?.gallery ?? []);
  const [assets, setAssets] = useState<MediaAsset[]>([]);
  const [uploaded, setUploaded] = useState<MediaAsset[]>([]);
  const [page, setPage] = useState(1);
  const [pages, setPages] = useState(1);
  const [refresh, setRefresh] = useState(0);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [libraryError, setLibraryError] = useState("");
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const dirty = (primary?.id ?? null) !== (product.managedMedia?.primary?.id ?? null)
    || gallery.map((item) => item.id).join() !== (product.managedMedia?.gallery ?? []).map((item) => item.id).join();

  useEffect(() => { onDirty(dirty); return () => onDirty(false); }, [dirty, onDirty]);
  useEffect(() => { onBusy(busy !== null); return () => onBusy(false); }, [busy, onBusy]);
  useEffect(() => {
    const controller = new AbortController();
    void read(() => listMedia(page, controller.signal)).then((result) => {
      if (controller.signal.aborted) return;
      setLoading(false);
      if (!result.ok) return setLibraryError(failureMessages[result.error.kind]);
      setAssets(result.data.items);
      setPages(result.data.totalPages);
      setLibraryError("");
    });
    return () => controller.abort();
  }, [page, refresh, read]);

  function failure(value: ApiFailure) {
    if (value.kind === "stale_revision") onStale();
    setError(value.kind === "validation" ? value.fields.map((field) => field.message).join(". ")
      : value.kind === "reference_conflict" ? referenceConflictMessage(value.references)
      : value.kind === "not_found" ? "Изображение не найдено или уже удалено. Выберите другое."
      : failureMessages[value.kind]);
  }
  async function upload(file?: File) {
    if (!file || busy || disabled) return;
    setError("");
    if (file.size > MAX_UPLOAD_BYTES) return setError(failureMessages.media_too_large);
    if (!/\.(jpe?g|png|webp)$/i.test(file.name)) return setError(failureMessages.unsupported_media_type);
    setBusy("upload");
    const result = await mutate((auth) => uploadMedia(file, auth));
    setBusy(null);
    if (!result.ok) return failure(result.error);
    setUploaded((items) => [result.data, ...items]);
  }
  async function save() {
    if (busy || disabled || !dirty) return;
    setError(""); setBusy("save");
    const result = await mutate((auth) => saveProductMedia(product.id, product.revision,
      { primaryId: primary?.id ?? null, galleryIds: gallery.map((item) => item.id) }, auth));
    setBusy(null);
    if (!result.ok) return failure(result.error);
    onSaved(result.data, { primary: primary ? { ...primary, position: 0 } : null, gallery: gallery.map((item, position) => ({ ...item, position })) });
  }
  async function removeAsset(id: string) {
    setError(""); setBusy("delete");
    const result = await mutate((auth) => deleteMedia(id, auth));
    setBusy(null); setConfirmDelete(null);
    if (!result.ok) return failure(result.error);
    setAssets((items) => items.filter((item) => item.id !== id));
    setUploaded((items) => items.filter((item) => item.id !== id));
    setRefresh((value) => value + 1);
  }
  function move(index: number, direction: number) {
    setGallery((items) => { const next = [...items]; [next[index], next[index + direction]] = [next[index + direction], next[index]]; return next; });
  }
  const candidates = [...new Map([...uploaded, ...assets].map((item) => [item.id, item])).values()];
  const preview = (item: MediaItem, label: string) => <img alt={`${label}: ${product.name}`} src={item.url} width={128} height={128} />;
  return (
    <section aria-labelledby="product-media" className={styles.mediaEditor}>
      <h2 id="product-media">Изображения</h2>
      <p className={styles.muted}>Отсоединение убирает изображение только из товара. Удаление файла — отдельное действие, доступное, если файл не используется товарами или заказами.</p>
      {disabled ? <p className={styles.muted}>Сначала сохраните изменения товара или загрузите актуальную версию при конфликте.</p> : null}
      {error ? <p role="alert" className={styles.fieldError}>{error}</p> : null}
      <fieldset disabled={disabled || busy !== null} className={styles.mediaControls}>
        <legend>Основное изображение</legend>
        {primary ? <div className={styles.mediaCard} data-testid="media-primary">
          {preview(primary, "Основное изображение")}
          <button type="button" onClick={() => setPrimary(null)}>Убрать основное из товара</button>
        </div> : <p>{product.image ? `Основное из прежнего каталога: ${product.image}` : "Основное изображение не выбрано."}</p>}
        <h3>Галерея</h3>
        <ol className={styles.mediaGrid} aria-label="Галерея товара">
          {gallery.map((item, index) => <li key={item.id} className={styles.mediaCard} data-media-id={item.id}>
            {preview(item, `Фото ${index + 1}`)}
            <div className={styles.actions}>
              <button type="button" aria-label={`Фото ${index + 1}: вверх`} disabled={index === 0} onClick={() => move(index, -1)}>↑</button>
              <button type="button" aria-label={`Фото ${index + 1}: вниз`} disabled={index === gallery.length - 1} onClick={() => move(index, 1)}>↓</button>
              <button type="button" onClick={() => { setPrimary(item); setGallery((items) => items.filter((value) => value.id !== item.id)); }}>Сделать основным</button>
              <button type="button" aria-label={`Убрать фото ${index + 1} из товара`} onClick={() => setGallery((items) => items.filter((value) => value.id !== item.id))}>Убрать из товара</button>
            </div>
          </li>)}
        </ol>
        {!primary && gallery.length === 0 && product.gallery.length > 0 ? <p className={styles.muted}>Используется прежняя галерея: {product.gallery.join(", ")}</p> : null}
        <p className={styles.muted}>Выбранные изображения заменяют прежнюю галерею целиком. Без выбранных изображений магазин использует прежние изображения товара.</p>
        <button className={styles.primaryButton} type="button" disabled={!dirty} onClick={save}>Сохранить изображения</button>
        {dirty ? <p role="status">Есть несохранённые изменения изображений.</p> : null}
        <h3>Добавить изображение</h3>
        <label htmlFor="product-media-upload">Загрузить изображение (JPEG, PNG, WebP, до 10 МБ)</label>
        <input id="product-media-upload" type="file" accept=".jpg,.jpeg,.png,.webp" onChange={(event) => { void upload(event.target.files?.[0]); event.target.value = ""; }} />
        <p className={styles.muted}>После загрузки выберите «Основное» или «В галерею», затем сохраните изображения. Загруженный файл сохраняется даже при ошибке сохранения товара.</p>
        {libraryError ? <p role="alert">{libraryError} <button type="button" onClick={() => setRefresh((value) => value + 1)}>Повторить список</button></p> : null}
        {loading ? <p role="status">Загрузка изображений…</p> : null}
        <ul className={styles.mediaGrid} aria-label="Доступные изображения">
          {candidates.map((item) => {
            const selected = primary?.id === item.id || gallery.some((value) => value.id === item.id);
            return <li key={item.id} className={styles.mediaCard} data-media-id={item.id}>
              {preview(item, "Доступное изображение")}
              <small>{item.width} × {item.height} · Товаров: {item.productCount} · Заказов: {item.orderCount}</small>
              <div className={styles.actions}>
                <button type="button" disabled={primary?.id === item.id} onClick={() => { setPrimary(item); setGallery((items) => items.filter((value) => value.id !== item.id)); }}>Основное</button>
                <button type="button" disabled={selected || gallery.length >= 20} onClick={() => setGallery((items) => [...items, item])}>В галерею</button>
                <button type="button" disabled={selected} onClick={() => setConfirmDelete(item.id)}>Удалить файл…</button>
              </div>
              {confirmDelete === item.id ? <div role="group" aria-label="Подтверждение удаления файла">
                <p>Удалить файл из хранилища? Это действие нельзя отменить.</p>
                <button type="button" onClick={() => removeAsset(item.id)}>Удалить файл навсегда</button>
                <button type="button" onClick={() => setConfirmDelete(null)}>Отмена удаления</button>
              </div> : null}
            </li>;
          })}
        </ul>
        <div className={styles.actions}>
          <button type="button" disabled={page <= 1} onClick={() => { setLoading(true); setPage(page - 1); }}>Предыдущие изображения</button>
          <span>Страница {page} из {Math.max(1, pages)}</span>
          <button type="button" disabled={page >= pages} onClick={() => { setLoading(true); setPage(page + 1); }}>Следующие изображения</button>
        </div>
      </fieldset>
      {busy ? <p role="status" aria-live="polite">{busy === "upload" ? "Загружаем изображение…" : busy === "save" ? "Сохраняем изображения…" : "Удаляем файл…"}</p> : null}
    </section>
  );
}
