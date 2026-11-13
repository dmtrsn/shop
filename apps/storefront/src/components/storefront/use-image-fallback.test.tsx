import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useImageFallback } from "./use-image-fallback";

describe("useImageFallback error handlers", () => {
  it("does not skip the original or retry sources when the same error is delivered repeatedly", () => {
    const { result } = renderHook(() => useImageFallback(["/thumb.webp", "/original.webp"]));
    const thumbnailError = result.current.onError;

    act(() => {
      thumbnailError();
      thumbnailError();
    });
    expect(result.current.src).toBe("/original.webp");
    act(() => thumbnailError());
    expect(result.current.src).toBe("/original.webp");

    const originalError = result.current.onError;
    act(() => originalError());
    expect(result.current.src).toBeUndefined();
    act(() => {
      thumbnailError();
      originalError();
    });
    expect(result.current.src).toBeUndefined();
  });

  it("ignores an old sequence's error handler and starts fresh when returning to that sequence", () => {
    const first = ["/thumb-a.webp", "/original-a.webp"];
    const second = ["/thumb-b.webp", "/original-b.webp"];
    const { result, rerender } = renderHook(({ sources }) => useImageFallback(sources), {
      initialProps: { sources: first },
    });
    const oldError = result.current.onError;
    act(() => result.current.onError());
    act(() => result.current.onError());
    expect(result.current.src).toBeUndefined();

    rerender({ sources: second });
    act(() => oldError());
    expect(result.current.src).toBe(second[0]);

    rerender({ sources: first });
    expect(result.current.src).toBe(first[0]);
  });
});
