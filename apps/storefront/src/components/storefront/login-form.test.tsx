import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred, jsonResponse } from "@/test/favorites-fixtures";

const { replaceDocument } = vi.hoisted(() => ({ replaceDocument: vi.fn() }));
vi.mock("@/lib/browser-navigation", () => ({ replaceDocument }));

import { LoginForm } from "./login-form";

const fetcher = vi.fn<typeof fetch>();
const csrf = "csrf-login-secret";
const success = { user: { id: "A", name: "Покупатель", email: "user@example.test" }, expiresAt: "2026-11-01T00:00:00Z", csrfToken: csrf };

function fill(email = "user@example.test", password = "unit-test-only") {
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: email } });
  fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: password } });
}
const submit = () => fireEvent.click(screen.getByRole("button", { name: "Войти" }));
const posts = () => fetcher.mock.calls.filter(([, init]) => init?.method === "POST");

beforeEach(() => {
  fetcher.mockReset();
  replaceDocument.mockReset();
  vi.stubGlobal("fetch", fetcher);
  window.history.replaceState(null, "", "/login?returnTo=%2Fcheckout");
  localStorage.clear();
  sessionStorage.clear();
});
afterEach(() => { vi.unstubAllGlobals(); window.history.replaceState(null, "", "/"); });

describe("login form", () => {
  it("uses credential autocomplete and never prefills", () => {
    render(<LoginForm target="/checkout" />);
    expect(screen.getByLabelText("Email")).toHaveAttribute("autocomplete", "email");
    expect(screen.getByLabelText("Email")).toHaveValue("");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("autocomplete", "current-password");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("type", "password");
    expect(screen.getByLabelText("Пароль")).toHaveValue("");
  });

  it("posts once and replaces the document with the server-normalized target; the CSRF token is dropped", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse(success, 200, null));
    render(<LoginForm target="/checkout" />);
    fill(" user@example.test ");
    submit();
    await waitFor(() => expect(replaceDocument).toHaveBeenCalledExactlyOnceWith("/checkout"));
    expect(posts()).toHaveLength(1);
    expect(JSON.parse(posts()[0][1]!.body as string)).toEqual({ email: "user@example.test", password: "unit-test-only" });
    expect(screen.getByRole("button", { name: "Входим…" })).toBeDisabled();
    for (const storage of [localStorage, sessionStorage]) {
      expect(JSON.stringify({ ...storage })).not.toContain(csrf);
    }
    expect(document.cookie).not.toContain(csrf);
    expect(window.location.href).not.toContain(csrf);
    expect(document.body.innerHTML).not.toContain(csrf);
  });

  it("restores a basePath taken from its own /login URL", async () => {
    window.history.replaceState(null, "", "/storefront-preview/login?returnTo=%2Fcart");
    fetcher.mockImplementation(async () => jsonResponse(success, 200, null));
    const view = render(<LoginForm target="/cart?q=1#top" />);
    fill(); submit();
    await waitFor(() => expect(replaceDocument).toHaveBeenLastCalledWith("/storefront-preview/cart?q=1#top"));
    view.unmount();
    render(<LoginForm target="/" />);
    fill(); submit();
    await waitFor(() => expect(replaceDocument).toHaveBeenLastCalledWith("/storefront-preview"));
  });

  it("re-checks the target in the browser as defense in depth", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse(success, 200, null));
    render(<LoginForm target="//evil.example/" />);
    fill(); submit();
    await waitFor(() => expect(replaceDocument).toHaveBeenCalledExactlyOnceWith("/"));
  });

  it("ignores repeated submits while the single POST is pending", async () => {
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    render(<LoginForm target="/checkout" />);
    fill();
    const form = screen.getByRole("button", { name: "Войти" }).closest("form")!;
    fireEvent.submit(form);
    fireEvent.submit(form);
    fireEvent.submit(form);
    expect(posts()).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Входим…" })).toBeDisabled();
    await act(async () => { pending.resolve(jsonResponse(success, 200, null)); });
    await waitFor(() => expect(replaceDocument).toHaveBeenCalledOnce());
    expect(posts()).toHaveLength(1);
  });

  it("validates empty fields locally without a request", () => {
    render(<LoginForm target="/checkout" />);
    submit();
    expect(screen.getByText("Укажите email")).toBeInTheDocument();
    expect(screen.getByText("Укажите пароль")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveAttribute("aria-invalid", "true");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it.each([
    [401, "unauthorized", "Неверный email или пароль"],
    [403, "csrf_failed", "Не удалось войти: обновите страницу"],
    [403, "forbidden", "Сервис входа недоступен, попробуйте позже"],
    [500, "internal_error", "Сервис входа недоступен, попробуйте позже"],
    [502, undefined, "Сервис входа недоступен, попробуйте позже"],
  ])("maps %i %s to a safe message, stays on the page and does not retry", async (status, code, message) => {
    fetcher.mockResolvedValue(jsonResponse({ error: { code, message: "backend detail: user exists" } }, status, null));
    render(<LoginForm target="/checkout" />);
    fill(); submit();
    expect(await screen.findByRole("alert")).toHaveTextContent(message);
    expect(screen.queryByText(/backend detail/)).toBeNull();
    expect(replaceDocument).not.toHaveBeenCalled();
    expect(posts()).toHaveLength(1);
    expect(screen.getByLabelText("Email")).toHaveValue("user@example.test");
    expect(screen.getByRole("button", { name: "Войти" })).toBeEnabled();
  });

  it("maps a network failure and a malformed success to the unavailable message", async () => {
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    render(<LoginForm target="/checkout" />);
    fill(); submit();
    expect(await screen.findByRole("alert")).toHaveTextContent("Сервис входа недоступен, попробуйте позже");
    fetcher.mockResolvedValueOnce(jsonResponse({ ...success, csrfToken: "" }, 200, null));
    submit();
    await waitFor(() => expect(posts()).toHaveLength(2));
    expect(await screen.findByRole("alert")).toHaveTextContent("Сервис входа недоступен, попробуйте позже");
    expect(replaceDocument).not.toHaveBeenCalled();
  });

  it("shows Go's field validation messages next to the fields", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "validation_error", message: "Проверьте заполненные поля",
      fields: [{ field: "email", message: "Укажите email" }, { field: "password", message: "Укажите пароль" }, { field: "other", message: "ignored" }] } }, 422, null));
    render(<LoginForm target="/checkout" />);
    fill(); submit();
    expect(await screen.findByText("Укажите пароль")).toHaveAttribute("id", "login-password-error");
    expect(screen.getByText("Укажите email")).toHaveAttribute("id", "login-email-error");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("aria-describedby", "login-password-error");
    expect(screen.queryByText("ignored")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
