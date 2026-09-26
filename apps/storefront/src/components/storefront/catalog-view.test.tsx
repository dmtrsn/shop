import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { CatalogData } from "@/lib/api/catalog";
import type { Product } from "@/lib/api/product";
import { parseCatalogState } from "@/lib/catalog-state";

import { CatalogView } from "./catalog-view";

const product: Product = {
  id: "p01",
  slug: "dark-chocolate-sea-salt",
  sku: "CHA-701",
  name: "Тёмный шоколад 70% с морской солью и очень длинным названием для проверки переноса",
  description: "",
  priceMinor: 48900,
  oldPriceMinor: 54900,
  currency: "RUB",
  unit: "плитка 90 г",
  image: "",
  gallery: [],
  category: { id: "cat-bars", slug: "chocolate-bars", name: "Плиточный шоколад" },
  manufacturer: { id: "m-choco", slug: "choco-atelier", name: "Choco Atelier", country: "Россия" },
  brand: { id: "b-choco", slug: "atelier-noir", name: "Atelier Noir", manufacturerId: "m-choco", manufacturer: "Choco Atelier" },
  tags: [],
  badges: ["Новинка"],
  stock: 24,
  minimumQuantity: 1,
  quantityStep: 1,
};

function data(items: Product[] = [product], overrides: Partial<CatalogData["page"]> = {}): CatalogData {
  return {
    page: { items, total: items.length, page: 1, limit: 24, totalPages: items.length ? 1 : 0, ...overrides },
    facets: {
      categories: [], manufacturers: [], brands: [], tags: [], series: [],
      minPriceMinor: 0, maxPriceMinor: 0,
    },
    categories: [{ id: "cat-chocolate", slug: "chocolate", name: "Шоколад", productCount: 6006 }],
    manufacturers: [{ id: "m-choco", slug: "choco-atelier", name: "Choco Atelier", country: "Россия", productCount: 4004 }],
    brands: [{ id: "b-choco", slug: "atelier-noir", name: "Atelier Noir", manufacturerId: "m-choco", manufacturer: "Choco Atelier", productCount: 2002 }],
  };
}

describe("CatalogView", () => {
  it("renders a successful comfortable catalog with real product identity and fallback", () => {
    render(<CatalogView data={data()} state={parseCatalogState({})} />);

    expect(screen.getByRole("heading", { level: 1, name: "Каталог" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: product.name })).toHaveAttribute("href", `/products/${product.slug}`);
    expect(screen.getByText("Арт. CHA-701")).toBeInTheDocument();
    expect(screen.getByText(/489,00\s*₽/)).toBeInTheDocument();
    expect(screen.getByRole("img", { name: `Нет изображения: ${product.name}` })).toBeInTheDocument();
    expect(screen.queryByText("Производитель / бренд / серия")).not.toBeInTheDocument();
    expect(screen.queryByText(/6006|4004|2002/)).not.toBeInTheDocument();
  });

  it("renders compact mode as a work list with stable fields", () => {
    render(<CatalogView data={data()} state={parseCatalogState({ view: "compact" })} />);

    expect(screen.getByText("Производитель / бренд / серия")).toBeInTheDocument();
    expect(screen.getByText("Choco Atelier · Atelier Noir")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Открыть" })).toHaveAttribute("href", `/products/${product.slug}`);
  });

  it("distinguishes a valid empty result and offers a filter reset", () => {
    render(<CatalogView data={data([], { total: 0, totalPages: 0 })} state={parseCatalogState({ q: "нет такого", stock: "1" })} />);

    expect(screen.getByRole("heading", { name: "Ничего не найдено" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Очистить фильтры" })).toHaveAttribute("href", "/catalog");
  });

  it("preserves catalog state in pagination links", () => {
    render(<CatalogView data={data([product], { total: 60, page: 2, totalPages: 3 })} state={parseCatalogState({ manufacturer: "choco-atelier", page: "2", view: "compact" })} />);

    expect(screen.getByText("25–48 из 60")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Далее" })).toHaveAttribute(
      "href",
      "/catalog?manufacturer=choco-atelier&page=3&view=compact",
    );
  });
});
