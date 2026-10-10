// Full-document navigation that replaces the current history entry. Unlike a
// router transition it discards every in-memory client state with the page.
export function replaceDocument(href: string): void {
  window.location.replace(href);
}
