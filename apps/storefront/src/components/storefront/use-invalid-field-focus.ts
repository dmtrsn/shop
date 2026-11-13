import { useEffect, useRef } from "react";

export function useInvalidFieldFocus(failure: object | null) {
  const formRef = useRef<HTMLFormElement>(null);

  useEffect(() => {
    if (!failure) return;
    // Errors and their descriptions are now rendered. Query only this form,
    // in DOM order, regardless of the order of backend error keys.
    formRef.current?.querySelector<HTMLInputElement>('input[aria-invalid="true"]')?.focus();
  }, [failure]);

  return formRef;
}
