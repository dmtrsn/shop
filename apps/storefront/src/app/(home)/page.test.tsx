import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const { getHomePageData } = vi.hoisted(() => ({ getHomePageData: vi.fn() }));

vi.mock("@/lib/api/home", () => ({ getHomePageData }));

import Page, { dynamic, metadata } from "./page";

describe("homepage route", () => {
  it("renders per request from the homepage loader", async () => {
    getHomePageData.mockResolvedValue({
      categories: { status: "unavailable" },
      collection: { status: "ready", data: undefined },
      manufacturers: { status: "unavailable" },
    });

    render(await Page());

    expect(getHomePageData).toHaveBeenCalledOnce();
    expect(screen.getByRole("heading", { level: 1, name: "Хороший вкус. Ничего лишнего." })).toBeInTheDocument();
    expect(dynamic).toBe("force-dynamic");
    expect(metadata.title).toBe("Слой — кондитерские изделия");
  });
});
