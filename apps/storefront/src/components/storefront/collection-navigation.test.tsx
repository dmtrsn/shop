import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Collection, CollectionPageData } from "@/lib/api/collection";
import type { Product } from "@/lib/api/product";

import { CollectionPage, CollectionsPage } from "./collection-navigation";

const collection: Collection = {
  id: "col-gifts",
  slug: "gifts-under-2000",
  name: "Подарки до 2 000 ₽ для очень длинного семейного праздника",
  description: "Готовые наборы без долгого выбора и достаточно длинное описание, которое должно переноситься без потери смысла.",
  accent: "coral",
  productIds: ["p02", "p01"],
  productCount: 2,
};

function product(id: string, name: string): Product {
  return {
    id,
    slug: `product-${id}`,
    sku: `SKU-${id}`,
    name,
    description: "",
    priceMinor: 48900,
    currency: "RUB",
    unit: "коробка 180 г",
    image: "",
    gallery: [],
    category: { id: "cat", slug: "category", name: "Категория" },
    manufacturer: { id: "maker", slug: "maker", name: "Мануфактура", country: "Россия" },
    brand: { id: "brand", slug: "brand", name: "Бренд", manufacturerId: "maker", manufacturer: "Мануфактура" },
    tags: [],
    badges: [],
    stock: 4,
    minimumQuantity: 1,
    quantityStep: 1,
  };
}

describe("collection discovery", () => {
  it("renders the directory with a canonical slug link and neutral missing-image state", () => {
    render(<CollectionsPage collections={[collection]} />);

    expect(screen.getByRole("heading", { level: 1, name: "Подборки" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: collection.name })).toHaveAttribute(
      "href",
      `/collections/${collection.slug}`,
    );
    expect(screen.getByRole("img", { name: `Нет изображения: Подборка «${collection.name}»` })).toBeInTheDocument();
    expect(screen.getByText(collection.description)).toBeInTheDocument();
  });

  it("renders an explicit empty directory", () => {
    render(<CollectionsPage collections={[]} />);

    expect(screen.getByRole("heading", { name: "Подборок пока нет" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Открыть каталог" })).toHaveAttribute("href", "/catalog");
  });

  it("renders collection context, ordered products and the real catalog filter", () => {
    const data: CollectionPageData = {
      collection,
      products: [product("p02", "Второй по алфавиту"), product("p01", "Первый по алфавиту")],
    };
    render(<CollectionPage data={data} />);

    expect(screen.getByRole("heading", { level: 1, name: collection.name })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Все товары подборки" })).toHaveAttribute(
      "href",
      "/catalog?collection=gifts-under-2000",
    );
    const list = screen.getByRole("list");
    expect(within(list).getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual([
      "Второй по алфавиту",
      "Первый по алфавиту",
    ]);
  });

  it("distinguishes an empty collection from an empty directory", () => {
    render(<CollectionPage data={{ collection: { ...collection, productIds: [], productCount: 0 }, products: [] }} />);

    expect(screen.getByRole("heading", { name: "В подборке пока нет товаров" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Подборок пока нет" })).not.toBeInTheDocument();
  });
});
