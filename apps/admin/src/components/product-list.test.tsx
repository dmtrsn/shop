import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const router = { replace: vi.fn(), refresh: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => "/products" }));

import { apiError, json, mockApi, product, testStaff } from "@/test/api-mock";
import type { ProductQuery } from "@/lib/products-api";
import { ProductList } from "./product-list";
import { StaffSessionProvider } from "./staff-session";

const query: ProductQuery = { q: "", status: "active", page: 1, limit: 50 };
const page = (items = [product()], extra = {}) => json({ items, total: items.length, page: 1, limit: 50, totalPages: Math.max(1, items.length && 1), ...extra });

function renderList(q: ProductQuery = query) {
  return render(
    <StaffSessionProvider staff={testStaff}>
      <ProductList query={q} />
    </StaffSessionProvider>,
  );
}

describe("product list", () => {
  beforeEach(() => Object.values(router).forEach((fn) => fn.mockReset()));
  afterEach(() => vi.unstubAllGlobals());

  it("shows loading, then the products with links to their editors", async () => {
    const api = mockApi({ "GET /api/admin/v1/products": page([product(), product({ id: "prd_" + "1".repeat(32), name: "Архивный", archived: true, priceMinor: 199 })]) });
    renderList();
    expect(screen.getByRole("status")).toHaveTextContent("Загрузка товаров");
    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row");
    expect(within(table).getAllByRole("columnheader").map((header) => header.textContent)).toEqual(["Товар", "Цена", "Остаток", "Категория и производитель", "Статус", "Ревизия"]);
    expect(within(rows[1]).getByRole("link", { name: "Тёмный шоколад" })).toHaveAttribute("href", "/products/p01");
    expect(rows[1]).toHaveTextContent("489,00 ₽");
    expect(rows[1]).toHaveTextContent("В продаже");
    expect(rows[2]).toHaveTextContent("В архиве");
    expect(rows[2]).toHaveTextContent("1,99 ₽");
    expect(api.called("GET /api/admin/v1/products")[0].url).toBe("/api/admin/v1/products?status=active&page=1&limit=50");
    expect(screen.getByRole("link", { name: "Создать товар" })).toHaveAttribute("href", "/products/new");
  });

  it("puts search, status and page size into the URL instead of filtering locally", async () => {
    mockApi({ "GET /api/admin/v1/products": page() });
    renderList();
    await screen.findByRole("table");
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Поиск"), "  шоколад ");
    await user.click(screen.getByRole("button", { name: "Найти" }));
    expect(router.push).toHaveBeenLastCalledWith("/products?q=%D1%88%D0%BE%D0%BA%D0%BE%D0%BB%D0%B0%D0%B4");
    await user.selectOptions(screen.getByLabelText("Статус"), "archived");
    expect(router.push).toHaveBeenLastCalledWith("/products?status=archived");
    await user.selectOptions(screen.getByLabelText("На странице"), "100");
    expect(router.push).toHaveBeenLastCalledWith("/products?limit=100");
  });

  it("asks Go for the archived filter and the requested page", async () => {
    const api = mockApi({ "GET /api/admin/v1/products": page([product({ archived: true })], { total: 120, page: 2, totalPages: 3 }) });
    renderList({ q: "бар", status: "archived", page: 2, limit: 50 });
    await screen.findByRole("table");
    expect(api.called("GET /api/admin/v1/products")[0].url).toBe("/api/admin/v1/products?status=archived&page=2&limit=50&q=%D0%B1%D0%B0%D1%80");
    const pages = screen.getByRole("navigation", { name: "Страницы списка" });
    expect(within(pages).getByRole("link", { name: "← Назад" })).toHaveAttribute("href", "/products?q=%D0%B1%D0%B0%D1%80&status=archived");
    expect(within(pages).getByRole("link", { name: "Вперёд →" })).toHaveAttribute("href", "/products?q=%D0%B1%D0%B0%D1%80&status=archived&page=3");
    expect(pages).toHaveTextContent("Страница 2 из 3");
  });

  it("has no previous link on the first page and no next link on the last", async () => {
    mockApi({ "GET /api/admin/v1/products": page() });
    renderList();
    const pages = await screen.findByRole("navigation", { name: "Страницы списка" });
    expect(within(pages).queryAllByRole("link")).toHaveLength(0);
  });

  it("says when nothing matches, and when a page is past the end", async () => {
    mockApi({ "GET /api/admin/v1/products": page([], { total: 0, totalPages: 0 }) });
    const { unmount } = renderList({ ...query, q: "zzz" });
    expect(await screen.findByText("Ничего не найдено.")).toBeInTheDocument();
    unmount();
    mockApi({ "GET /api/admin/v1/products": page([], { total: 3, page: 9, totalPages: 1 }) });
    renderList({ ...query, page: 9 });
    expect(await screen.findByText("На этой странице товаров нет.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "К первой странице" })).toHaveAttribute("href", "/products");
  });

  it("reports a backend failure without server text and retries on request", async () => {
    const api = mockApi({ "GET /api/admin/v1/products": [apiError(500, "internal_error"), page()] });
    renderList();
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Не удалось загрузить товары");
    expect(alert).not.toHaveTextContent("server text");
    await userEvent.setup().click(within(alert).getByRole("button", { name: "Повторить" }));
    expect(await screen.findByRole("table")).toBeInTheDocument();
    expect(api.called("GET /api/admin/v1/products")).toHaveLength(2);
  });

  it("sends an expired session to sign-in", async () => {
    mockApi({ "GET /api/admin/v1/products": apiError(401, "unauthorized") });
    renderList();
    await vi.waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
  });
});
