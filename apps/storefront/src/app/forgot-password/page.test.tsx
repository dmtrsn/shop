import { render, screen } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getAuthCapabilities } = vi.hoisted(() => ({ getAuthCapabilities: vi.fn() }));
vi.mock("@/lib/api/session", () => ({ getAuthCapabilities }));
vi.mock("@/components/storefront/forgot-password-form", () => ({ ForgotPasswordForm: () => <form aria-label="recovery" /> }));

import Page, { dynamic, metadata } from "./page";
import ResetPage, { dynamic as resetDynamic, metadata as resetMetadata } from "../reset-password/page";

beforeEach(() => {
  vi.clearAllMocks();
  getAuthCapabilities.mockResolvedValue({ status: "available", demoLogin: false, selfService: true });
});

describe("recovery pages", () => {
  it("are dynamic, not indexed, and the reset page sends no referrer", () => {
    expect(dynamic).toBe("force-dynamic");
    expect(resetDynamic).toBe("force-dynamic");
    expect(metadata.robots).toEqual({ index: false, follow: false });
    expect(resetMetadata.robots).toEqual({ index: false, follow: false });
    expect(resetMetadata.referrer).toBe("no-referrer");
  });

  it("show the request form only when Go advertises self-service", async () => {
    render(await Page());
    expect(screen.getByRole("form", { name: "recovery" })).toBeInTheDocument();
  });

  it("do not offer dead recovery without self-service, and treat an outage as unavailable", async () => {
    getAuthCapabilities.mockResolvedValue({ status: "available", demoLogin: true, selfService: false });
    const { unmount } = render(await Page());
    expect(screen.queryByRole("form")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("нельзя восстановить пароль самостоятельно");
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login");
    unmount();
    getAuthCapabilities.mockResolvedValue({ status: "unavailable" });
    render(await Page());
    expect(screen.getByRole("status")).toHaveTextContent("временно недоступен");
  });

  it("renders the reset shell with no token on the server", () => {
    const html = renderToStaticMarkup(<ResetPage />);
    expect(html).toContain("Новый пароль");
    expect(html).toContain("Проверяем ссылку");
  });
});
