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

    expect(screen.getAllByRole("heading", { level: 1 }).map((heading) => heading.textContent)).toEqual([
      "Хороший вкус. Ничего лишнего.",
    ]);
    expect(screen.getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent)).toEqual([
      "Найдите свой вкус",
      "Выбрали за вас",
      "Производители",
    ]);
    // Collection cards, then the featured collection's products, all under "Выбрали за вас".
    expect(screen.getAllByRole("heading", { level: 3 }).map((heading) => heading.textContent)).toEqual([
      "Новинки недели",
      "Подарки до 2 000 ₽",
      "Товар p03",
      "Товар p02",
    ]);
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
    expect(screen.getByText("Из подборки «Новинки недели», в редакционном порядке")).toBeInTheDocument();
  });

  it("renders the hero as a decorative, sized, high-priority image owned by Next", () => {
    const { container } = render(<HomePage data={ready()} />);

    const hero = container.querySelector('section[aria-labelledby="home-title"] img')!;
    expect(hero).toHaveAttribute("src", "/brand/hero-confectionery.webp");
    expect(hero).toHaveAttribute("alt", "");
    expect(hero).toHaveAttribute("width", "1672");
    expect(hero).toHaveAttribute("height", "941");
    expect(hero).toHaveAttribute("fetchpriority", "high");
    expect(hero).not.toHaveAttribute("loading", "lazy");
    expect(container.querySelector('img[src="/assets/hero-confectionery.webp"]')).toBeNull();
  });

  it("uses the neutral fallback for a missing product image and no photo for a collection without image", () => {
    render(<HomePage data={ready()} />);

    expect(screen.getByRole("img", { name: "Нет изображения: Товар p02" })).toBeInTheDocument();
    const card = screen.getByRole("link", { name: /^Главная подборка Новинки недели/ });
    expect(card.querySelector("img")).toBeNull();
  });

  it("renders the real collection image as decoration inside its card", () => {
    const data = ready();
    if (data.collection.status === "ready" && data.collection.data) {
      data.collection.data.collection = { ...collection, image: "/assets/collection.webp" };
    }
    render(<HomePage data={data} />);

    const card = screen.getByRole("link", { name: /^Главная подборка Новинки недели/ });
    expect(card).toHaveAttribute("href", "/collections/new");
    expect(card.querySelector("img")).toHaveAttribute("src", "/assets/collection.webp");
    expect(card.querySelector("img")).toHaveAttribute("alt", "");
  });

  it("tints collections only with accents Go serves; anything else stays neutral", () => {
    const data = ready();
    if (data.collection.status === "ready" && data.collection.data) {
      data.collection.data.others = [
        { ...otherCollection, accent: "cocoa" },
        { ...otherCollection, id: "col-x", slug: "x", name: "Подборка X", accent: "magenta" },
      ];
    }
    render(<HomePage data={data} />);

    const className = (name: RegExp) => screen.getByRole("link", { name }).className;
    expect(className(/^Главная подборка Новинки недели/)).toMatch(/accent-lime/);
    expect(className(/^Подборка Подарки до 2 000/)).toMatch(/accent-cocoa/);
    expect(className(/^Подборка Подборка X/)).toMatch(/accentNeutral/);
    expect(className(/^Подборка Подборка X/)).not.toMatch(/accent-/);
  });

  it("offers the featured collection from the hero only when one exists", () => {
    const { unmount } = render(<HomePage data={ready()} />);
    expect(screen.getByRole("link", { name: /Подборка: Новинки недели/ })).toHaveAttribute(
      "href",
      "/collections/new",
    );
    expect(screen.getByRole("link", { name: "Перейти в каталог" })).toHaveAttribute("href", "/catalog");
    unmount();

    render(<HomePage data={ready({ collection: { status: "ready", data: undefined } })} />);
    expect(screen.queryByRole("link", { name: /Подборка:/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Выбрали за вас" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Перейти в каталог" })).toHaveAttribute("href", "/catalog");
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

    expect(screen.queryByRole("heading", { name: "Найдите свой вкус" })).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Производители" })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Выбрали за вас" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 3, name: "Новинки недели" })).toBeInTheDocument();
    expect(within(screen.getByRole("main")).queryByRole("status")).not.toBeInTheDocument();
  });

  it("keeps the hero and a catalog exit with an honest notice when every section failed", () => {
    render(
      <HomePage
        data={{ categories: unavailable, collection: unavailable, manufacturers: unavailable }}
      />,
    );

    const notice = within(screen.getByRole("main")).getByRole("status");
    expect(notice).toHaveTextContent("Не удалось загрузить разделы каталога");
    expect(within(notice).getByRole("link", { name: "откройте каталог" })).toHaveAttribute("href", "/catalog");
    expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();
    expect(screen.queryAllByRole("heading", { level: 2 })).toHaveLength(0);
    expect(screen.getByRole("link", { name: "Перейти в каталог" })).toHaveAttribute("href", "/catalog");
    // Site-wide section links live in the footer (layout), not in the page.
    expect(screen.queryByRole("navigation", { name: "Разделы магазина" })).not.toBeInTheDocument();
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
    // Legacy demo claims stay out: delivery windows, trust strip, freshness, counts.
    expect(text).not.toMatch(/сегодня|16:00|19:00|2 час|свежие партии|контролируем|повтор за минуту|★|берут чаще/i);
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
    expect(screen.getByRole("link", { name: /^Шоколадные конфеты ручной работы без добавленного сахара/ }))
      .toHaveAttribute("href", "/categories/c");
    expect(screen.getByRole("link", { name: /Все категории \(12\)/ })).toHaveAttribute("href", "/categories");
  });
});
