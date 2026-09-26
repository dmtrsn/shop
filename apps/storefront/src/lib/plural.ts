export function pluralRu(count: number, [one, few, many]: [string, string, string]): string {
  const modulo100 = count % 100;
  const modulo10 = count % 10;
  if (modulo100 >= 11 && modulo100 <= 14) return many;
  if (modulo10 === 1) return one;
  if (modulo10 >= 2 && modulo10 <= 4) return few;
  return many;
}

export function productCountText(count: number): string {
  return `${count} ${pluralRu(count, ["товар", "товара", "товаров"])}`;
}
