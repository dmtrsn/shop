// Avatar initials from the real account name only: first letter of the first
// and last words, or of the only word. Empty or whitespace names give "".
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (!words.length) return "";
  const first = (word: string) => Array.from(word)[0] ?? "";
  const letters = words.length === 1 ? first(words[0]) : first(words[0]) + first(words[words.length - 1]);
  return letters.toLocaleUpperCase("ru-RU");
}
