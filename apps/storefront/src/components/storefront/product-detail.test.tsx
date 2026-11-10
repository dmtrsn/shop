import { fireEvent, screen, within } from "@testing-library/react";
import { render } from "@/test/render-with-favorites";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Product } from "@/lib/api/product";

import { ProductDetail } from "./product-detail";

const product: Product = {
  id: "p01",
  slug: "dark-chocolate-sea-salt",
  sku: "CHA-701",
  name: "Тёмный шоколад 70% с морской солью",
  description: "Выразительный вкус и аккуратно подобранные ингредиенты.",
  priceMinor: 48900,
  oldPriceMinor: 54900,
  currency: "RUB",
  unit: "плитка 90 г",
  weightGrams: 90,
  image: "/assets/product-chocolate.webp",
  gallery: [],
  category: { id: "cat-bars", slug: "chocolate-bars", name: "Плиточный шоколад" },
  manufacturer: {
    id: "m-choco",
    slug: "choco-atelier",
    name: "Choco Atelier",
    country: "Россия",
  },
  brand: {
    id: "b-choco",
    slug: "choco-atelier",
    name: "Choco Atelier",
    manufacturerId: "m-choco",
    manufacturer: "Choco Atelier",
  },
  series: "Origins",
  tags: ["веган", "без сахара"],
  badges: ["Новинка"],
  ingredients: "Какао, какао-масло, морская соль",
  shelfLifeDays: 180,
  stock: 24,
  minimumQuantity: 2,
  quantityStep: 2,
};

describe("ProductDetail", () => {
  it("renders managed primary and gallery in server order without adding legacy images", async () => {
    const image = "/media/med_" + "a".repeat(32);
    const gallery = ["/media/med_" + "c".repeat(32), "/media/med_" + "b".repeat(32)];
    const { container } = render(<ProductDetail product={{ ...product, image, gallery }} />);
    expect(screen.getByRole("img", { name: product.name })).toHaveAttribute("src", image);
    const thumbnails = screen.getByRole("group", { name: "Фотографии товара" }).querySelectorAll("img");
    expect([...thumbnails].map((img) => img.getAttribute("src"))).toEqual([image, ...gallery]);
    await userEvent.setup().click(screen.getByRole("button", { name: "Показать фото 2 из 3" }));
    expect(screen.getByRole("img", { name: product.name })).toHaveAttribute("src", gallery[0]);
    expect(container.querySelector('img[src^="/assets/"]')).toBeNull();
  });
  it("keeps the main image original and high priority while gallery controls draw thumbnails", async () => {
    const [image, second] = ["a", "c"].map((digit) => "/media/med_" + digit.repeat(32));
    const thumb = (url: string) => `${url}/thumbnail/${"f".repeat(64)}`;
    render(<ProductDetail product={{ ...product, image, gallery: [second], thumbnails: { [image]: thumb(image), [second]: thumb(second) } }} />);

    const main = screen.getByRole("img", { name: product.name });
    expect(main).toHaveAttribute("src", image);
    expect(main).toHaveAttribute("fetchpriority", "high");
    expect(main).not.toHaveAttribute("loading");
    const controls = screen.getByRole("group", { name: "Фотографии товара" }).querySelectorAll("img");
    expect([...controls].map((img) => img.getAttribute("src"))).toEqual([thumb(image), thumb(second)]);
    expect([...controls].every((img) => img.getAttribute("loading") === "lazy")).toBe(true);

    await userEvent.setup().click(screen.getByRole("button", { name: "Показать фото 2 из 2" }));
    expect(screen.getByRole("img", { name: product.name })).toHaveAttribute("src", second);
    expect(screen.getByRole("button", { name: "Показать фото 2 из 2" })).toHaveAttribute("aria-pressed", "true");
  });

  it("falls back from a broken gallery thumbnail to the original, then to the empty control", () => {
    const [image, second] = ["a", "c"].map((digit) => "/media/med_" + digit.repeat(32));
    const thumbnail = `${second}/thumbnail/${"f".repeat(64)}`;
    render(<ProductDetail product={{ ...product, image, gallery: [second], thumbnails: { [second]: thumbnail } }} />);

    const control = screen.getByRole("button", { name: "Показать фото 2 из 2" });
    fireEvent.error(control.querySelector("img")!);
    expect(control.querySelector("img")).toHaveAttribute("src", second);
    fireEvent.error(control.querySelector("img")!);
    expect(control.querySelector("img")).toBeNull();
    expect(control).toHaveAccessibleName("Показать фото 2 из 2");
    // The main image is independent of its control's fallback.
    expect(screen.getByRole("img", { name: product.name })).toHaveAttribute("src", image);
  });

  it("renders real product identity, price, stock, and local quantity rules", async () => {
    const user = userEvent.setup();
    render(<ProductDetail product={product} />);

    expect(
      screen.getByRole("heading", { level: 1, name: product.name }),
    ).toBeInTheDocument();
    expect(screen.getByText(/489,00\s*₽/)).toBeInTheDocument();
    expect(screen.getByText(/549,00\s*₽/)).toBeInTheDocument();
    expect(screen.getByText("В наличии")).toBeInTheDocument();
    expect(screen.getByText("Минимум 2 · шаг 2")).toBeInTheDocument();

    const quantity = screen.getByRole("textbox", { name: `Количество: ${product.name}` });
    expect(quantity).toHaveValue("2");
    await user.clear(quantity); await user.type(quantity, "4");
    expect(quantity).toHaveValue("4");
    expect(screen.getByRole("button", { name: /добавить в корзину/i })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByText(/не резервирует товар/i)).toBeInTheDocument();
  });

  it("has one h1, a category eyebrow, real lineage and section headings", () => {
    render(<ProductDetail product={product} />);

    expect(screen.getAllByRole("heading", { level: 1 }).map((heading) => heading.textContent)).toEqual([product.name]);
    expect(screen.getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent)).toEqual(["Описание", "О товаре"]);
    expect(screen.getByLabelText("Производитель, бренд и серия")).toHaveTextContent("Choco Atelier·Choco Atelier·Origins");
    const breadcrumbs = screen.getByRole("navigation", { name: "Хлебные крошки" });
    expect(within(breadcrumbs).getByRole("link", { name: "Главная" })).toHaveAttribute("href", "/");
    expect(within(breadcrumbs).getByText(product.name)).toHaveAttribute("aria-current", "page");
  });

  it("shows every real badge verbatim in the shared tones", () => {
    render(<ProductDetail product={{ ...product, badges: ["Новинка", "-11%", "Хит"] }} />);

    expect(screen.getByText("Новинка").className).toMatch(/badgeNew/);
    expect(screen.getByText("-11%").className).toMatch(/badgeSale/);
    expect(screen.getByText("Хит").className).not.toMatch(/badgeNew|badgeSale/);
  });

  it("limits fact chips to real packaging and identity data and adds no claims", () => {
    const { container } = render(<ProductDetail product={product} />);

    const facts = container.querySelector("dl")!;
    expect([...facts.querySelectorAll("dt")].map((term) => term.textContent)).toEqual(["Упаковка", "Артикул"]);
    expect(within(facts as HTMLElement).getByText("плитка 90 г")).toBeInTheDocument();
    expect(within(facts as HTMLElement).getByText("CHA-701")).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/★|рейтинг|отзыв|популяр|бестселлер|доставк|похожие/i);

    render(<ProductDetail product={{ ...product, unit: "коробка", weightGrams: 200 }} />);
    expect(screen.getAllByText("200 г")).toHaveLength(1);
  });

  it("groups price, stock and the real controls in one offer panel", () => {
    const { container } = render(<ProductDetail product={product} />);

    const offer = screen.getByRole("region", { name: "Цена и покупка" });
    expect(within(offer).getByLabelText("Цена")).toHaveTextContent(/489,00\s*₽/);
    expect(within(offer).getByText(/549,00\s*₽/).tagName).toBe("DEL");
    expect(within(offer).getByRole("button", { name: /добавить в корзину/i })).toBeInTheDocument();
    const favorite = container.querySelector('[data-favorite-product="p01"]')!;
    expect(offer).toContainElement(favorite as HTMLElement);
    expect(favorite.closest("a")).toBeNull();
  });

  it("does not fabricate a discount when there is no old price", () => {
    render(<ProductDetail product={{ ...product, oldPriceMinor: undefined, badges: [] }} />);

    expect(screen.getByLabelText("Цена").querySelector("del")).toBeNull();
    expect(screen.queryByText(/[-−]\d+\s?%/)).toBeNull();
  });

  it("states unavailability truthfully and offers no purchase", () => {
    render(<ProductDetail product={{ ...product, stock: 0 }} />);

    expect(screen.getByText("Нет в наличии")).toBeInTheDocument();
    expect(screen.getByText("Покупка недоступна")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /добавить в корзину/i })).toBeNull();
  });

  it("links the product category to its canonical category page", () => {
    render(<ProductDetail product={product} />);

    expect(screen.getByRole("link", { name: "Плиточный шоколад" })).toHaveAttribute(
      "href",
      "/categories/chocolate-bars",
    );
  });

  it("omits absent optional attributes and provides a missing-image state", () => {
    render(
      <ProductDetail
        product={{
          ...product,
          description: "",
          image: "",
          ingredients: undefined,
          series: undefined,
          shelfLifeDays: undefined,
          tags: [],
          weightGrams: undefined,
        }}
      />,
    );

    expect(screen.getByRole("img", { name: `Нет изображения: ${product.name}` })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Описание" })).not.toBeInTheDocument();
    expect(screen.queryByText("Состав")).not.toBeInTheDocument();
    expect(screen.queryByText("Серия")).not.toBeInTheDocument();
  });

  it("falls back when the authoritative image cannot load", () => {
    render(<ProductDetail product={product} />);

    fireEvent.error(screen.getByRole("img", { name: product.name }));

    expect(screen.getByRole("img", { name: `Нет изображения: ${product.name}` })).toBeInTheDocument();
  });
});
