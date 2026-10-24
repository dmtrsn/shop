import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ServerStaffSession } from "@/lib/server-session";

const gate = vi.hoisted(() => ({ session: { status: "anonymous" } as ServerStaffSession }));
const redirect = vi.hoisted(() =>
  vi.fn((target: string) => {
    throw new Error(`redirect:${target}`);
  }),
);

vi.mock("@/lib/server-session", () => ({ getServerStaffSession: async () => gate.session }));
vi.mock("next/navigation", () => ({
  redirect,
  useRouter: () => ({ replace: vi.fn(), refresh: vi.fn(), push: vi.fn() }),
  usePathname: () => "/",
}));

import LoginPage from "./login/page";
import StaffLayout from "./(staff)/layout";
import OverviewPage from "./(staff)/page";

const staff = { id: "stf_" + "f".repeat(32), email: "ops@example.com", displayName: "Оператор" };

describe("server route gates", () => {
  beforeEach(() => {
    redirect.mockClear();
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 502 })));
  });

  it("sends anonymous visitors of staff pages to /login", async () => {
    gate.session = { status: "anonymous" };
    await expect(StaffLayout({ children: <p>secret</p> })).rejects.toThrow("redirect:/login");
  });

  it("shows neither staff content nor a redirect when Go cannot confirm the session", async () => {
    gate.session = { status: "unavailable" };
    render(await StaffLayout({ children: <p>secret</p> }));
    expect(screen.getByRole("heading", { name: "Панель управления недоступна" })).toBeInTheDocument();
    expect(screen.queryByText("secret")).toBeNull();
    expect(redirect).not.toHaveBeenCalled();
  });

  it("renders the shell with the confirmed identity", async () => {
    gate.session = { status: "authenticated", staff };
    render(await StaffLayout({ children: <p>secret</p> }));
    expect(screen.getByText("secret")).toBeInTheDocument();
    expect(screen.getByText("Оператор")).toBeInTheDocument();
  });

  it("moves a signed-in staff member off the sign-in page", async () => {
    gate.session = { status: "authenticated", staff };
    await expect(LoginPage()).rejects.toThrow("redirect:/");
    gate.session = { status: "unavailable" };
    render(await LoginPage());
    expect(screen.getByRole("status")).toHaveTextContent("Не удалось проверить текущий вход");
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
  });

  it("does not invent dashboard metrics", () => {
    const { container } = render(<OverviewPage />);
    expect(screen.getByRole("heading", { name: "Панель управления" })).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/\d|₽|выручк|продаж|остат/i);
  });
});
