import { act, fireEvent, render, screen } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred, jsonResponse } from "@/test/favorites-fixtures";

const { replaceDocument } = vi.hoisted(() => ({ replaceDocument: vi.fn() }));
vi.mock("@/lib/browser-navigation", () => ({ replaceDocument }));

import { PasswordReset } from "./password-reset";

const fetcher = vi.fn<typeof fetch>();
const token = "ZyXwVuTsRqPoNmLkJiHgFeDcBa9876543210-_ZYXWA";
const password = "new-password-123";
const log = vi.spyOn(console, "log");
const submit = () => fireEvent.click(screen.getByRole("button", { name: "Сменить пароль" }));
const open = (hash = `#t=${token}`) => window.history.replaceState(null, "", `/reset-password${hash}`);
const noContent = () => new Response(null, { status: 204 });

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

describe("password reset page", () => {
  it("clears the fragment on the first render, under StrictMode too", () => {
    open();
    render(<StrictMode><PasswordReset /></StrictMode>);
    expect(window.location.hash).toBe("");
    expect(window.location.pathname).toBe("/reset-password");
    const field = screen.getByLabelText("Новый пароль");
    expect(field).toHaveAttribute("autocomplete", "new-password");
    expect(field).toHaveAttribute("type", "password");
    assertTokenOnlyInMemory();
  });

  it("posts the token once in the body, then offers sign-in without signing in", async () => {
    open();
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    render(<PasswordReset />);
    fireEvent.change(screen.getByLabelText("Новый пароль"), { target: { value: password } });
    submit();
    fireEvent.submit(screen.getByLabelText("Новый пароль").closest("form")!);
    expect(fetcher).toHaveBeenCalledOnce();
    const [url, init] = fetcher.mock.calls[0];
    expect(url).toBe("/api/auth/password-reset/confirm");
    expect(JSON.parse(init!.body as string)).toEqual({ token, password });
    await act(async () => pending.resolve(noContent()));
    expect(await screen.findByRole("heading", { name: "Пароль изменён" })).toHaveFocus();
    expect(screen.getByRole("status")).toHaveTextContent("Все прежние входы завершены");
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login");
    expect(replaceDocument).not.toHaveBeenCalled();
    expect(document.body.innerHTML).not.toContain(password);
    assertTokenOnlyInMemory();
  });

  it("shows the password rule for a usable link and keeps the token", async () => {
    open();
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "validation_error", message: "x", requestId: "r", fields: [{ field: "password", message: "Пароль должен быть не короче 10 символов" }] } }, 422, null));
    render(<PasswordReset />);
    fireEvent.change(screen.getByLabelText("Новый пароль"), { target: { value: "short" } });
    submit();
    expect(await screen.findByRole("alert")).toHaveTextContent("Пароль должен быть не короче 10 символов");
    expect(screen.getByLabelText("Новый пароль")).toHaveAttribute("aria-describedby", "reset-password-hint reset-password-error");
    fetcher.mockResolvedValueOnce(noContent());
    fireEvent.change(screen.getByLabelText("Новый пароль"), { target: { value: password } });
    submit();
    await screen.findByRole("heading", { name: "Пароль изменён" });
    expect(JSON.parse(fetcher.mock.calls[1][1]!.body as string).token).toBe(token);
  });

  it("explains an invalid, used or expired link and offers a new one", async () => {
    open();
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "link_invalid", message: "x", requestId: "r" } }, 410, null));
    render(<PasswordReset />);
    fireEvent.change(screen.getByLabelText("Новый пароль"), { target: { value: password } });
    submit();
    expect(await screen.findByRole("heading", { name: "Ссылка недействительна" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Запросить новую ссылку" })).toHaveAttribute("href", "/forgot-password");
    expect(screen.queryByLabelText("Новый пароль")).toBeNull();
  });

  it.each(["", "#t=", `#reset=${token}`])("shows the incomplete-link state for %j without any request", (hash) => {
    open(hash);
    render(<PasswordReset />);
    expect(screen.getByRole("heading", { name: "Ссылка неполная" })).toBeInTheDocument();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("reports an unavailable backend and stays on the form", async () => {
    open();
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    render(<PasswordReset />);
    fireEvent.change(screen.getByLabelText("Новый пароль"), { target: { value: password } });
    submit();
    expect(await screen.findByRole("alert")).toHaveTextContent("Сервис восстановления недоступен");
    expect(screen.getByRole("button", { name: "Сменить пароль" })).toBeEnabled();
  });
});
