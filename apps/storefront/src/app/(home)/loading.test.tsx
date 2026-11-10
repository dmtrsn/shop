import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import HomeLoading from "./loading";

describe("home loading boundary", () => {
  it("still renders and preloads the high-priority home hero on the server", () => {
    const document = new DOMParser().parseFromString(renderToStaticMarkup(<HomeLoading />), "text/html");
    const hero = document.querySelector('section[aria-labelledby="home-title"] img');
    expect(hero?.getAttribute("src")).toBe("/brand/hero-confectionery.webp");
    expect(hero?.getAttribute("fetchpriority")).toBe("high");
    expect(hero?.getAttribute("loading")).not.toBe("lazy");
    expect(document.querySelector('link[rel="preload"][as="image"]')?.getAttribute("href")).toBe(hero?.getAttribute("src"));
    expect(document.querySelector("main")?.getAttribute("aria-busy")).toBe("true");
  });
});
