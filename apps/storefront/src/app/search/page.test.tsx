import { describe, expect, it, vi } from "vitest";

const { redirect } = vi.hoisted(() => ({
  redirect: vi.fn((destination: string) => {
    throw new Error(`redirect:${destination}`);
  }),
}));

vi.mock("next/navigation", () => ({ redirect }));

import Page from "./page";

describe("search route", () => {
  it("redirects to canonical catalog results and keeps valid catalog state", async () => {
    await expect(
      Page({ searchParams: Promise.resolve({ q: " шоколад ", sort: "new", page: "2", junk: "x" }) }),
    ).rejects.toThrow(`redirect:/catalog?${new URLSearchParams({ q: "шоколад", sort: "new", page: "2" })}`);
  });

  it("redirects an empty search to the catalog", async () => {
    await expect(Page({ searchParams: Promise.resolve({}) })).rejects.toThrow("redirect:/catalog");
  });
});
