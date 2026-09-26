"use client";

import { useState } from "react";

import styles from "./catalog.module.css";

type CatalogProductImageProps = {
  image: string;
  name: string;
};

export function CatalogProductImage({ image, name }: CatalogProductImageProps) {
  const [failed, setFailed] = useState(false);

  if (!image || failed) {
    return (
      <span
        aria-label={`Нет изображения: ${name}`}
        className={styles.imageFallback}
        role="img"
      >
        <span aria-hidden="true">Слой</span>
        <small>Нет фото</small>
      </span>
    );
  }

  return (
    // The Go API owns current image URLs; a native image keeps this boundary permissive.
    <img
      alt={name}
      className={styles.productImage}
      loading="lazy"
      onError={() => setFailed(true)}
      src={image}
    />
  );
}
