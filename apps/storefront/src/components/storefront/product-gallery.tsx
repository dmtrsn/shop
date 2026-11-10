"use client";

import { useState } from "react";

import styles from "./product-detail.module.css";
import { useImageFallback } from "./use-image-fallback";

/** `src` is the original; the thumbnail only ever draws a gallery control. */
export type GalleryImage = { src: string; thumbnail?: string };

type ProductGalleryProps = {
  images: GalleryImage[];
  productName: string;
};

function GalleryThumb({ image }: { image: GalleryImage }) {
  const { src, ref, onError } = useImageFallback([image.thumbnail, image.src]);
  // With no source left the button's own neutral surface is the placeholder.
  return src ? <img alt="" height="88" loading="lazy" onError={onError} ref={ref} src={src} width="88" /> : null;
}

function ProductMainImage({ image, productName }: { image: string; productName: string }) {
  const { src, ref, onError } = useImageFallback([image]);

  if (!src) {
    return (
      <div className={styles.imageMissing} role="img" aria-label={`Нет изображения: ${productName}`}>
        <span>Изображение товара пока недоступно</span>
      </div>
    );
  }

  return (
    <div className={styles.imageFrame}>
      {/* LCP element: always the original, high priority, never lazy. */}
      <img
        alt={productName}
        className={styles.mainImage}
        fetchPriority="high"
        height="720"
        onError={onError}
        ref={ref}
        src={src}
        width="720"
      />
    </div>
  );
}

export function ProductGallery({ images, productName }: ProductGalleryProps) {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const selectedImage = images[selectedIndex]?.src;

  if (!selectedImage) {
    return (
      <div className={styles.imageMissing} role="img" aria-label={`Нет изображения: ${productName}`}>
        <span>Изображение товара пока недоступно</span>
      </div>
    );
  }

  return (
    <div className={styles.gallery}>
      {/* Each original gets fresh fallback state when selection changes. */}
      <ProductMainImage image={selectedImage} key={selectedImage} productName={productName} />
      {images.length > 1 ? (
        <div aria-label="Фотографии товара" className={styles.thumbnails} role="group">
          {images.map((image, index) => (
            <button
              aria-label={`Показать фото ${index + 1} из ${images.length}`}
              aria-pressed={selectedIndex === index}
              className={styles.thumbnail}
              key={image.src}
              onClick={() => setSelectedIndex(index)}
              type="button"
            >
              <GalleryThumb image={image} />
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
