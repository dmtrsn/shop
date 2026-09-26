import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Category } from "@/lib/api/catalog";

import { CategoriesPage, CategoryPage, DIRECTORY_CHILD_LIMIT } from "./category-navigation";

const chocolate: Category = { id: "cat-chocolate", slug: "chocolate", name: "Шоколад", description: "Плитки и драже", productCount: 6 };
const bars: Category = { id: "cat-bars", slug: "chocolate-bars", name: "Плиточный шоколад", parentId: "cat-chocolate", productCount: 4 };
const dragee: Category = { id: "cat-dragee", slug: "dragee", name: "Драже в шоколаде", parentId: "cat-chocolate", productCount: 2 };
const gifts: Category = { id: "cat-gifts", slug: "gift-boxes", name: "Подарочные наборы", productCount: 0 };

describe("CategoriesPage", () => {
  it("renders top-level categories with their direct children", () => {
    render(<CategoriesPage categories={[chocolate, bars, dragee, gifts]} />);

    expect(screen.getByRole("heading", { level: 1, name: "Категории" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Шоколад" })).toHaveAttribute("href", "/categories/chocolate");
    expect(screen.getByRole("link", { name: "Подарочные наборы" })).toHaveAttribute("href", "/categories/gift-boxes");
    expect(screen.getByText("Плитки и драже")).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent)).toEqual([
      "Шоколад",
      "Подарочные наборы",
    ]);

    const children = screen.getByRole("list", { name: "Подкатегории: Шоколад" });
    expect(within(children).getByRole("link", { name: "Плиточный шоколад" })).toHaveAttribute("href", "/categories/chocolate-bars");
    expect(within(children).getByText("4 товара в каталоге")).toBeInTheDocument();
    expect(screen.getByText("6 товаров в каталоге")).toBeInTheDocument();
    expect(screen.getByText("Пока нет товаров")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Все товары/ })).toHaveAttribute("href", "/catalog");
  });

  it("does not render grandchildren in the directory", () => {
    const dark: Category = { id: "cat-dark", slug: "dark", name: "Тёмный", parentId: "cat-bars", productCount: 1 };
    render(<CategoriesPage categories={[chocolate, bars, dark]} />);

    expect(screen.getByRole("link", { name: "Плиточный шоколад" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Тёмный" })).not.toBeInTheDocument();
  });

  it("bounds a long child list and links to the category page", () => {
    const many = Array.from({ length: DIRECTORY_CHILD_LIMIT + 3 }, (_, index): Category => ({
      id: `child-${index}`,
      slug: `child-${index}`,
      name: `Подкатегория ${index}`,
      parentId: chocolate.id,
      productCount: 1,
    }));
    render(<CategoriesPage categories={[chocolate, ...many]} />);

    const children = screen.getByRole("list", { name: "Подкатегории: Шоколад" });
    expect(within(children).getAllByRole("listitem")).toHaveLength(DIRECTORY_CHILD_LIMIT);
    expect(screen.getByRole("link", { name: `Все подкатегории (${DIRECTORY_CHILD_LIMIT + 3})` })).toHaveAttribute(
      "href",
      "/categories/chocolate",
    );
  });

  it("distinguishes an empty directory", () => {
    render(<CategoriesPage categories={[]} />);

    expect(screen.getByRole("heading", { name: "Категорий пока нет" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Открыть каталог" })).toHaveAttribute("href", "/catalog");
  });
});

describe("CategoryPage", () => {
  it("renders a parent category with subcategories and catalog entry points", () => {
    render(<CategoryPage data={{ category: chocolate, ancestors: [], children: [bars, dragee], siblings: [chocolate, gifts] }} />);

    const breadcrumbs = screen.getByRole("navigation", { name: "Хлебные крошки" });
    expect(within(breadcrumbs).getByRole("link", { name: "Категории" })).toHaveAttribute("href", "/categories");
    expect(within(breadcrumbs).getByText("Шоколад")).toHaveAttribute("aria-current", "page");

    expect(screen.getByRole("heading", { level: 1, name: "Шоколад" })).toBeInTheDocument();
    expect(screen.getByText("6 товаров в каталоге")).toBeInTheDocument();
    expect(screen.getByText("2 подкатегории")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Все товары категории" })).toHaveAttribute("href", "/catalog?category=chocolate");

    const rows = screen.getByRole("list", { name: "Подкатегории: Шоколад" });
    expect(within(rows).getByRole("link", { name: "Плиточный шоколад" })).toHaveAttribute("href", "/categories/chocolate-bars");
    expect(within(rows).getByRole("link", { name: "Товары: Драже в шоколаде" })).toHaveAttribute("href", "/catalog?category=dragee");
    expect(screen.queryByText(/включая|всего в ветке/i)).not.toBeInTheDocument();
  });

  it("renders a leaf with ancestor breadcrumbs, siblings, and a parent link", () => {
    render(<CategoryPage data={{ category: bars, ancestors: [chocolate], parent: chocolate, children: [], siblings: [bars, dragee] }} />);

    const breadcrumbs = screen.getByRole("navigation", { name: "Хлебные крошки" });
    expect(within(breadcrumbs).getByRole("link", { name: "Шоколад" })).toHaveAttribute("href", "/categories/chocolate");
    expect(within(breadcrumbs).getByText("Плиточный шоколад")).toHaveAttribute("aria-current", "page");

    expect(screen.getByRole("heading", { level: 2, name: "Рядом в «Шоколад»" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "← Шоколад" })).toHaveAttribute("href", "/categories/chocolate");
    const siblings = screen.getByRole("list", { name: "Категории в «Шоколад»" });
    expect(within(siblings).getByText("Плиточный шоколад")).toHaveAttribute("aria-current", "page");
    expect(within(siblings).getByRole("link", { name: "Драже в шоколаде" })).toHaveAttribute("href", "/categories/dragee");
    expect(screen.getByRole("link", { name: "Все товары категории" })).toHaveAttribute("href", "/catalog?category=chocolate-bars");
  });

  it("renders a top-level leaf next to the other top-level categories", () => {
    render(<CategoryPage data={{ category: gifts, ancestors: [], children: [], siblings: [chocolate, gifts] }} />);

    expect(screen.getByRole("heading", { level: 2, name: "Другие категории" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Все категории" })).toHaveAttribute("href", "/categories");
    expect(screen.getAllByText("Пока нет товаров").length).toBeGreaterThan(0);
  });
});
