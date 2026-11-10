import type Link from "next/link";
import { createRef, type ComponentProps } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("next/link", () => ({ default: ({ href, prefetch, ...props }: ComponentProps<typeof Link>) =>
  <a {...props} data-prefetch={String(prefetch)} href={String(href)} /> }));

import { HomeLink } from "./home-link";
import { RouteError, RouteNotFound } from "./route-states";

describe("home navigation prefetch", () => {
  it("keeps a real home link, its presentation, ref and click handler without prefetch", () => {
    const ref = createRef<HTMLAnchorElement>();
    const onClick = vi.fn((event) => event.preventDefault());
    render(<HomeLink aria-label="На главную" className="logo" onClick={onClick} ref={ref}>Слой</HomeLink>);

    const link = screen.getByRole("link", { name: "На главную" });
    expect(link).toHaveAttribute("href", "/");
    expect(link).toHaveAttribute("data-prefetch", "false");
    expect(link).toHaveClass("logo");
    expect(link).toHaveTextContent("Слой");
    expect(ref.current).toBe(link);
    fireEvent.click(link);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("disables only home prefetch in shared not-found actions", () => {
    render(<RouteNotFound title="Не найдено" links={[
      { href: "/", label: "Главная" },
      { href: "/catalog", label: "Каталог" },
      { href: "/account", label: "Аккаунт", prefetch: false },
    ]}>Попробуйте каталог.</RouteNotFound>);

    expect(screen.getAllByRole("link").map((link) => [link.getAttribute("href"), link.getAttribute("data-prefetch")])).toEqual([
      ["/", "false"], ["/catalog", "undefined"], ["/account", "false"],
    ]);
  });

  it.each([
    ["/", "false"], ["/catalog", "undefined"],
  ])("preserves the error action to %s with prefetch=%s", (href, prefetch) => {
    const reset = vi.fn();
    render(<RouteError title="Ошибка" link={{ href, label: "Вернуться" }} reset={reset}>Повторите попытку.</RouteError>);
    const link = screen.getByRole("link", { name: "Вернуться" });
    expect(link).toHaveAttribute("href", href);
    expect(link).toHaveAttribute("data-prefetch", prefetch);
    fireEvent.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
