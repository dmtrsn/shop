import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StorefrontHeader } from "./header";

describe("StorefrontHeader", () => {
  it("keeps discovery and transaction destinations semantic and named", () => {
    render(<StorefrontHeader />);

    expect(screen.getAllByRole("search")).toHaveLength(2);
    expect(screen.getAllByRole("searchbox", { name: "Поиск по магазину" })).toHaveLength(2);
    expect(screen.getByRole("link", { name: "Каталог" })).toHaveAttribute("href", "/catalog");
    expect(screen.getByRole("link", { name: "Производители" })).toHaveAttribute(
      "href",
      "/manufacturers",
    );
    expect(screen.getAllByRole("link", { name: "Корзина" })).toHaveLength(2);
  });
});
