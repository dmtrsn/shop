// The emailed link is /register/confirm#t=<token>. A fragment never reaches a
// server, a log or a Referer. The page reads it once and immediately removes
// it from the address bar and the current history entry, so the token is left
// only in component memory: never in storage, cookies or a URL.
const TOKEN = /^[A-Za-z0-9_-]{43}$/;

export function takeRegistrationToken(win: Pick<Window, "location" | "history"> = window): string | null {
  const { hash, pathname, search } = win.location;
  if (!hash) return null;
  // Remove whatever the fragment holds before looking at it.
  win.history.replaceState(null, "", pathname + search);
  const params = new URLSearchParams(hash.slice(1));
  const tokens = params.getAll("t");
  return tokens.length === 1 && TOKEN.test(tokens[0]) ? tokens[0] : null;
}
