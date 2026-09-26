"use client";

import { usePathname, useRouter } from "next/navigation";
import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  useSyncExternalStore,
  type FocusEvent,
  type FormEvent,
  type KeyboardEvent,
} from "react";

import { SearchInput } from "@/components/ui/search-input";
import {
  codePointLength,
  fetchSuggestions,
  SEARCH_QUERY_MAX_LENGTH,
  SUGGESTION_MIN_QUERY_LENGTH,
  searchResultsUrl,
  suggestionHref,
  type SearchSuggestion,
  type SuggestionType,
} from "@/lib/search-suggestions";

import styles from "./global-search.module.css";

export const SUGGESTION_DEBOUNCE_MS = 160;

type Status = "idle" | "loading" | "ready" | "error";

type Option =
  | { kind: "suggestion"; suggestion: SearchSuggestion; href: string }
  | { kind: "search"; href: string };

// Presentation order from the UX spec; order inside a group is the server's.
const GROUPS: Array<{ type: SuggestionType; label: string; rowLabel?: string }> = [
  { type: "product", label: "Товары" },
  { type: "category", label: "Категории", rowLabel: "Категория" },
  { type: "manufacturer", label: "Производители", rowLabel: "Производитель" },
  { type: "brand", label: "Бренды", rowLabel: "Бренд" },
];

function pluralSuggestions(count: number): string {
  const mod100 = count % 100;
  const mod10 = count % 10;
  if (mod100 >= 11 && mod100 <= 14) return "подсказок";
  if (mod10 === 1) return "подсказка";
  if (mod10 >= 2 && mod10 <= 4) return "подсказки";
  return "подсказок";
}

function isVisible(element: HTMLElement): boolean {
  return typeof element.checkVisibility === "function"
    ? element.checkVisibility()
    : element.offsetParent !== null;
}

const subscribeToNothing = () => () => {};
const shortcutLabel = () =>
  /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent) ? "⌘K" : "Ctrl K";

/** Bolds the first exact, case-sensitive occurrence; otherwise renders the title unchanged. */
function Highlighted({ query, text }: { query: string; text: string }) {
  const index = query ? text.indexOf(query) : -1;
  if (index < 0) return <>{text}</>;
  const end = index + query.length;
  return (
    <>
      {text.slice(0, index)}
      <strong>{text.slice(index, end)}</strong>
      {text.slice(end)}
    </>
  );
}

export function GlobalSearch({ className }: { className?: string }) {
  const router = useRouter();
  const pathname = usePathname();
  const baseId = useId();
  const inputId = `${baseId}-input`;
  const listboxId = `${baseId}-listbox`;
  const statusId = `${baseId}-status`;
  const optionId = (index: number) => `${baseId}-option-${index}`;

  const [value, setValue] = useState("");
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<Status>("idle");
  const [items, setItems] = useState<SearchSuggestion[]>([]);
  const [resultQuery, setResultQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(-1);

  const inputRef = useRef<HTMLInputElement>(null);
  const timerRef = useRef<number | undefined>(undefined);
  const abortRef = useRef<AbortController | null>(null);
  const sequenceRef = useRef(0);
  const lastQueryRef = useRef("");

  const shortcut = useSyncExternalStore(subscribeToNothing, shortcutLabel, () => null);

  const cancelPending = useCallback(() => {
    window.clearTimeout(timerRef.current);
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
      sequenceRef.current += 1;
      lastQueryRef.current = "";
      setStatus((current) => (current === "loading" ? "idle" : current));
    }
  }, []);

  const close = useCallback(() => {
    cancelPending();
    setOpen(false);
    setActiveIndex(-1);
  }, [cancelPending]);

  useEffect(() => close, [close]);

  useEffect(() => {
    close();
  }, [close, pathname]);

  useEffect(() => {
    function focusShortcut(event: globalThis.KeyboardEvent) {
      if (event.defaultPrevented || event.altKey || event.shiftKey) return;
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "k") return;
      const input = inputRef.current;
      // Desktop and mobile instances coexist; only the rendered one takes the shortcut.
      if (!input || !isVisible(input)) return;
      event.preventDefault();
      input.focus();
      input.select();
    }
    document.addEventListener("keydown", focusShortcut);
    return () => document.removeEventListener("keydown", focusShortcut);
  }, []);

  useEffect(() => {
    if (activeIndex < 0) return;
    document.getElementById(`${baseId}-option-${activeIndex}`)?.scrollIntoView?.({ block: "nearest" });
  }, [activeIndex, baseId]);

  async function load(query: string) {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    const sequence = ++sequenceRef.current;
    lastQueryRef.current = query;
    setStatus("loading");
    setOpen(true);
    try {
      const suggestions = await fetchSuggestions(query, { signal: controller.signal });
      if (sequence !== sequenceRef.current) return;
      setItems(suggestions);
      setStatus("ready");
    } catch {
      if (sequence !== sequenceRef.current || controller.signal.aborted) return;
      lastQueryRef.current = "";
      setItems([]);
      setStatus("error");
    } finally {
      if (abortRef.current === controller) abortRef.current = null;
    }
    setResultQuery(query);
    setActiveIndex(-1);
  }

  function schedule(nextValue: string) {
    window.clearTimeout(timerRef.current);
    const query = nextValue.trim();
    const length = codePointLength(query);
    if (length < SUGGESTION_MIN_QUERY_LENGTH || length > SEARCH_QUERY_MAX_LENGTH) {
      cancelPending();
      lastQueryRef.current = "";
      setStatus("idle");
      setItems([]);
      setResultQuery("");
      setActiveIndex(-1);
      return;
    }
    if (query === lastQueryRef.current) return;
    timerRef.current = window.setTimeout(() => void load(query), SUGGESTION_DEBOUNCE_MS);
  }

  const trimmed = value.trim();
  const hasPanel = status === "loading" || (status !== "idle" && resultQuery === trimmed);
  const expanded = open && hasPanel;

  const groups = GROUPS.map((group) => ({
    ...group,
    items: items.filter((item) => item.type === group.type),
  })).filter((group) => group.items.length > 0);
  const options: Option[] = [
    ...groups.flatMap((group) =>
      group.items.map((suggestion) => ({
        kind: "suggestion" as const,
        suggestion,
        href: suggestionHref(suggestion),
      })),
    ),
    { kind: "search", href: searchResultsUrl(value) },
  ];

  function select(option: Option) {
    close();
    router.push(option.href);
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    close();
    router.push(searchResultsUrl(value));
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing) return;
    switch (event.key) {
      case "ArrowDown":
      case "ArrowUp": {
        if (!hasPanel) return;
        event.preventDefault();
        const down = event.key === "ArrowDown";
        if (!expanded) {
          setOpen(true);
          if (!event.altKey) setActiveIndex(down ? 0 : options.length - 1);
          return;
        }
        if (event.altKey) return;
        setActiveIndex((index) => {
          if (down) return (index + 1) % options.length;
          return index <= 0 ? options.length - 1 : index - 1;
        });
        return;
      }
      case "Enter":
        if (expanded && activeIndex >= 0 && options[activeIndex]) {
          event.preventDefault();
          select(options[activeIndex]);
        }
        return;
      case "Escape":
        if (expanded) {
          event.preventDefault();
          close();
        } else if (value) {
          event.preventDefault();
          setValue("");
          schedule("");
        }
        return;
      case "Tab":
        close();
        return;
    }
  }

  function onBlur(event: FocusEvent<HTMLFormElement>) {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) close();
  }

  let announcement = "";
  if (expanded && status === "ready") {
    announcement = items.length
      ? `${items.length} ${pluralSuggestions(items.length)}`
      : "Нет подсказок";
  } else if (expanded && status === "error") {
    announcement = "Подсказки временно недоступны";
  }

  const searchOptionIndex = options.length - 1;
  const optionProps = (index: number) => ({
    "aria-selected": index === activeIndex,
    className: `${styles.option} ${index === activeIndex ? styles.active : ""}`,
    id: optionId(index),
    onClick: () => select(options[index]),
    // Keep focus in the combobox so pointer and keyboard selection share one path.
    onMouseDown: (event: { preventDefault(): void }) => event.preventDefault(),
    role: "option" as const,
  });

  let optionIndex = 0;

  return (
    <form
      action="/catalog"
      className={`${styles.search} ${className ?? ""}`}
      method="get"
      onBlur={onBlur}
      onSubmit={onSubmit}
      role="search"
    >
      <SearchInput
        ref={inputRef}
        aria-activedescendant={expanded && activeIndex >= 0 ? optionId(activeIndex) : undefined}
        aria-autocomplete="list"
        aria-controls={listboxId}
        aria-expanded={expanded}
        aria-keyshortcuts="Control+K Meta+K"
        autoComplete="off"
        className={styles.input}
        enterKeyHint="search"
        id={inputId}
        maxLength={SEARCH_QUERY_MAX_LENGTH}
        name="q"
        onChange={(event) => {
          setValue(event.target.value);
          setActiveIndex(-1);
          setOpen(true);
          schedule(event.target.value);
        }}
        onFocus={() => {
          if (hasPanel) setOpen(true);
        }}
        onKeyDown={onKeyDown}
        placeholder="Найти товар, артикул или производителя"
        role="combobox"
        value={value}
      />
      {shortcut && !value ? (
        <kbd aria-hidden="true" className={styles.shortcut}>
          {shortcut}
        </kbd>
      ) : null}

      <div className={styles.panel} hidden={!expanded}>
        <div aria-label="Подсказки поиска" className={styles.listbox} id={listboxId} role="listbox">
          {groups.map((group) => (
            <ul
              aria-labelledby={`${baseId}-group-${group.type}`}
              className={styles.group}
              key={group.type}
              role="group"
            >
              <li className={styles.groupLabel} id={`${baseId}-group-${group.type}`} role="presentation">
                {group.label}
              </li>
              {group.items.map((suggestion) => {
                const index = optionIndex++;
                return (
                  <li key={`${suggestion.type}-${suggestion.id}`} {...optionProps(index)}>
                    {suggestion.type === "product" ? (
                      suggestion.image ? (
                        // Suggestion images come from the Go asset host; see CatalogProductImage.
                        <img alt="" className={styles.thumbnail} loading="lazy" src={suggestion.image} />
                      ) : (
                        <span aria-hidden="true" className={styles.thumbnail} />
                      )
                    ) : null}
                    <span className={styles.optionText}>
                      {group.rowLabel ? <span className={styles.rowLabel}>{group.rowLabel}</span> : null}
                      <span className={styles.title}>
                        <Highlighted query={resultQuery} text={suggestion.title} />
                      </span>
                      {suggestion.subtitle ? (
                        <span className={styles.subtitle}>{suggestion.subtitle}</span>
                      ) : null}
                    </span>
                  </li>
                );
              })}
            </ul>
          ))}
          <div {...optionProps(searchOptionIndex)}>
            <span className={styles.optionText}>
              <span className={styles.title}>Искать «{trimmed}» в каталоге</span>
            </span>
          </div>
        </div>
        {status === "loading" ? (
          <p className={`${styles.message} ${styles.loading}`}>Ищем подсказки…</p>
        ) : null}
        {status === "ready" && items.length === 0 ? (
          <p className={styles.message}>Нет подсказок</p>
        ) : null}
        {status === "error" ? (
          <p className={styles.message}>Подсказки временно недоступны</p>
        ) : null}
      </div>

      <p className="visually-hidden" id={statusId} role="status">
        {announcement}
      </p>
    </form>
  );
}
