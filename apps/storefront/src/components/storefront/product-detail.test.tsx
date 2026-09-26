import { fireEvent, render, screen } from "@testing-library/react";
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
  ingredients: "Какао, какао-масло, морская соль",
  shelfLifeDays: 180,
  stock: 24,
  minimumQuantity: 2,
  quantityStep: 2,
};

describe("ProductDetail", () => {
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

    const quantity = screen.getByRole("spinbutton", { name: `Количество: ${product.name}` });
    expect(quantity).toHaveValue(2);
    await user.click(screen.getByRole("button", { name: `Увеличить: количество: ${product.name.toLowerCase()}` }));
    expect(quantity).toHaveValue(4);
    expect(screen.getByRole("button", { name: /добавление в корзину/i })).toBeDisabled();
    expect(screen.getByText(/не резервирует товар/i)).toBeInTheDocument();
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
