import { act, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
const { replace, router } = vi.hoisted(() => { const replace = vi.fn(); return { replace, router: { replace } }; });
vi.mock("next/navigation", () => ({ useRouter: () => router }));
import { LegacyHashBridge } from "./legacy-hash-bridge";

afterEach(() => { replace.mockReset(); window.history.replaceState(null, "", "/"); });

describe("hash bridge navigation", () => {
  it.each(["/", "/storefront-preview"])("passes a canonical unprefixed path to Next's basePath-aware router from %s", (base) => {
    window.history.replaceState(null, "", base + "#/cart");
    const { unmount } = render(<LegacyHashBridge />);
    expect(replace).toHaveBeenCalledExactlyOnceWith("/cart");
    act(() => { window.dispatchEvent(new HashChangeEvent("hashchange")); });
    expect(replace).toHaveBeenCalledTimes(1);
    unmount();
    window.history.replaceState(null, "", base + "#/profile");
    act(() => { window.dispatchEvent(new HashChangeEvent("hashchange")); });
    expect(replace).toHaveBeenCalledTimes(1);
  });
  it("ignores normal anchors and handles a later recognized hash", () => {
    window.history.replaceState(null, "", "/#main-content");
    render(<LegacyHashBridge />);
    expect(replace).not.toHaveBeenCalled();
    window.history.replaceState(null, "", "/#/orders");
    act(() => { window.dispatchEvent(new HashChangeEvent("hashchange")); });
    expect(replace).toHaveBeenCalledExactlyOnceWith("/account/orders");
  });
});
