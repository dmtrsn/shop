"use client";

import { useEffect, useState } from "react";

import styles from "./catalog.module.css";

type ProducerDependencyGuardProps = {
  brandSelectId: string;
  manufacturerSelectId: string;
  seriesSelectId: string;
};

type ChangedLevel = "brand" | "manufacturer" | undefined;

export function ProducerDependencyGuard({
  brandSelectId,
  manufacturerSelectId,
  seriesSelectId,
}: ProducerDependencyGuardProps) {
  const [changedLevel, setChangedLevel] = useState<ChangedLevel>();

  useEffect(() => {
    const manufacturer = document.getElementById(manufacturerSelectId);
    const brand = document.getElementById(brandSelectId);
    const series = document.getElementById(seriesSelectId);
    if (
      !(manufacturer instanceof HTMLSelectElement) ||
      !(brand instanceof HTMLSelectElement) ||
      !(series instanceof HTMLSelectElement)
    ) {
      return;
    }
    const manufacturerSelect = manufacturer;
    const brandSelect = brand;
    const seriesSelect = series;

    function clearSeries() {
      seriesSelect.value = "";
      seriesSelect.disabled = true;
      setChangedLevel("brand");
    }

    function clearProducerDescendants() {
      brandSelect.value = "";
      brandSelect.disabled = true;
      seriesSelect.value = "";
      seriesSelect.disabled = true;
      setChangedLevel("manufacturer");
    }

    manufacturerSelect.addEventListener("change", clearProducerDescendants);
    brandSelect.addEventListener("change", clearSeries);
    return () => {
      manufacturerSelect.removeEventListener("change", clearProducerDescendants);
      brandSelect.removeEventListener("change", clearSeries);
    };
  }, [brandSelectId, manufacturerSelectId, seriesSelectId]);

  return (
    <p aria-live="polite" className={styles.producerChangeMessage}>
      {changedLevel === "manufacturer"
        ? "Примените производителя, чтобы получить подходящие бренды и серии."
        : changedLevel === "brand"
          ? "Примените бренд, чтобы получить подходящие серии."
          : null}
    </p>
  );
}
