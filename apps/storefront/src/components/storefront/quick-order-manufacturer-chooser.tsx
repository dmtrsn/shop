"use client";

import { useId, useRef, useState } from "react";

import { Button, buttonClassName } from "@/components/ui/button";
import { SearchInput } from "@/components/ui/search-input";
import type { Manufacturer } from "@/lib/api/catalog";
import { matchesManufacturer } from "@/lib/quick-order";

import styles from "./quick-order.module.css";

function Directory({ manufacturers, selected, onSelect, autoFocus = false }: {
  manufacturers: Manufacturer[]; selected?: string; onSelect: (manufacturer: Manufacturer) => void; autoFocus?: boolean;
}) {
  const [query, setQuery] = useState("");
  const visible = manufacturers.filter((manufacturer) => matchesManufacturer(manufacturer, query));
  return <>
    <SearchInput autoFocus={autoFocus} label="Поиск производителя" placeholder="Поиск производителя" value={query} onChange={(event) => setQuery(event.target.value)} />
    <ul className={styles.manufacturers}>
      {visible.map((manufacturer) => <li key={manufacturer.id}>
        <button aria-current={selected === manufacturer.slug ? "true" : undefined} onClick={() => onSelect(manufacturer)} type="button">{manufacturer.name}</button>
      </li>)}
    </ul>
    {!visible.length ? <p role="status">Производители не найдены</p> : null}
  </>;
}

export function QuickOrderManufacturerChooser(props: {
  manufacturers: Manufacturer[]; selected?: string; onSelect: (manufacturer: Manufacturer) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const titleID = useId();
  const [open, setOpen] = useState(false);
  function close() { dialog.current?.close(); }
  return <>
    <aside aria-label="Производители" className={styles.sidebar}>
      <h2>Производители</h2><Directory {...props} />
    </aside>
    <div className={styles.mobileChooser}>
      <button className={buttonClassName("secondary")} ref={trigger} aria-haspopup="dialog" onClick={() => { setOpen(true); dialog.current?.showModal(); }} type="button">Выбрать производителя</button>
      <dialog aria-labelledby={titleID} className={styles.dialog} onClose={() => { setOpen(false); trigger.current?.focus(); }} ref={dialog}>
        <h2 id={titleID}>Производители</h2>
        {open ? <Directory {...props} autoFocus onSelect={(manufacturer) => { props.onSelect(manufacturer); close(); }} /> : null}
        <Button onClick={close} variant="secondary">Закрыть</Button>
      </dialog>
    </div>
  </>;
}
