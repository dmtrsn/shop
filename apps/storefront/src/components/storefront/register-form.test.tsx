import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { deferred, jsonResponse } from "@/test/favorites-fixtures";

import { RegisterForm, registrationFailure } from "./register-form";
import { BrowserRequestError } from "@/lib/api/browser-request";

const fetcher = vi.fn<typeof fetch>();
const password = "unit-test-only-1";
const posts = () => fetcher.mock.calls.filter(([, init]) => init?.method === "POST");

function fill({ name = "Анна", email = "ann@example.test", secret = password, phone = "" } = {}) {
  fireEvent.change(screen.getByLabelText("Имя"), { target: { value: name } });
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: email } });
  fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: secret } });
  fireEvent.change(screen.getByLabelText("Телефон (необязательно)"), { target: { value: phone } });
}
const submit = () => fireEvent.click(screen.getByRole("button", { name: "Создать аккаунт" }));

beforeEach(() => {
  fetcher.mockReset();
  vi.stubGlobal("fetch", fetcher);
  localStorage.clear();
  sessionStorage.clear();
});
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers(); });

describe("register form", () => {
  it("uses real labels and password-manager autocomplete, and allows paste", () => {
    render(<RegisterForm />);
    expect(screen.getByLabelText("Имя")).toHaveAttribute("autocomplete", "name");
    expect(screen.getByLabelText("Email")).toHaveAttribute("autocomplete", "email");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("autocomplete", "new-password");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("type", "password");
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("aria-describedby", "register-password-hint");
    expect(screen.getByLabelText("Телефон (необязательно)")).toHaveAttribute("autocomplete", "tel");
    expect(screen.getByLabelText("Пароль")).not.toHaveAttribute("onpaste");
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login");
    for (const field of ["Имя", "Email", "Пароль"]) expect(screen.getByLabelText(field)).toHaveValue("");
  });

  it("asks for required fields without sending anything", () => {
    render(<RegisterForm />);
    submit();
    expect(screen.getByText("Укажите имя")).toBeInTheDocument();
    expect(screen.getByText("Укажите email")).toBeInTheDocument();
    expect(screen.getByText("Придумайте пароль")).toBeInTheDocument();
    expect(screen.getByLabelText("Имя")).toHaveAttribute("aria-invalid", "true");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("posts once, then shows the same check-email state for every address and forgets the password", async () => {
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    render(<RegisterForm />);
    fill({ email: " ann@example.test " });
    submit();
    fireEvent.submit(screen.getByLabelText("Имя").closest("form")!);
    expect(screen.getByRole("button", { name: "Отправляем…" })).toBeDisabled();
    expect(posts()).toHaveLength(1);
    expect(JSON.parse(posts()[0][1]!.body as string)).toEqual({ name: "Анна", email: "ann@example.test", password });
    await act(async () => pending.resolve(jsonResponse({ status: "pending" }, 202, null)));
    const heading = await screen.findByRole("heading", { name: "Проверьте почту" });
    expect(heading).toHaveFocus();
    expect(screen.getByRole("status")).toHaveTextContent("ann@example.test");
    expect(screen.getByRole("status")).toHaveTextContent("Если аккаунт с этим адресом уже есть, в письме будет ссылка для входа");
    expect(document.body.innerHTML).not.toContain(password);
    for (const storage of [localStorage, sessionStorage]) expect(JSON.stringify({ ...storage })).not.toContain(password);
    expect(screen.queryByText(/уже зарегистрирован|новый аккаунт создан/i)).toBeNull();
  });

  it("shows Go's field errors next to the fields", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse({ error: { code: "validation_error", message: "x", requestId: "r",
      fields: [{ field: "password", message: "Пароль должен быть не короче 10 символов" }, { field: "phone", message: "Телефон цифрами" }] } }, 422, null));
    render(<RegisterForm />);
    fill({ secret: "short", phone: "abc" });
    submit();
    expect(await screen.findByText("Пароль должен быть не короче 10 символов")).toBeInTheDocument();
    expect(screen.getByLabelText("Пароль")).toHaveAttribute("aria-describedby", "register-password-hint register-password-error");
    expect(screen.getByText("Телефон цифрами")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Создать аккаунт" })).toBeEnabled();
  });

  it.each([
    [new BrowserRequestError(503, "service_unavailable"), "Сервис перегружен, повторите через несколько секунд"],
    [new BrowserRequestError(429), "Слишком много попыток. Повторите через минуту"],
    [new BrowserRequestError(404, "not_found"), "Регистрация сейчас недоступна"],
    [new BrowserRequestError(403, "csrf_failed"), "Не удалось отправить: обновите страницу"],
    [new BrowserRequestError(500, "internal_error"), "Сервис регистрации недоступен, попробуйте позже"],
    [new BrowserRequestError(), "Сервис регистрации недоступен, попробуйте позже"],
  ])("maps failures to neutral copy (%#)", (error, message) => {
    expect(registrationFailure(error)).toEqual({ message, fields: {} });
  });

  it("offers a resend after the cooldown with copy that is true in every case", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    fetcher.mockResolvedValueOnce(jsonResponse({ status: "pending" }, 202, null));
    render(<RegisterForm />);
    fill();
    submit();
    await screen.findByRole("heading", { name: "Проверьте почту" });
    const button = screen.getByRole("button", { name: /Отправить ещё раз можно через 60 с/ });
    expect(button).toBeDisabled();
    for (let second = 0; second < 61; second++) await act(async () => { vi.advanceTimersByTime(1000); });
    fetcher.mockResolvedValueOnce(jsonResponse({ status: "pending" }, 202, null));
    fireEvent.click(screen.getByRole("button", { name: "Отправить письмо ещё раз" }));
    await waitFor(() => expect(posts()).toHaveLength(2));
    expect(posts()[1][0]).toBe("/api/auth/registration/resend");
    expect(JSON.parse(posts()[1][1]!.body as string)).toEqual({ email: "ann@example.test" });
    expect(await screen.findByText(/Если регистрация ждёт подтверждения, мы отправили новое письмо/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Отправить ещё раз можно через/ })).toBeDisabled();
  });

  it("returns to the form to change the address, without the password", async () => {
    fetcher.mockResolvedValueOnce(jsonResponse({ status: "pending" }, 202, null));
    render(<RegisterForm />);
    fill();
    submit();
    fireEvent.click(await screen.findByRole("button", { name: "Указать другой адрес" }));
    expect(screen.getByLabelText("Email")).toHaveValue("ann@example.test");
    expect(screen.getByLabelText("Пароль")).toHaveValue("");
  });
});
