"use client";

import { useState } from "react";

import styles from "./orders.module.css";

// Decorative thumbnail from the order's own snapshot image. The item name is
// always rendered as text nearby, so the picture is hidden from assistive tech.
export function OrderThumb({ image }: { image: string }) {
  const [failed, setFailed] = useState(false);
  return (
    <span aria-hidden="true" className={styles.thumb}>
      {/* The Go API owns snapshot image URLs; a native image keeps this boundary permissive. */}
      {image && !failed ? <img alt="" decoding="async" loading="lazy" onError={() => setFailed(true)} src={image} /> : null}
    </span>
  );
}
