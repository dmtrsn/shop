import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const router = { replace: vi.fn(), refresh: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router, usePathname: () => "/" }));

import { AdminShell } from "./admin-shell";
import { signOutMessages, StaffSessionProvider } from "./staff-session";

const staff = { id: "stf_" + "d".repeat(32), email: "ops@example.com", displayName: "Ольга Оператор" };
const other = { ...staff, id: "stf_" + "e".repeat(32) };

const session = (who = staff, csrfToken = "csrf-1") => Response.json({ authenticated: true, staff: { ...who, createdAt: "2026-10-19T10:00:00Z" }, expiresAt: "x", csrfToken });

function stubFetch(...responses: Response[]) {
  const fetcher = vi.fn<typeof fetch>();
  for (const response of responses) fetcher.mockResolvedValueOnce(response);
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

function renderShell() {
  return render(
    <StaffSessionProvider staff={staff}>
      <AdminShell>
        <p>Содержимое</p>
      </AdminShell>
    </StaffSessionProvider>,
  );
}

const method = (fetcher: ReturnType<typeof stubFetch>, call: number) => fetcher.mock.calls[call][1]!.method;
const headers = (fetcher: ReturnType<typeof stubFetch>, call: number) => new Headers(fetcher.mock.calls[call][1]!.headers);

describe("authenticated admin shell", () => {
  beforeEach(() => {
    router.replace.mockReset();
    router.refresh.mockReset();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("shows the signed-in staff member and only real destinations", async () => {
    const fetcher = stubFetch(session());
    renderShell();
    expect(screen.getByText("Ольга Оператор")).toBeInTheDocument();
    expect(screen.getByText("ops@example.com")).toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "Разделы" });
    expect(within(nav).getAllByRole("link")).toHaveLength(3);
    expect(within(nav).getByRole("link", { name: "Обзор" })).toHaveAttribute("aria-current", "page");
    expect(within(nav).getByRole("link", { name: "Товары" })).toHaveAttribute("href", "/products");
    expect(within(nav).getByRole("link", { name: "Заказы" })).toHaveAttribute("href", "/orders");
    expect(nav.querySelector("[aria-disabled]")).toBeNull();
    expect(screen.getByRole("main")).toHaveTextContent("Содержимое");
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    expect(method(fetcher, 0)).toBe("GET");
  });

  it("sends an expired session to the sign-in page once", async () => {
    stubFetch(Response.json({ authenticated: false }));
    renderShell();
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
    expect(router.replace).toHaveBeenCalledTimes(1);
  });

  it("keeps the page when Go is unreachable instead of signing out", async () => {
    stubFetch(new Response("bad gateway", { status: 502 }));
    renderShell();
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("re-renders from the server when another staff member signed in elsewhere", async () => {
    stubFetch(session(other));
    renderShell();
    await waitFor(() => expect(router.refresh).toHaveBeenCalled());
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("signs out with the staff CSRF token and expected staff ID, then shows sign-in", async () => {
    const fetcher = stubFetch(session(), new Response(null, { status: 204 }));
    renderShell();
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    await userEvent.setup().click(screen.getByRole("button", { name: "Выйти" }));
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
    expect(method(fetcher, 1)).toBe("DELETE");
    expect(headers(fetcher, 1).get("X-CSRF-Token")).toBe("csrf-1");
    expect(headers(fetcher, 1).get("X-Expected-Staff-ID")).toBe(staff.id);
    expect(headers(fetcher, 1).has("X-Expected-Authenticated-User-ID")).toBe(false);
  });

  it("refuses to sign out someone else and says why", async () => {
    stubFetch(session(), Response.json({ error: { code: "authenticated_staff_changed" } }, { status: 409 }));
    renderShell();
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    await userEvent.setup().click(screen.getByRole("button", { name: "Выйти" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(signOutMessages.changed);
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("refetches a stale CSRF token from Go once and retries", async () => {
    const fetcher = stubFetch(session(staff, "old"), Response.json({ error: { code: "csrf_failed" } }, { status: 403 }), session(staff, "new"), new Response(null, { status: 204 }));
    renderShell();
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    await userEvent.setup().click(screen.getByRole("button", { name: "Выйти" }));
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/login"));
    expect(headers(fetcher, 1).get("X-CSRF-Token")).toBe("old");
    expect(method(fetcher, 2)).toBe("GET");
    expect(headers(fetcher, 3).get("X-CSRF-Token")).toBe("new");
  });

  it("keeps the session token and CSRF out of browser storage", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const cookie = vi.spyOn(document, "cookie", "set");
    stubFetch(session(), new Response(null, { status: 204 }));
    renderShell();
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    await userEvent.setup().click(screen.getByRole("button", { name: "Выйти" }));
    await waitFor(() => expect(router.replace).toHaveBeenCalled());
    expect(setItem).not.toHaveBeenCalled();
    expect(cookie).not.toHaveBeenCalled();
    expect(document.body.innerHTML).not.toContain("csrf-1");
    setItem.mockRestore();
    cookie.mockRestore();
  });
});
