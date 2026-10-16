import { afterEach, describe, expect, it, vi } from "vitest";

import { takeRegistrationToken } from "./registration-token";

const token = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-ABCDE";

afterEach(() => { window.history.replaceState(null, "", "/"); vi.restoreAllMocks(); });

describe("takeRegistrationToken", () => {
  it("returns the fragment token and removes it from the URL and current history entry first", () => {
    window.history.replaceState(null, "", `/register/confirm?x=1#t=${token}`);
    const length = window.history.length;
    const replace = vi.spyOn(window.history, "replaceState");
    expect(takeRegistrationToken()).toBe(token);
    expect(replace).toHaveBeenCalledExactlyOnceWith(null, "", "/register/confirm?x=1");
    expect(window.location.href).not.toContain(token);
    expect(window.location.hash).toBe("");
    expect(window.history.length).toBe(length);
    for (const storage of [localStorage, sessionStorage]) expect(JSON.stringify({ ...storage })).not.toContain(token);
  });

  it.each([
    ["no fragment", ""], ["empty token", "#t="], ["short", `#t=${token.slice(1)}`], ["long", `#t=${token}A`],
    ["illegal characters", `#t=${token.slice(1)}+`], ["two tokens", `#t=${token}&t=${token}`], ["other key", `#token=${token}`],
  ])("returns null for %s, and still clears any fragment", (_, hash) => {
    window.history.replaceState(null, "", `/register/confirm${hash}`);
    expect(takeRegistrationToken()).toBeNull();
    expect(window.location.hash).toBe("");
  });
});
