"use client";

import Link from "next/link";
import { useMemo, useRef, useState } from "react";

import type { Manufacturer } from "@/lib/api/catalog";
import { productCountText } from "@/lib/plural";
import { groupManufacturers } from "@/lib/producer-navigation";

import styles from "./producer-navigation.module.css";

function productCountLabel(count: number): string {
  return `${productCountText(count)} в каталоге`;
}

export function ManufacturerDirectory({
  manufacturers,
}: {
  manufacturers: Manufacturer[];
}) {
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const normalizedQuery = query.trim().toLocaleLowerCase("ru");
  const visibleManufacturers = useMemo(
    () => manufacturers.filter((manufacturer) =>
      manufacturer.name.toLocaleLowerCase("ru").includes(normalizedQuery),
    ),
    [manufacturers, normalizedQuery],
  );
  const groups = groupManufacturers(visibleManufacturers);

  function clearSearch() {
    setQuery("");
    searchRef.current?.focus();
  }

  return (
    <div className={styles.directoryBody}>
      <div className={styles.directoryTools}>
        <div className={styles.searchField}>
          <label htmlFor="manufacturer-search">Найти производителя</label>
          <div>
            <input
              autoComplete="off"
              id="manufacturer-search"
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Например, Caffarel или Белёвские"
              ref={searchRef}
              type="search"
              value={query}
            />
            {query ? (
              <button onClick={clearSearch} type="button">
                Очистить
              </button>
            ) : null}
          </div>
        </div>

        {groups.length ? (
          <nav aria-label="Алфавитный указатель производителей" className={styles.alphabet}>
            {groups.map((group) => (
              <a href={`#${group.id}`} key={group.key}>
                {group.key === "#" ? <><span aria-hidden="true">#</span><span className="visually-hidden">Прочие</span></> : group.key}
              </a>
            ))}
          </nav>
        ) : null}
      </div>

      <p aria-live="polite" className="visually-hidden">
        {visibleManufacturers.length
          ? `Найдено производителей: ${visibleManufacturers.length}`
          : "Производители не найдены"}
      </p>

      {groups.length ? (
        <div className={styles.groups}>
          {groups.map((group) => (
            <section aria-labelledby={`${group.id}-heading`} className={styles.group} id={group.id} key={group.key}>
              <h2 id={`${group.id}-heading`}>{group.key === "#" ? "Прочие" : group.key}</h2>
              <ul>
                {group.manufacturers.map((manufacturer) => (
                  <li key={manufacturer.id}>
                    <Link href={`/manufacturers/${encodeURIComponent(manufacturer.slug)}`}>
                      <span className={styles.manufacturerName}>{manufacturer.name}</span>
                      <span className={styles.manufacturerMeta}>
                        {manufacturer.country ? `${manufacturer.country} · ` : ""}
                        {productCountLabel(manufacturer.productCount)}
                      </span>
                      <span aria-hidden="true" className={styles.arrow}>→</span>
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      ) : (
        <div className={styles.filterEmpty} role="status">
          <h2>Производители не найдены</h2>
          <p>Проверьте написание или очистите локальный фильтр.</p>
          <button onClick={clearSearch} type="button">Очистить поиск</button>
        </div>
      )}
    </div>
  );
}
