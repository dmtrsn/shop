import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { CatalogProductImage } from "./catalog-product-image";

describe("CatalogProductImage", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders the API image with the product name as alt text", () => {
    render(<CatalogProductImage image="/assets/p.webp" name="Шоколад" />);

    expect(screen.getByRole("img", { name: "Шоколад" })).toHaveAttribute("src", "/assets/p.webp");
  });

  it("switches to the neutral fallback when the image fails after hydration", () => {
    render(<CatalogProductImage image="/assets/missing.webp" name="Шоколад" />);

    fireEvent.error(screen.getByRole("img", { name: "Шоколад" }));

    expect(screen.getByRole("img", { name: "Нет изображения: Шоколад" })).toBeInTheDocument();
  });

  it("detects an image that already failed before hydration", () => {
    vi.spyOn(HTMLImageElement.prototype, "complete", "get").mockReturnValue(true);
    vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockReturnValue(0);

    render(<CatalogProductImage image="/assets/missing.webp" name="Шоколад" />);

    expect(screen.getByRole("img", { name: "Нет изображения: Шоколад" })).toBeInTheDocument();
  });

  it("keeps an image that loaded before hydration", () => {
    vi.spyOn(HTMLImageElement.prototype, "complete", "get").mockReturnValue(true);
    vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockReturnValue(640);

    render(<CatalogProductImage image="/assets/p.webp" name="Шоколад" />);

    expect(screen.getByRole("img", { name: "Шоколад" })).toBeInTheDocument();
  });
});
