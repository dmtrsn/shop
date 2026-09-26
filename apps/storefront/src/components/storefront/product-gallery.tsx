"use client";

import { useState } from "react";

import styles from "./product-detail.module.css";

type ProductGalleryProps = {
  images: string[];
  productName: string;
};

export function ProductGallery({ images, productName }: ProductGalleryProps) {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [failedImage, setFailedImage] = useState<string>();
  const selectedImage = images[selectedIndex];

  if (!selectedImage) {
    return (
      <div className={styles.imageMissing} role="img" aria-label={`Нет изображения: ${productName}`}>
        <span>Изображение товара пока недоступно</span>
      </div>
    );
  }

  return (
    <div className={styles.gallery}>
      {failedImage === selectedImage ? (
        <div className={styles.imageMissing} role="img" aria-label={`Нет изображения: ${productName}`}>
          <span>Изображение товара пока недоступно</span>
        </div>
      ) : (
        <div className={styles.imageFrame}>
          <img
            alt={productName}
            className={styles.mainImage}
            height="720"
            onError={() => setFailedImage(selectedImage)}
            src={selectedImage}
            width="720"
          />
        </div>
      )}
      {images.length > 1 ? (
        <div aria-label="Фотографии товара" className={styles.thumbnails} role="group">
          {images.map((image, index) => (
            <button
              aria-label={`Показать фото ${index + 1} из ${images.length}`}
              aria-pressed={selectedIndex === index}
              className={styles.thumbnail}
              key={image}
              onClick={() => {
                setSelectedIndex(index);
                setFailedImage(undefined);
              }}
              type="button"
            >
              <img alt="" height="88" src={image} width="88" />
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
