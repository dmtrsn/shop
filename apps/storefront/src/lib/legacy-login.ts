import "server-only";
import { defaultLegacyLogin, normalizeLegacyLogin } from "./return-to";

type LoginEnvironment = {
  NODE_ENV?: string;
  STOREFRONT_LEGACY_LOGIN_URL?: string;
};

// Only this resolved navigation href may cross the server/client boundary.
// The API origin is deliberately not an input to this configuration.
export function getLegacyLoginHref(environment: LoginEnvironment = process.env): string {
  return normalizeLegacyLogin(environment.STOREFRONT_LEGACY_LOGIN_URL?.trim() || defaultLegacyLogin);
}
