import { render as testingRender, type RenderOptions } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";

import { FavoritesProvider } from "@/test/private-providers";
import type * as FavoritesClient from "@/lib/api/browser-request";

// Presentation tests keep the new private controls in their initial loading
// state. Provider/transport tests separately exercise the real async boundary.
vi.mock("@/lib/api/browser-request", async (importOriginal) => ({
  ...await importOriginal<typeof FavoritesClient>(),
  browserRequest: () => new Promise(() => {}),
}));

export function render(ui: ReactElement, options?: RenderOptions) {
  return testingRender(ui, { wrapper: ({ children }) => <FavoritesProvider>{children}</FavoritesProvider>, ...options });
}
