import { beforeEach, describe, expect, it, vi } from "vitest";

const { getCollectionPageData, redirect } = vi.hoisted(() => ({
  getCollectionPageData: vi.fn(),
  redirect: vi.fn((destination: string) => {
    throw new Error(`redirect:${destination}`);
  }),
}));

vi.mock("@/lib/api/collection", () => ({ getCollectionPageData }));
vi.mock("next/navigation", () => ({
  notFound: vi.fn(() => {
    throw new Error("not-found");
  }),
  redirect,
}));

import Page from "./page";

describe("collection detail route", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("redirects an internal ID to the canonical slug URL", async () => {
    getCollectionPageData.mockResolvedValue({
      collection: {
        id: "col-gifts",
        slug: "gifts-under-2000",
        name: "Подарки",
        description: "",
        accent: "coral",
        productIds: [],
        productCount: 0,
      },
      products: [],
    });

    await expect(Page({ params: Promise.resolve({ slug: "col-gifts" }) })).rejects.toThrow(
      "redirect:/collections/gifts-under-2000",
    );
    expect(redirect).toHaveBeenCalledWith("/collections/gifts-under-2000");
  });
});
