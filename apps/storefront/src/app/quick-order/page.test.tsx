import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { getManufacturers } from "@/lib/api/producer";
import type { Manufacturer } from "@/lib/api/catalog";

vi.mock("@/lib/api/producer", () => ({ getManufacturers: vi.fn() }));
vi.mock("@/components/storefront/quick-order-view", () => ({
  QuickOrderView: ({ manufacturers }: { manufacturers: Manufacturer[] }) => <p>{manufacturers.map((item) => item.name).join(", ")}</p>,
}));

import Page, { dynamic, metadata } from "./page";
import Loading from "./loading";

describe("/quick-order public route", () => {
  it("loads the complete existing public directory without a private route session", async () => {
    vi.mocked(getManufacturers).mockResolvedValueOnce([{ id: "a", slug: "alpha", name: "ООО «Альфа»", country: "Россия", productCount: 500 }]);
    render(await Page());
    expect(screen.getByText("ООО «Альфа»")).toBeInTheDocument();
    expect(getManufacturers).toHaveBeenCalledWith();
    expect(dynamic).toBe("force-dynamic");
    expect(metadata.title).toBe("Быстрый заказ — Слой");
  });

  it("lets directory failures reach its route boundary instead of presenting an empty directory", async () => {
    vi.mocked(getManufacturers).mockRejectedValueOnce(new Error("upstream"));
    await expect(Page()).rejects.toThrow("upstream");
  });

  it("loading is labelled and busy without fake zero counts or empty results", () => {
    render(<Loading />);
    expect(screen.getByRole("main", { name: "Загружаем быстрый заказ" })).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Быстрый заказ");
    expect(screen.queryByText(/ничего не найдено|0 позиций/i)).toBeNull();
  });
});
