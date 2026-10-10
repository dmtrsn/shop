import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/lib/legacy-login", () => ({ getLegacyLoginHref: () => "/legacy/#/login" }));

import { metadata } from "./layout";
import NotFound from "./not-found";

describe("root storefront shell", () => {
  it("renders a Russian not-found state with recovery links", () => {
    render(<NotFound />);

    expect(screen.getByRole("heading", { level: 1, name: "Страница не найдена" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "На главную" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("link", { name: "Перейти в каталог" })).toHaveAttribute("href", "/catalog");
    expect(screen.getByRole("main")).toHaveAttribute("id", "main-content");
  });

  it("uses production storefront metadata, not preview wording", () => {
    expect(metadata).toEqual({
      title: "Слой — кондитерские изделия",
      description: "Слой — современный магазин шоколада, печенья, вафель и других кондитерских изделий.",
    });
  });
});
