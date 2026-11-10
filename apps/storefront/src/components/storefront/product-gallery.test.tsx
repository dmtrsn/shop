import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ProductGallery } from "./product-gallery";

const productName = "Шоколад";
const images = ["a", "b", "c"].map((digit) => {
  const src = `/media/med_${digit.repeat(32)}`;
  return { src, thumbnail: `${src}/thumbnail/${"f".repeat(64)}` };
});
const gallery = <ProductGallery images={images} productName={productName} />;

describe("ProductGallery main image", () => {
  afterEach(() => vi.restoreAllMocks());

  it("server-renders the original at high priority with the existing alt and dimensions", () => {
    const container = document.createElement("div");
    container.innerHTML = renderToString(gallery);

    const main = container.querySelector(`img[alt="${productName}"]`)!;
    expect(main).toHaveAttribute("src", images[0].src);
    expect(main).toHaveAttribute("fetchpriority", "high");
    expect(main).toHaveAttribute("width", "720");
    expect(main).toHaveAttribute("height", "720");
    expect(main).not.toHaveAttribute("loading");
    expect(main.getAttribute("src")).not.toContain("/thumbnail/");
    expect(container.textContent).not.toContain("Изображение товара пока недоступно");
    expect([...container.querySelectorAll('button img')].map((img) => img.getAttribute("src")))
      .toEqual(images.map((image) => image.thumbnail));
  });

  it.each([0, 720])("hydrates an already completed original with naturalWidth=%i", (naturalWidth) => {
    const container = document.createElement("div");
    container.innerHTML = renderToString(gallery);
    document.body.appendChild(container);
    const main = container.querySelector(`img[alt="${productName}"]`)!;
    const width = vi.fn(() => naturalWidth);
    Object.defineProperties(main, {
      complete: { configurable: true, get: () => true },
      naturalWidth: { configurable: true, get: width },
    });

    // No error event is dispatched: the request finished before handlers attached.
    render(gallery, { container, hydrate: true });

    if (naturalWidth === 0) {
      expect(screen.getByRole("img", { name: `Нет изображения: ${productName}` }))
        .toHaveTextContent("Изображение товара пока недоступно");
      expect(screen.queryByRole("img", { name: productName })).toBeNull();
    } else {
      expect(screen.getByRole("img", { name: productName })).toBe(main);
      expect(main).toHaveAttribute("src", images[0].src);
    }
    expect(width).toHaveBeenCalledTimes(1);
    expect([...screen.getByRole("group").querySelectorAll("img")].map((img) => img.getAttribute("src")))
      .toEqual(images.map((image) => image.thumbnail));
  });

  it("recovers across failed originals and gallery selections without using thumbnails for the main", async () => {
    const user = userEvent.setup();
    render(gallery);

    for (const index of [0, 1]) {
      const main = screen.getByRole("img", { name: productName });
      expect(main).toHaveAttribute("src", images[index].src);
      expect(main).toHaveAttribute("fetchpriority", "high");
      expect(main).not.toHaveAttribute("loading");
      fireEvent.error(main);
      expect(screen.getByRole("img", { name: `Нет изображения: ${productName}` }))
        .toHaveTextContent("Изображение товара пока недоступно");
      expect(screen.queryByRole("img", { name: productName })).toBeNull();
      await user.click(screen.getByRole("button", { name: `Показать фото ${index + 2} из 3` }));
    }

    expect(screen.getByRole("img", { name: productName })).toHaveAttribute("src", images[2].src);
    // Returning to a formerly failed original also starts with fresh error state.
    await user.click(screen.getByRole("button", { name: "Показать фото 1 из 3" }));
    expect(screen.getByRole("img", { name: productName })).toHaveAttribute("src", images[0].src);
    expect([...screen.getByRole("group").querySelectorAll("img")].map((img) => img.getAttribute("src")))
      .toEqual(images.map((image) => image.thumbnail));
  });

  it("detects an already failed selected original and recovers on the next selection", async () => {
    vi.spyOn(HTMLImageElement.prototype, "complete", "get").mockReturnValue(true);
    vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockImplementation(function (this: HTMLImageElement) {
      return this.alt === productName && this.getAttribute("src") === images[1].src ? 0 : 720;
    });
    const user = userEvent.setup();
    render(gallery);

    await user.click(screen.getByRole("button", { name: "Показать фото 2 из 3" }));
    expect(screen.getByRole("img", { name: `Нет изображения: ${productName}` }))
      .toHaveTextContent("Изображение товара пока недоступно");
    expect(screen.queryByRole("img", { name: productName })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Показать фото 3 из 3" }));
    expect(screen.getByRole("img", { name: productName })).toHaveAttribute("src", images[2].src);
  });
});
