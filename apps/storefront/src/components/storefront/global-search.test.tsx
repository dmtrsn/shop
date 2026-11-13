import { act, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { SearchSuggestion } from "@/lib/search-suggestions";

const navigation = vi.hoisted(() => ({ push: vi.fn(), pathname: "/" }));

vi.mock("next/navigation", () => ({
  usePathname: () => navigation.pathname,
  useRouter: () => ({ push: navigation.push }),
}));

import { GlobalSearch, SUGGESTION_DEBOUNCE_MS } from "./global-search";

type Pending = { query: string; signal?: AbortSignal; resolve: (response: Response) => void };

const product: SearchSuggestion = {
  type: "product",
  id: "p02",
  slug: "milk-chocolate-hazelnut",
  title: "Молочный шоколад с цельным фундуком",
  subtitle: "Caffarel · плитка 100 г",
  image: "/assets/product-chocolate.webp",
};
const category: SearchSuggestion = { type: "category", id: "cat-bars", slug: "chocolate-bars", title: "Плиточный шоколад" };
const manufacturer: SearchSuggestion = { type: "manufacturer", id: "m-choco", slug: "choco-atelier", title: "Choco Atelier", subtitle: "Россия" };
const brand: SearchSuggestion = { type: "brand", id: "b-noir", slug: "atelier-noir", title: "Atelier Noir", subtitle: "Choco Atelier" };

let pending: Pending[];

function ok(items: unknown[]): Response {
  return { ok: true, status: 200, json: async () => ({ items }) } as Response;
}

function failed(status: number): Response {
  return { ok: false, status, json: async () => ({}) } as Response;
}

async function settle(response: Response, request = pending.at(-1)) {
  await act(async () => {
    request?.resolve(response);
  });
}

async function debounce() {
  await act(() => new Promise((resolve) => setTimeout(resolve, SUGGESTION_DEBOUNCE_MS + 30)));
}

function setup(ui = <GlobalSearch />) {
  const user = userEvent.setup();
  return { user, ...render(ui) };
}

beforeEach(() => {
  pending = [];
  navigation.push.mockReset();
  navigation.pathname = "/";
  vi.stubGlobal(
    "fetch",
    vi.fn((input: string, init?: RequestInit) =>
      new Promise<Response>((resolve) => {
        const query = new URL(input, "http://storefront.test").searchParams.get("q") ?? "";
        pending.push({ query, signal: init?.signal ?? undefined, resolve });
      }),
    ),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(HTMLElement.prototype, "checkVisibility");
});

describe("GlobalSearch", () => {
  it("implements a combobox with grouped, canonically linked suggestions", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox", { name: "Поиск по магазину" });

    expect(input).toHaveAttribute("aria-expanded", "false");
    expect(input).toHaveAttribute("aria-autocomplete", "list");
    expect(input.closest("form")).toHaveAttribute("action", "/catalog");

    await user.type(input, "шо");
    await debounce();
    await settle(ok([product, brand, category, manufacturer, { type: "collection", id: "c", slug: "c", title: "Зима" }]));

    const listbox = screen.getByRole("listbox", { name: "Подсказки поиска" });
    expect(input).toHaveAttribute("aria-expanded", "true");
    expect(input).toHaveAttribute("aria-controls", listbox.id);
    expect(within(listbox).getAllByRole("group").map((group) => group.getAttribute("aria-labelledby")))
      .toHaveLength(4);
    expect(within(listbox).getByRole("group", { name: "Товары" })).toBeInTheDocument();
    expect(within(listbox).getAllByRole("option").map((option) => option.textContent)).toEqual([
      product.title + product.subtitle,
      "Категория" + category.title,
      "Производитель" + manufacturer.title + manufacturer.subtitle,
      "Бренд" + brand.title + brand.subtitle,
      "Искать «шо» в каталоге",
    ]);
    expect(screen.getByRole("status")).toHaveTextContent("4 подсказки");
  });

  it("draws product suggestions from the thumbnail, then the original, then a neutral square", async () => {
    const image = "/media/med_" + "a".repeat(32);
    const thumbnail = `${image}/thumbnail/${"f".repeat(64)}`;
    const { user } = setup();
    await user.type(screen.getByRole("combobox", { name: "Поиск по магазину" }), "шо");
    await debounce();
    await settle(ok([{ ...product, image, thumbnail }, { ...product, id: "p03" }, category]));

    const [managed, legacy] = within(screen.getByRole("group", { name: "Товары" })).getAllByRole("option");
    expect(managed.querySelector("img")).toHaveAttribute("src", thumbnail);
    expect(legacy.querySelector("img")).toHaveAttribute("src", product.image);
    fireEvent.error(managed.querySelector("img")!);
    expect(managed.querySelector("img")).toHaveAttribute("src", image);
    fireEvent.error(managed.querySelector("img")!);
    expect(managed.querySelector("img")).toBeNull();
    expect(managed.querySelector("span[aria-hidden=true]")).toBeInTheDocument();
    expect(pending).toHaveLength(1);
  });

  it("debounces requests and starts only at two code points", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "ш");
    await debounce();
    expect(fetch).not.toHaveBeenCalled();

    await user.type(input, "о");
    expect(fetch).not.toHaveBeenCalled();
    await debounce();
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(pending[0].query).toBe("шо");

    await user.type(input, " ");
    await debounce();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("aborts obsolete requests and ignores late responses", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "шо");
    await debounce();
    const first = pending[0];
    await user.type(input, "к");
    await debounce();
    const second = pending[1];

    expect(first.signal?.aborted).toBe(true);
    expect(second.signal?.aborted).toBe(false);

    await settle(ok([brand]), second);
    await settle(ok([product]), first);

    const listbox = screen.getByRole("listbox");
    expect(within(listbox).queryByText(product.title)).not.toBeInTheDocument();
    expect(within(listbox).getByText(brand.title)).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("1 подсказка");
  });

  it("removes old options immediately and resets the active selection when the query changes", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");
    await user.type(input, "cocoa");
    await debounce();
    await settle(ok([product]));
    await user.keyboard("{ArrowDown}");
    expect(input).toHaveAttribute("aria-activedescendant", screen.getAllByRole("option")[0].id);

    fireEvent.change(input, { target: { value: "vanilla" } });

    expect(input).not.toHaveAttribute("aria-activedescendant");
    expect(screen.queryByRole("option", { name: /Молочный шоколад/, hidden: true })).toBeNull();
    expect(pending).toHaveLength(1);
    await debounce();
    expect(pending.at(-1)?.query).toBe("vanilla");
    expect(screen.getAllByRole("option")).toHaveLength(1);
  });

  it("cannot activate an old product with the keyboard while the new query is pending", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");
    await user.type(input, "cocoa");
    await debounce();
    await settle(ok([product]));
    fireEvent.change(input, { target: { value: "vanilla" } });
    await debounce();

    await user.keyboard("{ArrowDown}{Enter}");

    expect(navigation.push).toHaveBeenCalledExactlyOnceWith("/catalog?q=vanilla");
  });

  it("offers only the current full-search action to the pointer while the new query is pending", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");
    await user.type(input, "cocoa");
    await debounce();
    await settle(ok([product]));
    fireEvent.change(input, { target: { value: "vanilla" } });
    await debounce();

    expect(screen.queryByRole("option", { name: /Молочный шоколад/ })).toBeNull();
    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(1);
    await user.click(options[0]);
    expect(navigation.push).toHaveBeenCalledExactlyOnceWith("/catalog?q=vanilla");
  });

  it("makes the new response selectable after hiding old results during loading", async () => {
    const vanilla = { ...product, id: "vanilla", slug: "vanilla", title: "Vanilla" };
    const { user } = setup();
    const input = screen.getByRole("combobox");
    await user.type(input, "cocoa");
    await debounce();
    await settle(ok([product]));
    fireEvent.change(input, { target: { value: "vanilla" } });
    await debounce();
    expect(screen.getAllByRole("option")).toHaveLength(1);

    await settle(ok([vanilla]));

    expect(screen.getAllByRole("option")).toHaveLength(2);
    expect(screen.queryByRole("option", { name: /Молочный шоколад/ })).toBeNull();
    await user.keyboard("{ArrowDown}{Enter}");
    expect(navigation.push).toHaveBeenCalledExactlyOnceWith("/products/vanilla");
  });

  it("does not restore old options when the new query fails", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");
    await user.type(input, "cocoa");
    await debounce();
    await settle(ok([product]));
    fireEvent.change(input, { target: { value: "vanilla" } });
    await debounce();
    await settle(failed(502));

    expect(screen.getByRole("status")).toHaveTextContent("Подсказки временно недоступны");
    expect(screen.getAllByRole("option")).toHaveLength(1);
    await user.keyboard("{ArrowDown}{Enter}");
    expect(navigation.push).toHaveBeenCalledExactlyOnceWith("/catalog?q=vanilla");
  });

  it("keeps a response arriving during the next debounce out of the new query's options", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");
    await user.type(input, "cocoa");
    await debounce();
    const cocoaRequest = pending[0];
    fireEvent.change(input, { target: { value: "vanilla" } });
    await settle(ok([product]), cocoaRequest);

    expect(screen.queryByRole("option", { name: /Молочный шоколад/, hidden: true })).toBeNull();
    await debounce();
    expect(screen.getAllByRole("option")).toHaveLength(1);
    await settle(ok([brand]));
    expect(screen.getByRole("option", { name: /Atelier Noir/ })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /Молочный шоколад/ })).toBeNull();
  });

  it("keeps the existing request behavior when returning to a previously loaded query", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");
    await user.type(input, "cocoa");
    await debounce();
    await settle(ok([product]));
    fireEvent.change(input, { target: { value: "vanilla" } });
    await debounce();
    const vanillaRequest = pending[1];

    fireEvent.change(input, { target: { value: "cocoa" } });
    await debounce();

    expect(pending.map((request) => request.query)).toEqual(["cocoa", "vanilla", "cocoa"]);
    expect(vanillaRequest.signal?.aborted).toBe(true);
    await settle(ok([brand]), vanillaRequest);
    expect(screen.queryByRole("option", { name: /Atelier Noir/ })).toBeNull();
    await settle(ok([category]));
    expect(screen.getByRole("option", { name: /Плиточный шоколад/ })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /Молочный шоколад/ })).toBeNull();
  });

  it("moves the active option with arrows and opens it with Enter", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "шоколад");
    await debounce();
    await settle(ok([product, category]));
    const options = screen.getAllByRole("option");

    await user.keyboard("{ArrowDown}");
    expect(input).toHaveAttribute("aria-activedescendant", options[0].id);
    expect(options[0]).toHaveAttribute("aria-selected", "true");
    await user.keyboard("{ArrowUp}");
    expect(input).toHaveAttribute("aria-activedescendant", options[2].id);
    await user.keyboard("{ArrowDown}{ArrowDown}");
    expect(input).toHaveAttribute("aria-activedescendant", options[1].id);

    await user.keyboard("{Enter}");
    expect(navigation.push).toHaveBeenCalledWith("/categories/chocolate-bars");
    expect(input).toHaveAttribute("aria-expanded", "false");
    expect(input).not.toHaveAttribute("aria-activedescendant");
  });

  it("selects with the pointer through the same path", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "шоколад");
    await debounce();
    await settle(ok([product]));
    await user.click(screen.getByRole("option", { name: new RegExp(product.subtitle ?? "") }));

    expect(navigation.push).toHaveBeenCalledWith("/products/milk-chocolate-hazelnut");
    expect(input).toHaveFocus();
  });

  it("submits the typed query to canonical catalog results without an active option", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, " шоколад {Enter}");

    expect(navigation.push).toHaveBeenCalledWith(`/catalog?${new URLSearchParams({ q: "шоколад" })}`);
  });

  it("ignores Enter while an input method is composing", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "шоколад");
    await debounce();
    await settle(ok([product]));
    await user.keyboard("{ArrowDown}");
    fireEvent.keyDown(input, { key: "Enter", isComposing: true });

    expect(navigation.push).not.toHaveBeenCalled();
  });

  it("closes with Escape, then clears on a second Escape", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "шоколад");
    await debounce();
    await settle(ok([product]));
    await user.keyboard("{Escape}");
    expect(input).toHaveAttribute("aria-expanded", "false");
    expect(input).toHaveValue("шоколад");

    await user.keyboard("{Escape}");
    expect(input).toHaveValue("");
  });

  it("keeps the full-search action in empty and error states", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "zzz");
    await debounce();
    await settle(ok([]));
    expect(screen.getByText("Нет подсказок", { selector: "p:not([role])" })).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Нет подсказок");
    expect(screen.getByRole("option", { name: "Искать «zzz» в каталоге" })).toBeInTheDocument();

    await user.type(input, "z");
    await debounce();
    await settle(failed(502));
    expect(screen.getByRole("status")).toHaveTextContent("Подсказки временно недоступны");
    expect(screen.getByRole("option", { name: "Искать «zzzz» в каталоге" })).toBeInTheDocument();
  });

  it("shows a loading row while keeping the typed query", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "шо");
    await debounce();

    expect(screen.getByText("Ищем подсказки…")).toBeInTheDocument();
    expect(input).toHaveValue("шо");
  });

  it("bolds only an exact literal match and preserves the title text", async () => {
    const { user } = setup();
    const input = screen.getByRole("combobox");

    await user.type(input, "шоколад");
    await debounce();
    await settle(ok([product, category]));

    const [productOption, categoryOption] = screen.getAllByRole("option");
    expect(productOption.querySelector("strong")).toHaveTextContent("шоколад");
    expect(within(productOption).getByText(/Молочный/).textContent).toBe(product.title);
    expect(categoryOption.querySelector("strong")).toHaveTextContent("шоколад");

    await user.clear(input);
    await user.type(input, "Шоколад");
    await debounce();
    await settle(ok([product]));
    expect(screen.getAllByRole("option")[0].querySelector("strong")).toBeNull();
  });

  it("focuses the visible instance with Ctrl/Cmd+K", () => {
    render(
      <>
        <GlobalSearch />
        <GlobalSearch />
      </>,
    );
    const [hidden, visible] = screen.getAllByRole("combobox");
    Object.defineProperty(HTMLElement.prototype, "checkVisibility", {
      configurable: true,
      value(this: HTMLElement) {
        return this === visible;
      },
    });

    fireEvent.keyDown(document, { key: "k", ctrlKey: true });
    expect(visible).toHaveFocus();

    visible.blur();
    fireEvent.keyDown(document, { key: "K", metaKey: true });
    expect(visible).toHaveFocus();
    expect(hidden).not.toHaveFocus();
  });

  it("gives simultaneous instances distinct, self-contained ARIA relationships", async () => {
    const { container, user } = setup(
      <>
        <GlobalSearch />
        <GlobalSearch />
      </>,
    );
    const ids = Array.from(container.querySelectorAll("[id]"), (element) => element.id);
    expect(new Set(ids).size).toBe(ids.length);

    const comboboxes = screen.getAllByRole("combobox");
    const activeIds: string[] = [];
    for (const input of comboboxes) {
      const form = input.closest("form");
      const listbox = document.getElementById(input.getAttribute("aria-controls") ?? "");
      expect(listbox).toHaveAttribute("role", "listbox");
      expect(form?.contains(listbox)).toBe(true);

      await user.type(input, "шоколад");
      await debounce();
      await settle(ok([product]));
      await user.keyboard("{ArrowDown}");

      const active = document.getElementById(input.getAttribute("aria-activedescendant") ?? "");
      expect(active).toHaveAttribute("role", "option");
      expect(listbox?.contains(active)).toBe(true);
      activeIds.push(active?.id ?? "");
    }

    expect(activeIds[0]).not.toBe(activeIds[1]);
    const allIds = Array.from(container.querySelectorAll("[id]"), (element) => element.id);
    expect(new Set(allIds).size).toBe(allIds.length);
  });
});
