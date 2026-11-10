"use client";

import styles from "./catalog.module.css";
import { useImageFallback } from "./use-image-fallback";

type CatalogProductImageProps = {
  image: string;
  name: string;
  thumbnail?: string;
};

export function CatalogProductImage({ image, name, thumbnail }: CatalogProductImageProps) {
  const { src, ref, onError } = useImageFallback([thumbnail, image]);

  if (!src) {
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
      onError={onError}
      ref={ref}
      src={src}
    />
  );
}
