import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getCategoryPageData, notFound, redirect } = vi.hoisted(() => ({
  getCategoryPageData: vi.fn(),
  notFound: vi.fn(() => {
    throw new Error("not-found");
  }),
  redirect: vi.fn((destination: string) => {
    throw new Error(`redirect:${destination}`);
  }),
}));

vi.mock("@/lib/api/category", () => ({ getCategoryPageData }));
vi.mock("next/navigation", () => ({ notFound, redirect }));

import Page from "./page";

const chocolate = { id: "cat-chocolate", slug: "chocolate", name: "Шоколад", productCount: 6 };

describe("category detail route", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getCategoryPageData.mockResolvedValue({
      category: chocolate,
      ancestors: [],
      children: [],
      siblings: [chocolate],
    });
  });

  it("redirects an internal ID to the canonical slug URL", async () => {
    await expect(Page({ params: Promise.resolve({ slug: "cat-chocolate" }) })).rejects.toThrow(
      "redirect:/categories/chocolate",
    );
    expect(getCategoryPageData).toHaveBeenCalledWith("cat-chocolate");
  });

  it("renders the not-found boundary for an unknown category", async () => {
    getCategoryPageData.mockResolvedValue(undefined);

    await expect(Page({ params: Promise.resolve({ slug: "missing" }) })).rejects.toThrow("not-found");
    expect(redirect).not.toHaveBeenCalled();
  });

  it("renders a category requested by its canonical slug", async () => {
    render(await Page({ params: Promise.resolve({ slug: "chocolate" }) }));

    expect(screen.getByRole("heading", { level: 1, name: "Шоколад" })).toBeInTheDocument();
    expect(redirect).not.toHaveBeenCalled();
    expect(notFound).not.toHaveBeenCalled();
  });
});
