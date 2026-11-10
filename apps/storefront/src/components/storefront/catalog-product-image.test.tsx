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

  describe("with a thumbnail", () => {
    const original = "/media/med_" + "a".repeat(32);
    const thumbnail = `${original}/thumbnail/${"f".repeat(64)}`;

    it("draws the thumbnail first", () => {
      render(<CatalogProductImage image={original} name="Шоколад" thumbnail={thumbnail} />);

      expect(screen.getByRole("img", { name: "Шоколад" })).toHaveAttribute("src", thumbnail);
    });

    it("retries the original once, then shows the placeholder", () => {
      render(<CatalogProductImage image={original} name="Шоколад" thumbnail={thumbnail} />);

      fireEvent.error(screen.getByRole("img", { name: "Шоколад" }));
      expect(screen.getByRole("img", { name: "Шоколад" })).toHaveAttribute("src", original);

      fireEvent.error(screen.getByRole("img", { name: "Шоколад" }));
      expect(screen.getByRole("img", { name: "Нет изображения: Шоколад" })).toBeInTheDocument();
      expect(screen.queryByRole("img", { name: "Шоколад" })).toBeNull();
    });

    it("moves to the original when the thumbnail failed before hydration", () => {
      const complete = vi.spyOn(HTMLImageElement.prototype, "complete", "get").mockReturnValue(true);
      vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockImplementation(function (this: HTMLImageElement) {
        return this.getAttribute("src") === thumbnail ? 0 : 640;
      });

      render(<CatalogProductImage image={original} name="Шоколад" thumbnail={thumbnail} />);

      expect(complete).toHaveBeenCalled();
      expect(screen.getByRole("img", { name: "Шоколад" })).toHaveAttribute("src", original);
    });

    it("stops at the placeholder when every source failed before hydration", () => {
      vi.spyOn(HTMLImageElement.prototype, "complete", "get").mockReturnValue(true);
      const width = vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockReturnValue(0);

      render(<CatalogProductImage image={original} name="Шоколад" thumbnail={thumbnail} />);

      expect(screen.getByRole("img", { name: "Нет изображения: Шоколад" })).toBeInTheDocument();
      // One check per source, no loop.
      expect(width).toHaveBeenCalledTimes(2);
    });
  });
});
