(() => {
  "use strict";

  // Same contract as apps/storefront/src/lib/return-to.ts; shared vectors test both.
  function normalizeReturnTo(candidate, origin) {
    if (typeof candidate !== "string" || candidate.length > 2048 || !candidate.startsWith("/") || candidate.startsWith("//")) return null;
    try {
      const decoded = decodeURIComponent(candidate);
      if (/%[0-9a-f]{2}/i.test(decoded)) return null;
      if ([...candidate + decoded].some(char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127 || char === "\\")) return null;
      const base = new URL(origin);
      const url = new URL(candidate, base.origin);
      const decodedURL = new URL(decoded, base.origin);
      if (!["http:", "https:"].includes(base.protocol) || url.origin !== base.origin || decodedURL.origin !== base.origin) return null;
      if (url.username || url.password || decodedURL.username || decodedURL.password) return null;
      if (url.pathname.startsWith("//") || decodedURL.pathname.startsWith("//")) return null;
      if (/(^|\/)(legacy|login|auth|api|_next)(\/|$)/i.test(decodedURL.pathname)) return null;
      if (/^#\/?(login|profile)(?:[/?]|$)/i.test(decodedURL.hash)) return null;
      for (const key of url.searchParams.keys()) {
        if (/^(returnTo|token|access_token|csrf|csrfToken|password|authorization|userID|expectedUserID)$/i.test(key)) return null;
      }
      return url.pathname + url.search + url.hash;
    } catch {
      return null;
    }
  }

  function returnAfterLogin(location) {
    const params = new URLSearchParams(location.search);
    if (!params.has("returnTo")) return false;
    const values = params.getAll("returnTo");
    const target = values.length === 1 ? normalizeReturnTo(values[0], location.origin) : null;
    location.replace(target ?? "/");
    return true;
  }

  window.SloyMigration = Object.freeze({ normalizeReturnTo, returnAfterLogin });
})();
