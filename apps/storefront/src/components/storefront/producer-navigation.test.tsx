import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ManufacturerPageData } from "@/lib/api/producer";

import { ManufacturerPage, ManufacturersPage } from "./producer-navigation";

const data: ManufacturerPageData = {
  manufacturer: {
    id: "m-choco",
    slug: "choco-atelier",
    name: "Choco Atelier",
    country: "Россия",
    description: "Bean-to-bar шоколад из Петербурга",
    productCount: 4,
  },
  brands: [{
    id: "b-noir",
    slug: "atelier-noir",
    name: "Atelier Noir",
    manufacturerId: "m-choco",
    manufacturer: "Choco Atelier",
    productCount: 2,
  }],
};

describe("producer page rendering", () => {
  it("renders the directory empty state without demo fallback data", () => {
    render(<ManufacturersPage manufacturers={[]} />);

    expect(screen.getByRole("heading", { level: 1, name: "От фабрик до мастерских" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Производителей пока нет" })).toBeInTheDocument();
  });

  it("renders brands and links manufacturer and brand into the existing catalog", () => {
    render(<ManufacturerPage data={data} />);

    expect(screen.getByRole("heading", { name: "Atelier Noir" })).toBeInTheDocument();
    expect(screen.getAllByRole("link", { name: /Все товары производителя/ })[0]).toHaveAttribute(
      "href",
      "/catalog?manufacturer=choco-atelier",
    );
    expect(screen.getByRole("link", { name: "Товары бренда →" })).toHaveAttribute(
      "href",
      "/catalog?manufacturer=choco-atelier&brand=atelier-noir",
    );
    expect(screen.queryByText("Серии бренда")).not.toBeInTheDocument();
  });
});
