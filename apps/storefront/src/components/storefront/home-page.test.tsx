import { screen, within } from "@testing-library/react";
import { render } from "@/test/render-with-favorites";
import { describe, expect, it } from "vitest";

import type { Category, Manufacturer } from "@/lib/api/catalog";
import type { Collection } from "@/lib/api/collection";
import type { HomePageData } from "@/lib/api/home";
import type { Product } from "@/lib/api/product";

import { HomePage } from "./home-page";

const categories: Category[] = [
  { id: "cat-chocolate", slug: "chocolate", name: "Шоколад", description: "Плитки, драже и шоколадные наборы", productCount: 8 },
  { id: "cat-marmalade", slug: "marmalade", name: "Мармелад", productCount: 0 },
];

const collection: Collection = {
  id: "col-new",
  slug: "new",
  name: "Новинки недели",
  description: "Свежие вкусы от любимых мануфактур",
  accent: "lime",
  productIds: ["p03", "p02"],
  productCount: 2,
};

const otherCollection: Collection = {
  ...collection,
  id: "col-gifts",
  slug: "gifts-under-2000",
  name: "Подарки до 2 000 ₽",
  productIds: ["p04"],
  productCount: 1,
};

const manufacturers: Manufacturer[] = [
  { id: "m-caffarel", slug: "caffarel", name: "Caffarel", country: "Италия", productCount: 4 },
  { id: "m-kolomna", slug: "kolomna", name: "Коломна", country: "Россия", productCount: 1 },
];

function product(id: string, overrides: Partial<Product> = {}): Product {
  return {
    id,
    slug: `product-${id}`,
    sku: `SKU-${id}`,
    name: `Товар ${id}`,
    description: "",
    priceMinor: 71900,
    currency: "RUB",
    unit: "плитка 85 г",
    image: "",
    gallery: [],
    category: { id: "cat-bars", slug: "chocolate-bars", name: "Плиточный шоколад" },
    manufacturer: { id: "m", slug: "m", name: "Sweetzerland" },
    brand: { id: "b", slug: "b", name: "Sweetzerland" },
    tags: [],
    badges: [],
    stock: 9,
    minimumQuantity: 1,
    quantityStep: 1,
    ...overrides,
  } as Product;
}

function ready(overrides: Partial<HomePageData> = {}): HomePageData {
  return {
    categories: { status: "ready", data: { categories, totalRoots: categories.length } },
    collection: {
      status: "ready",
      data: {
        collection,
        products: [
          product("p03", { image: "/assets/product-chocolate.webp", badges: ["Новинка"] }),
          product("p02"),
        ],
        others: [otherCollection],
      },
    },
    manufacturers: { status: "ready", data: { manufacturers } },
    ...overrides,
  };
}

const unavailable = { status: "unavailable" } as const;

function hrefs(): string[] {
  return screen.getAllByRole("link").map((link) => link.getAttribute("href") ?? "");
}

describe("HomePage", () => {
  it("has one H1 and a logical section order", () => {
    render(<HomePage data={ready()} />);

    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(screen.getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent)).toEqual([
      "Категории",
      "Новинки недели",
      "Производители",
    ]);
    expect(screen.getByRole("heading", { level: 3, name: "Товар p03" })).toBeInTheDocument();
  });

  it("links only to canonical storefront routes", () => {
    render(<HomePage data={ready()} />);

    const links = hrefs();
    for (const href of [
      "/catalog",
      "/categories",
      "/categories/chocolate",
      "/collections",
      "/collections/new",
      "/collections/gifts-under-2000",
      "/products/product-p03",
      "/manufacturers",
      "/manufacturers/caffarel",
    ]) {
      expect(links).toContain(href);
    }
    const canonical = /^\/(catalog|categories|collections|manufacturers|products)(\/[^/?#]+)?$/;
    expect(links.filter((href) => !canonical.test(href))).toEqual([]);
  });

  it("renders collection products in the given editorial order", () => {
    render(<HomePage data={ready()} />);

    const list = screen.getByRole("list", { name: "Товары подборки «Новинки недели»" });
    expect(
      within(list).getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent),
    ).toEqual(["Товар p03", "Товар p02"]);
    expect(screen.getByText("В редакционном порядке")).toBeInTheDocument();
  });

  it("uses the neutral fallback for a missing product image and no slot for a collection without image", () => {
    const { container } = render(<HomePage data={ready()} />);

    expect(screen.getByRole("img", { name: "Нет изображения: Товар p02" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Открыть подборку/ })).not.toBeInTheDocument();
    const hero = container.querySelector('img[src="/assets/hero-confectionery.webp"]');
    expect(hero).toHaveAttribute("alt", "");
    expect(hero).toHaveAttribute("width", "1672");
    expect(hero).toHaveAttribute("height", "941");
  });

  it("renders a real collection image when the API provides one", () => {
    const data = ready();
    if (data.collection.status === "ready" && data.collection.data) {
      data.collection.data.collection = { ...collection, image: "/assets/collection.webp" };
    }
    render(<HomePage data={data} />);

    expect(screen.getByRole("link", { name: "Открыть подборку: Новинки недели" })).toBeInTheDocument();
  });

  it("offers the featured collection from the hero only when one exists", () => {
    const { unmount } = render(<HomePage data={ready()} />);
    expect(screen.getByRole("link", { name: /Подборка: Новинки недели/ })).toHaveAttribute(
      "href",
      "/collections/new",
    );
    unmount();

    render(<HomePage data={ready({ collection: { status: "ready", data: undefined } })} />);
    expect(screen.queryByRole("link", { name: /Подборка:/ })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Открыть каталог" })).toHaveAttribute("href", "/catalog");
  });

  it("omits failed or empty optional sections without a notice", () => {
    render(
      <HomePage
        data={ready({
          categories: unavailable,
          manufacturers: { status: "ready", data: { manufacturers: [] } },
        })}
      />,
    );

    expect(screen.queryByRole("heading", { name: "Категории" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Производители" })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Новинки недели" })).toBeInTheDocument();
    expect(within(screen.getByRole("main")).queryByRole("status")).not.toBeInTheDocument();
  });

  it("keeps the hero and navigation with an honest notice when every section failed", () => {
    render(
      <HomePage
        data={{ categories: unavailable, collection: unavailable, manufacturers: unavailable }}
      />,
    );

    expect(within(screen.getByRole("main")).getByRole("status")).toHaveTextContent("Не удалось загрузить разделы каталога");
    expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();
    expect(screen.queryAllByRole("heading", { level: 2 })).toHaveLength(0);
    const nav = screen.getByRole("navigation", { name: "Разделы магазина" });
    expect(within(nav).getAllByRole("link").map((link) => link.getAttribute("href"))).toEqual([
      "/catalog",
      "/categories",
      "/manufacturers",
      "/collections",
    ]);
  });

  it("shows neutral category and manufacturer metadata", () => {
    render(<HomePage data={ready()} />);

    expect(screen.getByText("8 товаров в каталоге")).toBeInTheDocument();
    expect(screen.getByText("Пока нет товаров")).toBeInTheDocument();
    expect(screen.getByText("Плитки, драже и шоколадные наборы")).toBeInTheDocument();
    expect(screen.getByText("Италия · 4 товара")).toBeInTheDocument();
    expect(screen.getByText("По алфавиту")).toBeInTheDocument();
  });

  it("adds no popularity, recommendation, delivery or discount claims of its own", () => {
    const { container } = render(<HomePage data={ready()} />);

    const text = container.textContent ?? "";
    expect(text).not.toMatch(/популяр|рекоменд|хит|бестселлер|выбор покупателей|доставк|скидк|отзыв|рейтинг/i);
    // Backend editorial badges are shown verbatim, never invented or relabelled.
    expect(screen.getByText("Новинка")).toBeInTheDocument();
  });

  it("renders long Russian names and descriptions in full", () => {
    const longName =
      "Ассорти бельгийских пралине ручной работы с фундуком, миндалём и солёной карамелью в подарочной коробке";
    const longDescription =
      "Редакционная подборка сезонных кондитерских изделий от небольших российских и европейских мануфактур";
    const data = ready({
      categories: {
        status: "ready",
        data: {
          categories: [{ id: "c", slug: "c", name: "Шоколадные конфеты ручной работы без добавленного сахара", productCount: 1 }],
          totalRoots: 12,
        },
      },
    });
    if (data.collection.status === "ready" && data.collection.data) {
      data.collection.data.collection = { ...collection, description: longDescription };
      data.collection.data.products = [product("p01", { name: longName })];
    }
    render(<HomePage data={data} />);

    expect(screen.getByRole("heading", { level: 3, name: longName })).toBeInTheDocument();
    expect(screen.getByText(longDescription)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Шоколадные конфеты ручной работы без добавленного сахара" }))
      .toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Все категории \(12\)/ })).toHaveAttribute("href", "/categories");
  });
});
