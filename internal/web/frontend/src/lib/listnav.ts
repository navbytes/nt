// stepId moves a roving focus cursor through an ordered list of row ids. It's the
// pure core of j/k list navigation, factored out so the index math is unit-tested
// without a DOM. From "nothing focused" (null), j (dir +1) lands on the first row
// and k (dir -1) on the last; from a known row it clamps at the ends (no wrap), so
// holding j parks you on the last task instead of cycling.
export function stepId(ids: string[], currentId: string | null, dir: 1 | -1): string | null {
  if (ids.length === 0) return null;
  const i = currentId ? ids.indexOf(currentId) : -1;
  if (i < 0) return dir === 1 ? ids[0]! : ids[ids.length - 1]!;
  const n = Math.min(Math.max(i + dir, 0), ids.length - 1);
  return ids[n]!;
}
