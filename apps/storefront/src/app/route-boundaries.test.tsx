import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentType } from "react";
import { describe, expect, it, vi } from "vitest";

import HomeError from "./(home)/error";
import AccountError from "./account/error";
import RepeatError from "./account/orders/[id]/repeat/error";
import CatalogError from "./catalog/error";
import CheckoutError from "./checkout/error";
import NotFound from "./not-found";
import ProductNotFound from "./products/[slug]/not-found";

type ErrorBoundary = ComponentType<{ error: Error & { digest?: string }; reset: () => void }>;

describe("route error boundaries", () => {
  it.each([
    ["home", HomeError, "Не удалось открыть главную страницу", "/catalog"],
    ["account", AccountError, "Не удалось открыть аккаунт", null],
    ["checkout", CheckoutError, "Не удалось открыть оформление заказа", "/cart"],
    ["catalog", CatalogError, "Не удалось загрузить товары", "/"],
  ] as const)("%s is an alert with retry, never an empty or signed-out state", (_, Boundary: ErrorBoundary, title, href) => {
    const reset = vi.fn();
    render(<Boundary error={new Error("upstream detail")} reset={reset} />);
    expect(screen.getByRole("alert")).toContainElement(screen.getByRole("heading", { level: 1, name: title }));
    expect(screen.queryByText(/upstream detail|ничего не найдено|вы вышли/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
    if (href) expect(screen.getAllByRole("link").map((link) => link.getAttribute("href"))).toEqual([href]);
    else expect(screen.queryByRole("link")).toBeNull();
  });

  it("shows the digest only as an opaque reference, and only when present", () => {
    const { rerender } = render(<CatalogError error={Object.assign(new Error("x"), { digest: "abc123" })} reset={() => {}} />);
    expect(screen.getByText("Код обращения: abc123")).toBeInTheDocument();
    rerender(<CatalogError error={new Error("x")} reset={() => {}} />);
    expect(screen.queryByText(/Код обращения/)).toBeNull();
  });

  it("repeat never offers a retry that could resend an ambiguous submission", () => {
    render(<RepeatError />);
    expect(screen.getByRole("heading", { level: 1, name: "Не удалось открыть повтор заказа" })).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByRole("link", { name: "Вернуться к заказам" })).toHaveAttribute("href", "/account/orders");
  });
});

describe("not-found boundaries", () => {
  it.each([
    ["global", NotFound, "Страница не найдена"],
    ["product", ProductNotFound, "Товар не найден"],
  ] as const)("%s is a calm missing state with recovery links, not an alert", (_, Boundary, title) => {
    render(<Boundary />);
    expect(screen.getByRole("heading", { level: 1, name: title })).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getAllByRole("link").map((link) => link.getAttribute("href"))).toEqual(["/", "/catalog"]);
  });
});
