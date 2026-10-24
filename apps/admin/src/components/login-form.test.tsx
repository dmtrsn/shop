import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const router = { replace: vi.fn(), refresh: vi.fn(), push: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => router }));

import { LoginForm, loginMessages } from "./login-form";

const staff = { id: "stf_" + "c".repeat(32), email: "ops@example.com", displayName: "Оператор", createdAt: "2026-10-19T10:00:00Z" };

function stubFetch(response: Response) {
  const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

async function submit(email: string, password: string) {
  const user = userEvent.setup();
  if (email) await user.type(screen.getByLabelText("Email"), email);
  if (password) await user.type(screen.getByLabelText("Пароль"), password);
  await user.click(screen.getByRole("button", { name: "Войти" }));
}

describe("staff login form", () => {
  beforeEach(() => {
    router.replace.mockReset();
    router.refresh.mockReset();
  });
  afterEach(() => vi.unstubAllGlobals());

  it("labels fields for password managers and accessibility", () => {
    render(<LoginForm />);
    expect(screen.getByLabelText("Email")).toHaveAttribute("autocomplete", "email");
    expect(screen.getByLabelText("Email")).toHaveAttribute("type", "email");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("autocomplete", "current-password");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("type", "password");
    expect(screen.queryByText(/забыли|регистрац|создать/i)).toBeNull();
  });

  it("requires both fields before calling Go", async () => {
    const fetcher = stubFetch(new Response(null, { status: 500 }));
    render(<LoginForm />);
    await submit("", "");
    expect(screen.getByText(loginMessages.email)).toBeInTheDocument();
    expect(screen.getByText(loginMessages.password)).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveAttribute("aria-invalid", "true");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("posts the credentials and hands over to the server gate on success", async () => {
    const fetcher = stubFetch(Response.json({ staff, expiresAt: "2026-10-19T22:00:00Z", csrfToken: "csrf" }));
    render(<LoginForm />);
    await submit(" ops@example.com ", "staff password 1");
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/"));
    expect(router.refresh).toHaveBeenCalled();
    const [url, init] = fetcher.mock.calls[0];
    expect(url).toBe("/api/admin/v1/auth/session");
    expect(JSON.parse(init!.body as string)).toEqual({ email: "ops@example.com", password: "staff password 1" });
  });

  it("shows one generic message for any rejected sign-in and clears the password", async () => {
    stubFetch(Response.json({ error: { code: "unauthorized", message: "x", requestId: "r" } }, { status: 401 }));
    render(<LoginForm />);
    await submit("ops@example.com", "wrong password");
    expect(await screen.findByRole("alert")).toHaveTextContent(loginMessages.invalid);
    expect(screen.getByLabelText("Пароль")).toHaveValue("");
    expect(screen.getByLabelText("Email")).toHaveValue("ops@example.com");
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("explains the edge rate limit with its retry hint", async () => {
    stubFetch(new Response("Too many sign-in attempts. Try again later.\n", { status: 429, headers: { "Retry-After": "12" } }));
    render(<LoginForm />);
    await submit("ops@example.com", "pw");
    expect(await screen.findByRole("alert")).toHaveTextContent("Повторите через 12 с.");
    expect(screen.getByRole("button", { name: "Войти" })).toBeEnabled();
  });

  it("reports an unreachable service without blaming the credentials", async () => {
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockRejectedValue(new TypeError("offline")));
    render(<LoginForm />);
    await submit("ops@example.com", "pw");
    expect(await screen.findByRole("alert")).toHaveTextContent(loginMessages.unavailable);
  });

  it("stores nothing in the browser", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const cookie = vi.spyOn(document, "cookie", "set");
    stubFetch(Response.json({ staff, expiresAt: "2026-10-19T22:00:00Z", csrfToken: "csrf" }));
    render(<LoginForm />);
    await submit("ops@example.com", "staff password 1");
    await waitFor(() => expect(router.replace).toHaveBeenCalled());
    expect(setItem).not.toHaveBeenCalled();
    expect(cookie).not.toHaveBeenCalled();
    setItem.mockRestore();
    cookie.mockRestore();
  });
});
