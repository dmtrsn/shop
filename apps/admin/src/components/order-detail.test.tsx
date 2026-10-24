import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const router = { replace: vi.fn(), refresh: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => "/orders/ord-2026-10483" }));

import { apiError, json, mockApi, order, testStaff } from "@/test/api-mock";
import { cancelExplanation, invalidTransitionMessage, OrderDetail, orderConflictMessage } from "./order-detail";
import { StaffSessionProvider } from "./staff-session";

const ORDER = "/api/admin/v1/orders/ord-2026-10483";

function renderDetail() {
  return render(
    <StaffSessionProvider staff={testStaff}>
      <OrderDetail orderId="ord-2026-10483" />
    </StaffSessionProvider>,
  );
}

const noDeletes = (api: ReturnType<typeof mockApi>) => expect(api.calls.filter((call) => call.method === "DELETE")).toEqual([]);

describe("order detail", () => {
  beforeEach(() => Object.values(router).forEach((fn) => fn.mockReset()));
  afterEach(() => vi.unstubAllGlobals());

  it("shows the checkout snapshot for fulfilment", async () => {
    mockApi({ [`GET ${ORDER}`]: json(order()) });
    renderDetail();
    expect(await screen.findByRole("heading", { name: "Заказ #10483" })).toBeInTheDocument();
    expect(screen.getByTestId("order-revision")).toHaveTextContent("1");
    expect(screen.getByText("Москва, ул. Большая Дмитровка, 12, кв. 4")).toBeInTheDocument();
    expect(screen.getByText("Позвонить за час")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "demo@sugar.ru" })).toHaveAttribute("href", "mailto:demo@sugar.ru");
    const table = screen.getByRole("table");
    const rows = within(table).getAllByRole("row");
    expect(rows[1]).toHaveTextContent("Розовый шоколад с малиной");
    expect(rows[1]).toHaveTextContent("1 438,00 ₽");
    expect(within(table).getByRole("rowheader", { name: "Товары" }).closest("tr")).toHaveTextContent("2 170,00 ₽");
    expect(within(table).getByRole("rowheader", { name: "Доставка" }).closest("tr")).toHaveTextContent("300,00 ₽");
    expect(within(table).getByRole("rowheader", { name: "Итого" }).closest("tr")).toHaveTextContent("2 470,00 ₽");
  });

  it("offers exactly the transitions Go allows and sends the shown revision", async () => {
    const api = mockApi({
      [`GET ${ORDER}`]: json(order({ revision: 4 })),
      [`POST ${ORDER}/status`]: json(order({ revision: 5, status: "transit", statusLabel: "В пути", allowedTransitions: ["delivered"] })),
    });
    renderDetail();
    const actions = await screen.findByRole("region", { name: "Действия" });
    expect(within(actions).getAllByRole("button").map((b) => b.textContent)).toEqual(["Передать в доставку", "Отменить заказ…"]);
    expect(within(actions).queryByRole("combobox")).toBeNull();
    await userEvent.setup().click(within(actions).getByRole("button", { name: "Передать в доставку" }));
    expect(await screen.findByText("Статус изменён: «В пути». Ревизия 5.")).toBeInTheDocument();
    expect(api.called(`POST ${ORDER}/status`)[0].body).toEqual({ status: "transit", expectedRevision: 4 });
    expect(api.called(`POST ${ORDER}/status`)[0].headers.get("X-Expected-Staff-ID")).toBe(testStaff.id);
    expect(screen.getByTestId("order-revision")).toHaveTextContent("5");
    expect(within(actions).getAllByRole("button").map((b) => b.textContent)).toEqual(["Отметить доставленным"]);
    noDeletes(api);
  });

  it("on a stale revision keeps the shown order, never retries, and adopts the new one only on reload", async () => {
    const api = mockApi({
      [`GET ${ORDER}`]: [json(order()), json(order({ revision: 3, status: "delivered", statusLabel: "Доставлен", allowedTransitions: [] }))],
      [`POST ${ORDER}/status`]: apiError(409, "stale_revision"),
    });
    renderDetail();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Передать в доставку" }));
    expect(await screen.findByText(orderConflictMessage)).toBeInTheDocument();
    expect(screen.getByTestId("order-revision")).toHaveTextContent("1");
    expect(screen.getByRole("button", { name: "Передать в доставку" })).toBeDisabled();
    expect(api.called(`POST ${ORDER}/status`)).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Загрузить актуальную версию" }));
    expect(await screen.findByText("Загружена актуальная версия. Ревизия 3.")).toBeInTheDocument();
    expect(screen.queryByText(orderConflictMessage)).toBeNull();
    expect(screen.getByText("Заказ доставлен — дальнейших действий нет.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Передать|Отметить|Отменить/ })).toBeNull();
  });

  it("treats an invalid transition like a conflict", async () => {
    mockApi({ [`GET ${ORDER}`]: json(order()), [`POST ${ORDER}/cancel`]: apiError(409, "invalid_transition") });
    renderDetail();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Отменить заказ…" }));
    await user.click(screen.getByRole("button", { name: "Отменить заказ" }));
    expect(await screen.findByText(invalidTransitionMessage)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Загрузить актуальную версию" })).toBeInTheDocument();
  });

  it("cancels only after an explicit, truthful confirmation", async () => {
    const api = mockApi({
      [`GET ${ORDER}`]: json(order({ revision: 2 })),
      [`POST ${ORDER}/cancel`]: json(order({ revision: 3, status: "cancelled", statusLabel: "Отменён", allowedTransitions: [] })),
    });
    renderDetail();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Отменить заказ…" }));
    const dialog = screen.getByRole("group", { name: "Отменить заказ #10483?" });
    expect(dialog).toHaveTextContent(cancelExplanation);
    expect(cancelExplanation).toMatch(/вернутся на склад/);
    expect(cancelExplanation).toMatch(/Деньги автоматически не возвращаются, покупатель уведомление не получает/);
    await waitFor(() => expect(within(dialog).getByRole("heading")).toHaveFocus());
    await user.click(within(dialog).getByRole("button", { name: "Не отменять" }));
    expect(api.called(`POST ${ORDER}/cancel`)).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "Отменить заказ…" }));
    await user.click(screen.getByRole("button", { name: "Отменить заказ" }));
    expect(await screen.findByText("Заказ отменён, товары возвращены на склад. Ревизия 3.")).toBeInTheDocument();
    expect(api.called(`POST ${ORDER}/cancel`)[0].body).toEqual({ expectedRevision: 2 });
    expect(screen.getByText("Заказ отменён — дальнейших действий нет.")).toBeInTheDocument();
    noDeletes(api);
  });

  it("shows no fake controls on a terminal order", async () => {
    mockApi({ [`GET ${ORDER}`]: json(order({ status: "delivered", statusLabel: "Доставлен", allowedTransitions: [], deliveredAt: "2026-10-25T10:00:00Z" })) });
    renderDetail();
    expect(await screen.findByText("Заказ доставлен — дальнейших действий нет.")).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Действия" })).queryAllByRole("button")).toHaveLength(0);
  });

  it("hands an expired session to sign-in", async () => {
    mockApi({ [`GET ${ORDER}`]: json(order()), [`POST ${ORDER}/status`]: apiError(401, "unauthorized") });
    renderDetail();
    await userEvent.setup().click(await screen.findByRole("button", { name: "Передать в доставку" }));
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
    expect(screen.queryByText(/Статус изменён/)).toBeNull();
  });

  it("refreshes a rejected CSRF token once", async () => {
    const api = mockApi({
      [`GET ${ORDER}`]: json(order()),
      "GET /api/admin/v1/auth/session": [
        json({ authenticated: true, staff: { ...testStaff, createdAt: "x" }, expiresAt: "x", csrfToken: "old" }),
        json({ authenticated: true, staff: { ...testStaff, createdAt: "x" }, expiresAt: "x", csrfToken: "new" }),
      ],
      [`POST ${ORDER}/status`]: [apiError(403, "csrf_failed"), json(order({ revision: 2, status: "transit", statusLabel: "В пути", allowedTransitions: ["delivered"] }))],
    });
    renderDetail();
    await screen.findByRole("button", { name: "Передать в доставку" });
    await waitFor(() => expect(api.called("GET /api/admin/v1/auth/session")).toHaveLength(1));
    await userEvent.setup().click(screen.getByRole("button", { name: "Передать в доставку" }));
    expect(await screen.findByText("Статус изменён: «В пути». Ревизия 2.")).toBeInTheDocument();
    expect(api.called(`POST ${ORDER}/status`).map((call) => call.headers.get("X-CSRF-Token"))).toEqual(["old", "new"]);
  });

  it("says when the order does not exist", async () => {
    mockApi({ [`GET ${ORDER}`]: apiError(404, "not_found") });
    renderDetail();
    expect(await screen.findByRole("heading", { name: "Заказ не найден" })).toBeInTheDocument();
  });
});
