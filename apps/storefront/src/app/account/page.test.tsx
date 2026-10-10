import { fireEvent, render, screen } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { PrivateProviders } from "@/test/private-providers";
import { sessionResponse } from "@/test/favorites-fixtures";

const { getServerSession } = vi.hoisted(() => ({ getServerSession: vi.fn() }));
vi.mock("@/lib/api/session", () => ({ getServerSession }));
vi.mock("@/lib/legacy-login", () => ({ getLegacyLoginHref: () => "/legacy/#/login" }));

import Page, { dynamic, metadata } from "./page";
import Loading from "./loading";
import ErrorPage from "./error";

beforeEach(() => { vi.clearAllMocks(); });

describe("account server boundary", () => {
  it("passes only the dynamic session gate and login URL; no private history or credentials", async () => {
    expect(dynamic).toBe("force-dynamic");
    expect(metadata.robots).toEqual({ index: false, follow: false });
    getServerSession.mockResolvedValue({ status: "authenticated", user: sessionResponse().user, csrfToken: "secret" });
    const element = await Page();
    expect(getServerSession).toHaveBeenCalledTimes(1);
    expect(element.props).toEqual({ initialSession: { status: "authenticated", userID: "A" }, loginHref: "/legacy/#/login" });
    const html = renderToStaticMarkup(<PrivateProviders loginHref={null}>{element}</PrivateProviders>);
    expect(html).toContain("Проверяем аккаунт…");
    expect(html).not.toContain("secret");
    expect(html).not.toContain("user@example.test");
  });

  it.each(["anonymous", "unavailable"])("preserves the %s server gate", async (status) => {
    getServerSession.mockResolvedValue({ status });
    expect((await Page()).props.initialSession).toEqual({ status });
  });

  it("has one loading heading and an actionable error boundary without error details", () => {
    expect(renderToStaticMarkup(<Loading />)).toContain('aria-busy="true"');
    const reset = vi.fn();
    render(<ErrorPage error={new Error("private failure")} reset={reset} />);
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(screen.queryByText("private failure")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Повторить" }));
    expect(reset).toHaveBeenCalledOnce();
  });
});
