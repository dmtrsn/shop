import { useEffect, useRef, useState } from "react";

/**
 * Tries each distinct non-empty source in order. A load error moves to the next
 * one, and after the last `src` is undefined, so the caller shows its
 * placeholder. Small surfaces pass [thumbnail, original]: a missing thumbnail
 * file or stale metadata still shows the original (ADR 022). A changed candidate
 * sequence starts fresh; unchanged candidates retain their failures.
 */
export function useImageFallback(sources: ReadonlyArray<string | undefined>) {
  const candidates = [...new Set(sources.filter((source): source is string => Boolean(source)))];
  const identity = JSON.stringify(candidates);
  const [fallback, setFallback] = useState({ identity, failures: 0 });
  const failures = fallback.identity === identity ? fallback.failures : 0;
  // Reset during render so the new sequence never commits an old fallback index.
  if (fallback.identity !== identity) setFallback({ identity, failures: 0 });
  const ref = useRef<HTMLImageElement>(null);
  const src = candidates[failures];
  // Ignore old handlers and repeated errors for a source we have already left.
  const onError = () => setFallback((current) =>
    current.identity === identity && current.failures === failures
      ? { identity, failures: failures + 1 }
      : current,
  );

  // A server-rendered image can fail before hydration attaches onError.
  useEffect(() => {
    const element = ref.current;
    if (element?.complete && element.naturalWidth === 0) onError();
  }, [src]);

  return { src, ref, onError };
}
