import type Link from "next/link";
import type { ComponentProps } from "react";
import { render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({
  usePathname: () => "/",
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("next/link", () => ({ default: ({ href, children, prefetch, className, "aria-label": label }: ComponentProps<typeof Link>) =>
  <a aria-label={label} className={className} data-prefetch={String(prefetch)} href={String(href)}>{children}</a> }));

import { StorefrontHeader } from "./header";

afterEach(() => { vi.unstubAllGlobals(); });

describe("StorefrontHeader", () => {
  it("leads with the logo, the Каталог control and one search", () => {
    render(<StorefrontHeader />);
    const logo = screen.getByRole("link", { name: "Слой — на главную" });
    expect(logo).toHaveAttribute("href", "/");
    expect(logo.querySelector("svg")).toHaveAttribute("viewBox", "0 0 42 42");
    expect(logo).toHaveTextContent("слой");
    expect(screen.getByRole("link", { name: "Каталог" })).toHaveAttribute("href", "/catalog");
    expect(screen.getAllByRole("search")).toHaveLength(1);
    expect(screen.getByRole("search")).toHaveAttribute("action", "/catalog");
    expect(screen.getByRole("combobox", { name: "Поиск по магазину" })).toBeInTheDocument();
  });

  it("customer actions are real, labelled, never prefetched and carry no counts", () => {
    render(<StorefrontHeader />);
    const nav = screen.getByRole("navigation", { name: "Покупки и аккаунт" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((link) => [link.textContent, link.getAttribute("href"), link.getAttribute("data-prefetch")])).toEqual([
      ["Избранное", "/favorites", "false"],
      ["Заказы", "/account/orders", "false"],
      ["Аккаунт", "/account", "false"],
      ["Корзина", "/cart", "false"],
    ]);
    for (const link of links) expect(link).not.toHaveTextContent(/\d/);
  });

  it("renders without any request and without a fake utility bar", () => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const { container } = render(<StorefrontHeader />);
    expect(fetch).not.toHaveBeenCalled();
    expect(container.textContent).not.toMatch(/Доставка|Москва|\+7|@/);
    expect(container.querySelector('a[href^="tel:"], a[href^="mailto:"]')).toBeNull();
  });
});
