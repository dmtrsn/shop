import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { BrowserRequestError } from "@/lib/api/browser-request";
import { deferred, jsonResponse } from "@/test/favorites-fixtures";

import { ForgotPasswordForm, resetRequestFailure } from "./forgot-password-form";

const fetcher = vi.fn<typeof fetch>();
const submit = () => fireEvent.click(screen.getByRole("button", { name: "Отправить ссылку" }));

beforeEach(() => { fetcher.mockReset(); vi.stubGlobal("fetch", fetcher); });
afterEach(() => { vi.unstubAllGlobals(); });

describe("forgot password form", () => {
  it("has a labelled email field with email autocomplete and a way back to sign-in", () => {
    render(<ForgotPasswordForm />);
    expect(screen.getByLabelText("Email")).toHaveAttribute("autocomplete", "email");
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login");
  });

  it("asks for the address without sending anything", () => {
    render(<ForgotPasswordForm />);
    submit();
    expect(screen.getByText("Укажите email")).toBeInTheDocument();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("posts once and shows the same generic answer for every address", async () => {
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    render(<ForgotPasswordForm />);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: " kate@example.test " } });
    submit();
    fireEvent.submit(screen.getByLabelText("Email").closest("form")!);
    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher.mock.calls[0][0]).toBe("/api/auth/password-reset");
    expect(JSON.parse(fetcher.mock.calls[0][1]!.body as string)).toEqual({ email: "kate@example.test" });
    await act(async () => pending.resolve(jsonResponse({ status: "pending" }, 202, null)));
    expect(await screen.findByRole("heading", { name: "Проверьте почту" })).toHaveFocus();
    expect(screen.getByRole("status")).toHaveTextContent("Если аккаунт с адресом kate@example.test существует, мы отправили на него письмо");
    expect(document.body.textContent).not.toMatch(/не найден|не существует|заблокирован|отключ/i);
  });

  it("shows Go's field message for a malformed address", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "validation_error", message: "x", requestId: "r", fields: [{ field: "email", message: "Укажите корректный email" }] } }, 422, null));
    render(<ForgotPasswordForm />);
    fireEvent.change(screen.getByLabelText("Email"), { target: { value: "nope" } });
    submit();
    expect(await screen.findByText("Укажите корректный email")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveAttribute("aria-invalid", "true");
  });

  it.each([
    [new BrowserRequestError(429), "Слишком много запросов. Повторите через минуту"],
    [new BrowserRequestError(404, "not_found"), "Восстановление пароля сейчас недоступно"],
    [new BrowserRequestError(500, "internal_error"), "Сервис восстановления недоступен, попробуйте позже"],
    [new BrowserRequestError(), "Сервис восстановления недоступен, попробуйте позже"],
  ])("maps failures to neutral copy (%#)", (error, message) => {
    expect(resetRequestFailure(error)).toEqual({ message });
  });
});
