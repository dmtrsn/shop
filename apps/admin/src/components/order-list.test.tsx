import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const router = { replace: vi.fn(), refresh: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => "/orders" }));

import { apiError, json, mockApi, order, testStaff } from "@/test/api-mock";
import type { OrderQuery } from "@/lib/orders-api";
import { OrderList } from "./order-list";
import { StaffSessionProvider } from "./staff-session";

const query: OrderQuery = { q: "", status: "", page: 1, limit: 50 };
const page = (items = [order({ items: undefined })], extra = {}) => json({ items, total: items.length, page: 1, limit: 50, totalPages: 1, ...extra });

function renderList(q: OrderQuery = query) {
  return render(
    <StaffSessionProvider staff={testStaff}>
      <OrderList query={q} />
    </StaffSessionProvider>,
  );
}

describe("order list", () => {
  beforeEach(() => Object.values(router).forEach((fn) => fn.mockReset()));
  afterEach(() => vi.unstubAllGlobals());

  it("shows orders with their stored status labels and links", async () => {
    const api = mockApi({ "GET /api/admin/v1/orders": page([order({ items: undefined }), order({ id: "ord-2026-002", number: "#10482", status: "delivered", statusLabel: "Доставлен", allowedTransitions: [], items: undefined })]) });
    renderList();
    expect(screen.getByRole("status")).toHaveTextContent("Загрузка заказов");
    const table = await screen.findByRole("table");
    expect(within(table).getAllByRole("columnheader").map((h) => h.textContent)).toEqual(["Заказ", "Создан", "Покупатель", "Статус", "Сумма", "Товаров"]);
    const rows = within(table).getAllByRole("row");
    expect(within(rows[1]).getByRole("link", { name: "Заказ #10483" })).toHaveAttribute("href", "/orders/ord-2026-10483");
    expect(rows[1]).toHaveTextContent("Собираем заказ");
    expect(rows[1]).toHaveTextContent("2 470,00 ₽");
    expect(rows[1]).toHaveTextContent("demo@sugar.ru");
    expect(rows[2]).toHaveTextContent("Доставлен");
    expect(api.called("GET /api/admin/v1/orders")[0].url).toBe("/api/admin/v1/orders?page=1&limit=50");
  });

  it("filters by status and search through the URL", async () => {
    const api = mockApi({ "GET /api/admin/v1/orders": page() });
    renderList({ ...query, status: "processing" });
    await screen.findByRole("table");
    expect(api.called("GET /api/admin/v1/orders")[0].url).toBe("/api/admin/v1/orders?page=1&limit=50&status=processing");
    const user = userEvent.setup();
    await user.selectOptions(screen.getByLabelText("Статус"), "cancelled");
    expect(router.push).toHaveBeenLastCalledWith("/orders?status=cancelled");
    await user.type(screen.getByLabelText("Поиск"), "#10483");
    await user.click(screen.getByRole("button", { name: "Найти" }));
    expect(router.push).toHaveBeenLastCalledWith("/orders?q=%2310483&status=processing");
  });

  it("paginates and handles empty, error and retry", async () => {
    mockApi({ "GET /api/admin/v1/orders": page([order({ items: undefined })], { total: 120, page: 2, totalPages: 3 }) });
    const { unmount } = renderList({ ...query, page: 2 });
    const pages = await screen.findByRole("navigation", { name: "Страницы списка" });
    expect(within(pages).getByRole("link", { name: "← Назад" })).toHaveAttribute("href", "/orders");
    expect(within(pages).getByRole("link", { name: "Вперёд →" })).toHaveAttribute("href", "/orders?page=3");
    unmount();
    mockApi({ "GET /api/admin/v1/orders": page([], { total: 0, totalPages: 0 }) });
    const second = renderList({ ...query, status: "transit" });
    expect(await screen.findByText("Ничего не найдено.")).toBeInTheDocument();
    second.unmount();
    const api = mockApi({ "GET /api/admin/v1/orders": [apiError(503, "service_unavailable"), page()] });
    renderList();
    const alert = await screen.findByRole("alert");
    await userEvent.setup().click(within(alert).getByRole("button", { name: "Повторить" }));
    expect(await screen.findByRole("table")).toBeInTheDocument();
    expect(api.called("GET /api/admin/v1/orders")).toHaveLength(2);
  });
});
