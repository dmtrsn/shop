import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({
  usePathname: () => "/",
  useRouter: () => ({ push: vi.fn() }),
}));

import { StorefrontHeader } from "./header";

describe("StorefrontHeader", () => {
  it("keeps discovery and transaction destinations semantic and named", () => {
    render(<StorefrontHeader />);

    expect(screen.getAllByRole("search")).toHaveLength(2);
    const comboboxes = screen.getAllByRole("combobox", { name: "Поиск по магазину" });
    expect(comboboxes).toHaveLength(2);
    expect(comboboxes[0].getAttribute("aria-controls")).not.toBe(
      comboboxes[1].getAttribute("aria-controls"),
    );
    for (const form of screen.getAllByRole("search")) {
      expect(form).toHaveAttribute("action", "/catalog");
    }
    expect(screen.getByRole("link", { name: "Каталог" })).toHaveAttribute("href", "/catalog");
    expect(screen.getByRole("link", { name: "Производители" })).toHaveAttribute(
      "href",
      "/manufacturers",
    );
    expect(screen.getByRole("link", { name: "Подборки" })).toHaveAttribute(
      "href",
      "/collections",
    );
    expect(screen.getAllByRole("link", { name: "Корзина" })).toHaveLength(2);
  });
});
