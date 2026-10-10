// @vitest-environment node
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { afterEach, describe, expect, it, vi } from "vitest";

type Browser = Window & typeof globalThis & {
  SloyMigration: { returnAfterLogin: (location: Pick<Location, "search" | "origin" | "replace">) => boolean };
};
const { JSDOM } = createRequire(import.meta.url)("jsdom") as {
  JSDOM: new (html: string, options: object) => { window: Browser };
};
const source = (file: string) => readFileSync(new URL(`../../../../web/${file}`, import.meta.url), "utf8");
const windows: Browser[] = [];
afterEach(() => { for (const window of windows.splice(0)) window.close(); });

async function boot(search: string, authenticated = false) {
  const { window } = new JSDOM(source("index.html"), {
    url: `https://shop.test/legacy/${search}#/login`, runScripts: "outside-only",
  });
  windows.push(window);
  const replace = vi.fn();
  let failLogin = false;
  const session = { authenticated: true, user: { id: "test-user", name: "Test" }, csrfToken: "test-csrf" };
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    const login = url === "/api/auth/session" && options?.method === "POST";
    const failed = login && failLogin;
    const body = failed ? { error: { message: "credential detail must not reach the UI" } }
      : url === "/api/auth/session" ? (login || authenticated ? session : { authenticated: false })
      : url === "/api/me" ? session.user : { items: [] };
    return { ok: !failed, status: failed ? 401 : 200, json: async () => body } as Response;
  });
  window.fetch = fetch as typeof window.fetch;
  window.scrollTo = vi.fn();
  window.eval(source("migration.js"));
  const consume = window.SloyMigration.returnAfterLogin;
  window.SloyMigration = { returnAfterLogin: () => consume({ search: window.location.search, origin: window.location.origin, replace }) };
  window.eval(source("app.js"));
  await vi.waitFor(() => expect(fetch.mock.calls.some(([url]) => url === "/api/auth/session")).toBe(true));
  // Let the actual app finish its initial render before dispatching a form event.
  await new Promise(resolve => setTimeout(resolve, 0));
  const submit = () => {
    window.document.querySelector<HTMLInputElement>("#login-email")!.value = "test@example.invalid";
    window.document.querySelector<HTMLInputElement>("#login-password")!.value = "unit-test-only";
    window.document.querySelector("#login-form")!.dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
  };
  return { window, replace, fetch, submit, fail: (value: boolean) => { failLogin = value; } };
}

describe("actual legacy cookie-login hooks", () => {
  it("keeps a failed login and its target, then replaces only after a successful retry", async () => {
    const search = "?returnTo=%2Fstorefront-preview%2Fcheckout%3Fstep%3Dreview";
    const app = await boot(search);
    app.fail(true);
    app.submit();
    await vi.waitFor(() => expect(app.window.document.querySelector<HTMLElement>("#login-error")!.hidden).toBe(false));
    expect(app.replace).not.toHaveBeenCalled();
    expect(app.window.location.search).toBe(search);
    expect(app.window.document.querySelector("#login-error")!.textContent).toBe("Не удалось войти. Проверьте, что API запущен, и повторите.");
    app.fail(false);
    app.submit();
    await vi.waitFor(() => expect(app.replace).toHaveBeenCalledExactlyOnceWith("/storefront-preview/checkout?step=review"));
    expect(app.fetch.mock.calls.some(([url]) => url === "/api/me")).toBe(false);
    expect(app.fetch.mock.calls.filter(([url, options]) => url === "/api/auth/session" && options?.method === "POST")
      .every(([, options]) => options?.credentials === "same-origin")).toBe(true);
  });
  it("preserves the original account-data flow without returnTo", async () => {
    const app = await boot("");
    app.submit();
    await vi.waitFor(() => expect(app.fetch.mock.calls.some(([url]) => url === "/api/me")).toBe(true));
    expect(app.replace).not.toHaveBeenCalled();
  });
  it("returns an already authenticated entry only after real session discovery", async () => {
    const app = await boot("?returnTo=%2Fstorefront-preview%2Fcart", true);
    expect(app.replace).toHaveBeenCalledExactlyOnceWith("/storefront-preview/cart");
    expect(app.fetch.mock.calls.some(([url, options]) => url === "/api/auth/session" && options?.method === "POST")).toBe(false);
  });
  it("does not treat stored UI data as authentication", async () => {
    const app = await boot("?returnTo=%2Fcart");
    expect(app.replace).not.toHaveBeenCalled();
  });
  it("cannot leave the origin on a malicious successful-login target", async () => {
    const app = await boot("?returnTo=%2Fx%2F..%2F%2Fevil.example");
    app.submit();
    await vi.waitFor(() => expect(app.replace).toHaveBeenCalledExactlyOnceWith("/"));
  });
});
