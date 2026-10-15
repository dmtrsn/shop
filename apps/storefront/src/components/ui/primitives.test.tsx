import { fireEvent, render, screen } from "@testing-library/react";
import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";

import { Button, ButtonLink, buttonClassName } from "./button";
import { EmptyState } from "./empty-state";
import { CheckIcon, GridIcon } from "./icons";
import { LogoMark } from "./logo-mark";
import { PageHeader } from "./page-header";
import { Panel } from "./panel";
import { SectionHeader } from "./section-header";
import { Skeleton } from "./skeleton";

describe("Button", () => {
  it("defaults to a non-submitting primary action and keeps native disabled semantics", () => {
    const click = vi.fn();
    render(<Button disabled onClick={click}>Оформить</Button>);
    const button = screen.getByRole("button", { name: "Оформить" });
    expect(button).toHaveAttribute("type", "button");
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(click).not.toHaveBeenCalled();
  });

  it("keeps explicit submit, aria-busy and aria-disabled untouched", () => {
    render(<Button aria-busy aria-disabled type="submit" variant="brand" size="sm">Войти</Button>);
    const button = screen.getByRole("button", { name: "Войти" });
    expect(button).toHaveAttribute("type", "submit");
    expect(button).toHaveAttribute("aria-busy", "true");
    expect(button).toHaveAttribute("aria-disabled", "true");
  });

  it("gives each variant and size its own treatment", () => {
    const classes = (["primary", "brand", "secondary", "quiet", "tertiary"] as const).map((variant) => buttonClassName(variant));
    expect(new Set(classes).size).toBe(5);
    expect(buttonClassName("primary", "sm")).not.toBe(buttonClassName("primary", "md"));
    expect(buttonClassName("secondary", "md", "extra")).toMatch(/ extra$/);
  });

  it("ButtonLink stays a real link with the button treatment", () => {
    render(<ButtonLink href="/catalog" variant="secondary">Каталог</ButtonLink>);
    const link = screen.getByRole("link", { name: "Каталог" });
    expect(link).toHaveAttribute("href", "/catalog");
    expect(link.className).toBe(buttonClassName("secondary"));
    expect(screen.queryByRole("button")).toBeNull();
  });
});

describe("LogoMark", () => {
  it("is decorative unless it is named or shows the wordmark", () => {
    const { container, rerender } = render(<LogoMark />);
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
    expect(screen.queryByRole("img")).toBeNull();
    rerender(<LogoMark label="Слой" />);
    expect(screen.getByRole("img", { name: "Слой" })).toBeInTheDocument();
    rerender(<LogoMark label="Слой" wordmark />);
    expect(screen.queryByRole("img")).toBeNull();
    expect(screen.getByText("слой")).toBeInTheDocument();
  });

  it("uses the original mark geometry", () => {
    const { container } = render(<LogoMark tone="inverse" size={24} />);
    const svg = container.querySelector("svg")!;
    expect(svg).toHaveAttribute("viewBox", "0 0 42 42");
    expect(svg).toHaveAttribute("width", "24");
    expect(svg.querySelector("rect")).toHaveAttribute("rx", "13");
    expect(svg.querySelector("path")).toHaveAttribute("d", "M10 14.5h22L21 29z");
  });
});

describe("layout primitives", () => {
  it("PageHeader renders one h1 with eyebrow, subtitle, actions and focusable title props", () => {
    const ref = createRef<HTMLHeadingElement>();
    render(<PageHeader actions={<Button>Действие</Button>} eyebrow="Аккаунт" subtitle="3 заказа" title="Мои заказы"
      titleProps={{ ref, tabIndex: -1, id: "page-title" }} />);
    const heading = screen.getByRole("heading", { level: 1, name: "Мои заказы" });
    expect(screen.getAllByRole("heading")).toHaveLength(1);
    expect(heading).toHaveAttribute("id", "page-title");
    expect(ref.current).toBe(heading);
    expect(screen.getByText("Аккаунт")).toBeInTheDocument();
    expect(screen.getByText("3 заказа")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Действие" })).toBeInTheDocument();
  });

  it("SectionHeader supports level, id and a see-all link", () => {
    render(<SectionHeader id="cats" kicker="Каталог" level={3} link={{ href: "/categories", label: "Все категории" }} title="Категории" />);
    expect(screen.getByRole("heading", { level: 3, name: "Категории" })).toHaveAttribute("id", "cats");
    expect(screen.getByRole("link", { name: "Все категории" })).toHaveAttribute("href", "/categories");
  });

  it("EmptyState names the situation with a heading, decorative icon and actions", () => {
    const { container } = render(<EmptyState actions={<ButtonLink href="/catalog">В каталог</ButtonLink>} icon={<GridIcon />} level={3} title="Пусто">Добавьте товары.</EmptyState>);
    expect(screen.getByRole("heading", { level: 3, name: "Пусто" })).toBeInTheDocument();
    expect(screen.getByText("Добавьте товары.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "В каталог" })).toHaveAttribute("href", "/catalog");
    expect(container.querySelector("svg")!.closest("[aria-hidden='true']")).not.toBeNull();
  });

  it("Panel renders the requested element and passes attributes", () => {
    render(<Panel aria-label="Итоги" as="aside" lift tone="inverse">Итог</Panel>);
    expect(screen.getByRole("complementary", { name: "Итоги" })).toHaveTextContent("Итог");
  });

  it("Skeleton is hidden from assistive technology and uses no inline style", () => {
    const { container } = render(<Skeleton variant="media" />);
    const element = container.firstElementChild!;
    expect(element).toHaveAttribute("aria-hidden", "true");
    expect(element).not.toHaveAttribute("style");
  });

  it("icons are decorative by default", () => {
    const { container } = render(<CheckIcon />);
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
    expect(container.querySelector("svg")).toHaveAttribute("focusable", "false");
  });
});
