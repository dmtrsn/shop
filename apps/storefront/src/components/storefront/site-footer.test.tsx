import type Link from "next/link";
import type { ComponentProps } from "react";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("next/link", () => ({ default: ({ href, children, prefetch, "aria-label": label }: ComponentProps<typeof Link>) =>
  <a aria-label={label} data-prefetch={String(prefetch)} href={String(href)}>{children}</a> }));

import { SiteFooter } from "./site-footer";

afterEach(() => { vi.unstubAllGlobals(); });

describe("SiteFooter", () => {
  it("links only to real storefront routes; private ones are not prefetched", () => {
    render(<SiteFooter />);
    const links = screen.getAllByRole("link").map((link) => [link.getAttribute("aria-label") ?? link.textContent, link.getAttribute("href"), link.getAttribute("data-prefetch")]);
    expect(links).toEqual([
      ["Слой — на главную", "/", "undefined"],
      ["Все товары", "/catalog", "undefined"],
      ["Категории", "/categories", "undefined"],
      ["Производители", "/manufacturers", "undefined"],
      ["Подборки", "/collections", "undefined"],
      ["Избранное", "/favorites", "false"],
      ["Корзина", "/cart", "false"],
      ["Мои заказы", "/account/orders", "false"],
      ["Аккаунт", "/account", "false"],
    ]);
    expect(screen.getByRole("contentinfo")).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Каталог" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Покупателям" })).toBeInTheDocument();
  });

  it("makes no contact, delivery, hours or VAT claims and makes no request", () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const { container } = render(<SiteFooter />);
    expect(container.textContent).not.toMatch(/\+7|@|телефон|доставк|ежедневно|\d{1,2}:\d{2}|НДС|Москва|оплат/i);
    expect(container.querySelector('a[href^="tel:"], a[href^="mailto:"], a[href^="http"]')).toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });
});
