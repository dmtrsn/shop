import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import type { CatalogFacets } from "@/lib/api/catalog";
import { parseCatalogState } from "@/lib/catalog-state";

import { CatalogFilterForm } from "./catalog-filter-form";

const categories = [
  { id: "cat-chocolate", slug: "chocolate", name: "Шоколад", productCount: 6006 },
];
const manufacturers = [
  { id: "m-choco", slug: "choco-atelier", name: "Choco Atelier", country: "Россия", productCount: 4004 },
  { id: "m-north", slug: "north", name: "Северная", country: "Россия", productCount: 3003 },
];
const brands = [
  { id: "b-noir", slug: "atelier-noir", name: "Atelier Noir", manufacturerId: "m-choco", manufacturer: "Choco Atelier", productCount: 2002 },
  { id: "b-maison", slug: "atelier-maison", name: "Atelier Maison", manufacturerId: "m-choco", manufacturer: "Choco Atelier", productCount: 1001 },
  { id: "b-arctic", slug: "arctic", name: "Арктика", manufacturerId: "m-north", manufacturer: "Северная", productCount: 909 },
];
const facets: CatalogFacets = {
  categories: [],
  manufacturers: [],
  brands: [],
  tags: [],
  series: [{ id: "Origins", slug: "Origins", name: "Origins", count: 2 }],
  minPriceMinor: 0,
  maxPriceMinor: 0,
};

function renderForm(params: Record<string, string> = {}) {
  return render(
    <CatalogFilterForm
      brands={brands}
      categories={categories}
      facets={facets}
      manufacturers={manufacturers}
      prefix="test-filter"
      state={parseCatalogState(params)}
    />,
  );
}

describe("CatalogFilterForm producer hierarchy", () => {
  it("does not label directory options with global product totals", () => {
    renderForm({ manufacturer: "choco-atelier" });

    expect(screen.getByRole("option", { name: "Шоколад" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Choco Atelier" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Atelier Noir" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /6006|4004|2002/ })).not.toBeInTheDocument();
  });

  it("indents category options by tree depth in hierarchy order", () => {
    render(
      <CatalogFilterForm
        brands={brands}
        categories={[
          { id: "cat-dark", slug: "dark", name: "Тёмный", parentId: "cat-bars", productCount: 1 },
          { id: "cat-bars", slug: "chocolate-bars", name: "Плиточный", parentId: "cat-chocolate", productCount: 4 },
          ...categories,
        ]}
        facets={facets}
        manufacturers={manufacturers}
        prefix="test-filter"
        state={parseCatalogState({})}
      />,
    );

    const options = Array.from(
      (screen.getByLabelText("Категория") as HTMLSelectElement).options,
    ).map((option) => [option.value, option.textContent]);
    expect(options).toEqual([
      ["", "Все категории"],
      ["chocolate", "Шоколад"],
      ["chocolate-bars", "— Плиточный"],
      ["dark", "— — Тёмный"],
    ]);
  });

  it("clears and stages brand and series after manufacturer changes", async () => {
    const user = userEvent.setup();
    renderForm({ manufacturer: "choco-atelier", brand: "atelier-noir", series: "Origins" });

    await user.selectOptions(screen.getByLabelText("Производитель"), "north");

    expect(screen.getByLabelText("Бренд")).toHaveValue("");
    expect(screen.getByLabelText("Бренд")).toBeDisabled();
    expect(screen.getByLabelText("Серия")).toHaveValue("");
    expect(screen.getByLabelText("Серия")).toBeDisabled();
    expect(screen.getByText(/примените производителя/i)).toBeInTheDocument();
  });

  it("clears and stages series after brand changes", async () => {
    const user = userEvent.setup();
    renderForm({ manufacturer: "choco-atelier", brand: "atelier-noir", series: "Origins" });

    await user.selectOptions(screen.getByLabelText("Бренд"), "atelier-maison");

    expect(screen.getByLabelText("Бренд")).toHaveValue("atelier-maison");
    expect(screen.getByLabelText("Серия")).toHaveValue("");
    expect(screen.getByLabelText("Серия")).toBeDisabled();
    expect(screen.getByText(/примените бренд/i)).toBeInTheDocument();
  });
});
