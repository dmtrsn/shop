import type { ReactNode } from "react";

import { BrowserSessionProvider } from "@/components/storefront/browser-session-provider";
import { CartProvider } from "@/components/storefront/cart-provider";
import { FavoritesProvider as FavoritesFeatureProvider } from "@/components/storefront/favorites-provider";

export { useFavorites, type FavoritesRouteSession } from "@/components/storefront/favorites-provider";

// Same composition as the root layout: one shared browser session and two
// independent lazy private features.
export function PrivateProviders({ children, loginHref }: { children: ReactNode; loginHref: string | null }) {
  return (
    <BrowserSessionProvider>
      <FavoritesFeatureProvider loginHref={loginHref}>
        <CartProvider loginHref={loginHref}>{children}</CartProvider>
      </FavoritesFeatureProvider>
    </BrowserSessionProvider>
  );
}

// Existing Favorites tests render through the full private composition.
export const FavoritesProvider = PrivateProviders;
