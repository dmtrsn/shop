import { screen, within } from "@testing-library/react";
import { render } from "@/test/render-with-favorites";
import { describe, expect, it } from "vitest";

import { favoriteProduct } from "@/test/favorites-fixtures";

import { ProductCard } from "./product-card";

const product = favoriteProduct("p01", {
  slug: "dark-chocolate",
  sku: "CHA-701",
  name: "Тёмный шоколад 70%",
  unit: "плитка 90 г",
  priceMinor: 48900,
  oldPriceMinor: 54900,
  badges: ["Новинка", "-11%", "Хит"],
});

describe("ProductCard", () => {
  it("links image and title to the product and has no separate details link", () => {
    render(<ProductCard product={product} />);
    expect(screen.getByRole("link", { name: "Открыть товар: Тёмный шоколад 70%" })).toHaveAttribute("href", "/products/dark-chocolate");
    expect(screen.getByRole("link", { name: "Тёмный шоколад 70%" })).toHaveAttribute("href", "/products/dark-chocolate");
    expect(screen.getByRole("heading", { level: 2, name: "Тёмный шоколад 70%" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Подробнее" })).toBeNull();
  });

  it("shows real identity, unit and SKU as a secondary line, and the real price pair", () => {
    render(<ProductCard product={product} />);
    expect(screen.getByText("Мастерская · Марка")).toBeInTheDocument();
    expect(screen.getByText("плитка 90 г")).toBeInTheDocument();
    expect(screen.getByText("Арт. CHA-701")).toBeInTheDocument();
    const price = screen.getByLabelText("Цена");
    expect(within(price).getByText(/489,00\s*₽/).tagName).toBe("STRONG");
    expect(within(price).getByText(/549,00\s*₽/).tagName).toBe("DEL");
  });

  it("shows only the first two backend badges verbatim, toned by their meaning", () => {
    render(<ProductCard product={product} />);
    const media = screen.getByRole("link", { name: "Открыть товар: Тёмный шоколад 70%" });
    const badges = [...media.querySelectorAll("span > span")].filter((element) => !element.closest("[role=img]"));
    expect(badges.map((badge) => badge.textContent)).toEqual(["Новинка", "-11%"]);
    expect(badges[0].className).toMatch(/badgeNew/);
    expect(badges[1].className).toMatch(/badgeSale/);
  });

  it("keeps a neutral tone for other editorial badges and invents none", () => {
    render(<ProductCard product={{ ...product, oldPriceMinor: undefined, badges: ["Хит"] }} />);
    const badge = screen.getByText("Хит");
    expect(badge.className).not.toMatch(/badgeNew|badgeSale/);
    expect(screen.queryByText(/★|рейтинг|популяр|мало|скидк/i)).toBeNull();
    expect(screen.getByLabelText("Цена").querySelector("del")).toBeNull();
  });

  it("keeps the real favorite and cart controls outside the product links", () => {
    const { container } = render(<ProductCard product={product} />);
    const favorite = container.querySelector('[data-favorite-product="p01"]')!;
    expect(favorite.tagName).toBe("BUTTON");
    expect(favorite.closest("a")).toBeNull();
    const add = screen.getByRole("button", { name: "Добавить в корзину: Тёмный шоколад 70%" });
    expect(add.closest("a")).toBeNull();
    // Initial state while the private providers settle: busy, never a fake count.
    expect(add).toHaveAttribute("aria-busy", "true");
  });
});
