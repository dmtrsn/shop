"use client";

import { useEffect, useRef, useState } from "react";

import styles from "./catalog.module.css";

type CatalogProductImageProps = {
  image: string;
  name: string;
};

export function CatalogProductImage({ image, name }: CatalogProductImageProps) {
  const [failed, setFailed] = useState(false);
  const imageRef = useRef<HTMLImageElement>(null);

  // A server-rendered image can fail before hydration attaches onError.
  useEffect(() => {
    const element = imageRef.current;
    if (element?.complete && element.naturalWidth === 0) setFailed(true);
  }, [image]);

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
      ref={imageRef}
      src={image}
    />
  );
}
