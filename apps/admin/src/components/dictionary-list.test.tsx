import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const router = { replace: vi.fn(), refresh: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => "/dictionaries/categories" }));

import { apiError, brand, category, collection, json, mockApi, testStaff } from "@/test/api-mock";
import type { DictionaryKind, DictionaryStatus } from "@/lib/dictionaries-api";
import { parseDictionaryStatus } from "@/lib/dictionary-query";
import { collectionPeriod, DictionaryList } from "./dictionary-list";
import { StaffSessionProvider } from "./staff-session";

function renderList(kind: DictionaryKind, status: DictionaryStatus = "active") {
  return render(
    <StaffSessionProvider staff={testStaff}>
      <DictionaryList kind={kind} status={status} />
    </StaffSessionProvider>,
  );
}

describe("dictionary list", () => {
  beforeEach(() => Object.values(router).forEach((fn) => fn.mockReset()));
  afterEach(() => vi.unstubAllGlobals());

  it("shows categories as their two-level tree with usage and status", async () => {
    const api = mockApi({
      "GET /api/admin/v1/categories": json({
        items: [
          category({ childCount: 1 }),
          category({ id: "cat-gifts", slug: "gifts", name: "Подарки", archived: true }),
          category({ id: "cat-bars", slug: "bars", name: "Плитки", parentId: "cat-choc", productCount: 12 }),
        ],
      }),
    });
    renderList("categories", "all");
    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getAllByRole("cell")[0].textContent)).toEqual([
      "Шоколадchocolate · cat-choc",
      "— Плиткиbars · cat-bars",
      "Подаркиgifts · cat-gifts",
    ]);
    expect(rows[1]).toHaveTextContent("12");
    expect(within(rows[2]).getByText("В архиве")).toBeInTheDocument();
    expect(within(rows[1]).getByRole("link", { name: /Плитки/ })).toHaveAttribute("href", "/dictionaries/categories/cat-bars");
    expect(api.calls.find((call) => call.url.startsWith("/api/admin/v1/categories"))?.url).toBe("/api/admin/v1/categories?status=all");
    expect(screen.getByRole("link", { name: "Добавить категорию" })).toHaveAttribute("href", "/dictionaries/categories/new");
    expect(within(screen.getByRole("navigation", { name: "Справочники" })).getByRole("link", { name: "Категории" })).toHaveAttribute("aria-current", "page");
  });

  it("keeps the status filter in the URL", async () => {
    mockApi({ "GET /api/admin/v1/brands": json({ items: [brand()] }) });
    renderList("brands");
    await screen.findByRole("table");
    expect(screen.getByRole("cell", { name: "Choco Atelier" })).toBeInTheDocument();
    await userEvent.setup().selectOptions(screen.getByLabelText("Статус"), "archived");
    expect(router.push).toHaveBeenCalledWith("/dictionaries/brands?status=archived");
    expect(parseDictionaryStatus("deleted")).toBe("active");
    expect(parseDictionaryStatus(["all"])).toBe("all");
  });

  it("describes collection periods in Moscow days", async () => {
    mockApi({ "GET /api/admin/v1/collections": json({ items: [collection({ startsAt: "2026-11-30T21:00:00Z", endsAt: "2027-01-10T20:59:59Z" })] }) });
    renderList("collections");
    expect(await screen.findByRole("cell", { name: "01.12.2026 – 10.01.2027" })).toBeInTheDocument();
    expect(collectionPeriod({})).toBe("Всегда");
  });

  it("offers a retry when the list fails, and says when it is empty", async () => {
    mockApi({ "GET /api/admin/v1/manufacturers": [apiError(500, "internal_error"), json({ items: [] })] });
    renderList("manufacturers", "archived");
    expect(await screen.findByRole("alert")).toHaveTextContent("Не удалось загрузить справочник.");
    await userEvent.setup().click(screen.getByRole("button", { name: "Повторить" }));
    expect(await screen.findByText("В архиве ничего нет.")).toBeInTheDocument();
  });
});
