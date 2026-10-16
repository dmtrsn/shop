import { render, screen } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getServerSession, getAuthCapabilities, redirect } = vi.hoisted(() => ({
  getServerSession: vi.fn(),
  getAuthCapabilities: vi.fn(),
  redirect: vi.fn((target: string) => { throw Object.assign(new Error("NEXT_REDIRECT"), { target }); }),
}));
vi.mock("@/lib/api/session", () => ({ getServerSession, getAuthCapabilities }));
vi.mock("next/navigation", () => ({ redirect }));
vi.mock("@/components/storefront/register-form", () => ({ RegisterForm: () => <form aria-label="registration" /> }));

import Page, { dynamic, metadata } from "./page";
import ConfirmPage, { dynamic as confirmDynamic, metadata as confirmMetadata } from "./confirm/page";
import Loading from "./loading";

beforeEach(() => {
  vi.clearAllMocks();
  getServerSession.mockResolvedValue({ status: "anonymous" });
  getAuthCapabilities.mockResolvedValue({ status: "available", demoLogin: false, selfService: true });
});

describe("/register", () => {
  it("is dynamic and not indexed", () => {
    expect(dynamic).toBe("force-dynamic");
    expect(confirmDynamic).toBe("force-dynamic");
    expect(metadata.robots).toEqual({ index: false, follow: false });
    expect(confirmMetadata.robots).toEqual({ index: false, follow: false });
    expect(confirmMetadata.referrer).toBe("no-referrer");
  });

  it("shows the form when Go advertises self-registration", async () => {
    render(await Page());
    expect(screen.getByRole("heading", { level: 1, name: "Регистрация" })).toBeInTheDocument();
    expect(screen.getByRole("form", { name: "registration" })).toBeInTheDocument();
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("explains a shop without self-registration and links to sign-in", async () => {
    getAuthCapabilities.mockResolvedValue({ status: "available", demoLogin: true, selfService: false });
    render(await Page());
    expect(screen.queryByRole("form")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("нельзя зарегистрироваться самостоятельно");
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login");
  });

  it("reports an outage as unavailable, not as disabled", async () => {
    getAuthCapabilities.mockResolvedValue({ status: "unavailable" });
    render(await Page());
    expect(screen.queryByRole("form")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("временно недоступен");
  });

  it("sends a signed-in visitor to the account", async () => {
    getServerSession.mockResolvedValue({ status: "authenticated", user: { id: "A", name: "N", email: "a@test.ru" } });
    await expect(Page()).rejects.toMatchObject({ target: "/account" });
    expect(getAuthCapabilities).not.toHaveBeenCalled();
  });

  it("renders the confirmation shell without any token on the server", () => {
    const html = renderToStaticMarkup(<ConfirmPage />);
    expect(html).toContain("Подтверждение регистрации");
    expect(html).toContain("Проверяем ссылку");
    expect(renderToStaticMarkup(<Loading />)).toContain('aria-busy="true"');
  });
});
