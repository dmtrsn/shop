import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { Manufacturer } from "@/lib/api/catalog";

import { ManufacturerDirectory } from "./manufacturer-directory";

const manufacturers: Manufacturer[] = [
  { id: "m-choco", slug: "choco-atelier", name: "Choco Atelier", country: "Россия", productCount: 4 },
  { id: "m-caffarel", slug: "caffarel", name: "Caffarel", country: "Италия", productCount: 3 },
  { id: "m-belev", slug: "belevskie-sladosti", name: "Белёвские сладости", country: "Россия", productCount: 5 },
];

describe("manufacturer directory", () => {
  it("renders semantic groups and slug-based manufacturer links", () => {
    render(<ManufacturerDirectory manufacturers={manufacturers} />);

    expect(screen.getByRole("navigation", { name: "Алфавитный указатель производителей" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "C" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Б" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Choco Atelier/ })).toHaveAttribute(
      "href",
      "/manufacturers/choco-atelier",
    );
    expect(screen.getByText("Италия · 3 товара в каталоге")).toBeInTheDocument();
  });

  it("filters locally, exposes a no-result state, and returns focus after clear", async () => {
    const user = userEvent.setup();
    render(<ManufacturerDirectory manufacturers={manufacturers} />);
    const search = screen.getByRole("searchbox", { name: "Найти производителя" });

    await user.type(search, "нет такого");
    expect(screen.getByRole("heading", { name: "Производители не найдены" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Очистить поиск" }));

    expect(search).toHaveFocus();
    expect(screen.getByRole("link", { name: /Choco Atelier/ })).toBeInTheDocument();
  });
});
