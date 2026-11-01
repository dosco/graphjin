function normalize(value: string): string {
  return value
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase();
}

function withinOneEdit(a: string, b: string): boolean {
  if (Math.abs(a.length - b.length) > 1) return false;
  let edits = 0;
  let left = 0;
  let right = 0;
  while (left < a.length && right < b.length) {
    if (a[left] === b[right]) {
      left += 1;
      right += 1;
      continue;
    }
    if (++edits > 1) return false;
    if (a.length >= b.length) left += 1;
    if (b.length >= a.length) right += 1;
  }
  return true;
}

export function matchesListSearch(query: string, ...values: unknown[]): boolean {
  const terms = normalize(query).trim().split(/\s+/).filter(Boolean);
  if (terms.length === 0) return true;
  const text = normalize(values.filter(Boolean).join(" "));
  const words = text.split(/[^a-z0-9]+/).filter(Boolean);
  return terms.every(
    (term) =>
      text.includes(term) ||
      (term.length >= 4 && words.some((word) => withinOneEdit(term, word))),
  );
}
