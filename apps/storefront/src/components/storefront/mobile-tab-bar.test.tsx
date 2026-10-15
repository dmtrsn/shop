import type Link from "next/link";
import type { ComponentProps } from "react";
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const navigation = vi.hoisted(() => ({ pathname: "/" }));
vi.mock("next/navigation", () => ({ usePathname: () => navigation.pathname }));
vi.mock("next/link", () => ({ default: ({ href, children, prefetch, "aria-current": current }: ComponentProps<typeof Link>) =>
  <a aria-current={current} data-prefetch={String(prefetch)} href={String(href)}>{children}</a> }));

import { MobileTabBar } from "./mobile-tab-bar";

const tabs = () => within(screen.getByRole("navigation", { name: "Основная навигация" })).getAllByRole("link");

describe("MobileTabBar", () => {
  it("has five real, labelled destinations; private ones are not prefetched", () => {
    render(<MobileTabBar />);
    expect(tabs().map((link) => [link.textContent, link.getAttribute("href"), link.getAttribute("data-prefetch")])).toEqual([
      ["Главная", "/", "undefined"],
      ["Каталог", "/catalog", "undefined"],
      ["Избранное", "/favorites", "false"],
      ["Корзина", "/cart", "false"],
      ["Аккаунт", "/account", "false"],
    ]);
    for (const link of tabs()) expect(link).not.toHaveTextContent(/\d/);
  });

  it.each([
    ["/", "Главная"], ["/catalog", "Каталог"], ["/products/chocolate", "Каталог"], ["/categories/bars", "Каталог"],
    ["/favorites", "Избранное"], ["/checkout", "Корзина"], ["/account/orders/ord-1", "Аккаунт"], ["/login", "Аккаунт"],
  ])("marks %s as the %s tab only", (pathname, current) => {
    navigation.pathname = pathname;
    render(<MobileTabBar />);
    expect(tabs().filter((link) => link.getAttribute("aria-current") === "page").map((link) => link.textContent)).toEqual([current]);
  });
});
