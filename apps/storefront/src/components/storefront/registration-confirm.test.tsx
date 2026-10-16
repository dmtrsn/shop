import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred, jsonResponse } from "@/test/favorites-fixtures";

const { replaceDocument } = vi.hoisted(() => ({ replaceDocument: vi.fn() }));
vi.mock("@/lib/browser-navigation", () => ({ replaceDocument }));

import { RegistrationConfirm } from "./registration-confirm";

const fetcher = vi.fn<typeof fetch>();
const token = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-ABCDE";
const password = "unit-test-only-1";
const session = { user: { id: "usr_new", name: "Анна", email: "ann@example.test" }, expiresAt: "2026-11-01T00:00:00Z", csrfToken: "csrf-new" };
const log = vi.spyOn(console, "log");

function open(hash = `#t=${token}`, path = "/register/confirm") {
  window.history.replaceState(null, "", path + hash);
}
const submit = () => fireEvent.click(screen.getByRole("button", { name: "Подтвердить и войти" }));

beforeEach(() => {
  fetcher.mockReset();
  replaceDocument.mockReset();
  vi.stubGlobal("fetch", fetcher);
  localStorage.clear();
  sessionStorage.clear();
});
afterEach(() => { vi.unstubAllGlobals(); window.history.replaceState(null, "", "/"); });

function assertTokenOnlyInMemory() {
  expect(window.location.href).not.toContain(token);
  expect(document.body.innerHTML).not.toContain(token);
  expect(document.cookie).not.toContain(token);
  for (const storage of [localStorage, sessionStorage]) expect(JSON.stringify({ ...storage })).not.toContain(token);
  expect(JSON.stringify(log.mock.calls)).not.toContain(token);
}

describe("registration confirmation", () => {
  it("clears the fragment on the first render, under StrictMode's double effects too", () => {
    open();
    render(<StrictMode><RegistrationConfirm /></StrictMode>);
    expect(window.location.hash).toBe("");
    expect(window.location.pathname).toBe("/register/confirm");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("autocomplete", "current-password");
    assertTokenOnlyInMemory();
  });

  it("posts the token once, in the body, and opens the account with a full navigation", async () => {
    open();
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    render(<RegistrationConfirm />);
    fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: password } });
    submit();
    fireEvent.submit(screen.getByLabelText("Пароль").closest("form")!);
    expect(screen.getByRole("button", { name: "Подтверждаем…" })).toBeDisabled();
    expect(fetcher).toHaveBeenCalledOnce();
    const [url, init] = fetcher.mock.calls[0];
    expect(url).toBe("/api/auth/registration/confirm");
    expect(JSON.parse(init!.body as string)).toEqual({ token, password });
    await act(async () => pending.resolve(jsonResponse(session, 200, null)));
    await waitFor(() => expect(replaceDocument).toHaveBeenCalledExactlyOnceWith("/account"));
    assertTokenOnlyInMemory();
  });

  it("keeps a basePath taken from its own URL", async () => {
    open(`#t=${token}`, "/preview/register/confirm");
    fetcher.mockResolvedValueOnce(jsonResponse(session, 200, null));
    render(<RegistrationConfirm />);
    fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: password } });
    submit();
    await waitFor(() => expect(replaceDocument).toHaveBeenCalledWith("/preview/account"));
  });

  it("lets the visitor retry a wrong password with the same link", async () => {
    open();
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "unauthorized", message: "x", requestId: "r" } }, 401, null));
    render(<RegistrationConfirm />);
    fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: "wrong-password-1" } });
    submit();
    expect(await screen.findByRole("alert")).toHaveTextContent("Пароль не совпадает");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("aria-invalid", "true");
    fetcher.mockResolvedValueOnce(jsonResponse(session, 200, null));
    fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: password } });
    submit();
    await waitFor(() => expect(replaceDocument).toHaveBeenCalledOnce());
    expect(JSON.parse(fetcher.mock.calls[1][1]!.body as string).token).toBe(token);
  });

  it("explains an invalid, used or expired link and forgets the token", async () => {
    open();
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "link_invalid", message: "x", requestId: "r" } }, 410, null));
    render(<RegistrationConfirm />);
    fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: password } });
    submit();
    expect(await screen.findByRole("heading", { name: "Ссылка недействительна" })).toBeInTheDocument();
    expect(screen.getByText(/Если вы уже подтвердили регистрацию, просто войдите/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login");
    expect(screen.getByRole("link", { name: "Зарегистрироваться снова" })).toHaveAttribute("href", "/register");
    expect(screen.queryByLabelText("Пароль")).toBeNull();
    expect(replaceDocument).not.toHaveBeenCalled();
  });

  it.each(["", "#t=", "#t=short", `#token=${token}`])("shows the incomplete-link state for %j without any request", (hash) => {
    open(hash);
    render(<RegistrationConfirm />);
    expect(screen.getByRole("heading", { name: "Ссылка неполная" })).toBeInTheDocument();
    expect(window.location.hash).toBe("");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("reports an unavailable backend without leaving the form", async () => {
    open();
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    render(<RegistrationConfirm />);
    fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: password } });
    submit();
    expect(await screen.findByRole("alert")).toHaveTextContent("Сервис регистрации недоступен");
    expect(screen.getByRole("button", { name: "Подтвердить и войти" })).toBeEnabled();
  });
});
