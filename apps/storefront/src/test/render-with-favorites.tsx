import { render as testingRender, type RenderOptions } from "@testing-library/react";
import type { ReactElement } from "react";
import { vi } from "vitest";

import { FavoritesProvider } from "@/components/storefront/favorites-provider";
import type * as FavoritesClient from "@/lib/api/favorites-client";

// Presentation tests keep the new private controls in their initial loading
// state. Provider/transport tests separately exercise the real async boundary.
vi.mock("@/lib/api/favorites-client", async (importOriginal) => ({
  ...await importOriginal<typeof FavoritesClient>(),
  getBrowserSession: () => new Promise(() => {}),
}));

export function render(ui: ReactElement, options?: RenderOptions) {
  return testingRender(ui, { wrapper: ({ children }) => <FavoritesProvider loginHref={null}>{children}</FavoritesProvider>, ...options });
}
