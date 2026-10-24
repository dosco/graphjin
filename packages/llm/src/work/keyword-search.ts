import { sql } from "@neko/db";

type SQL = ReturnType<typeof sql>;

/**
 * A Postgres full-text query that matches any of the query's words (English
 * stemming, stop words dropped). plainto_tsquery joins words with AND, which
 * misses most natural-language questions, so the ANDs become ORs and ranking
 * (ts_rank_cd) puts rows that match more words first. A query of only stop
 * words matches nothing.
 */
export function keywordTsQuery(query: string): SQL {
  return sql`nullif(replace(plainto_tsquery('english', ${query})::text, ' & ', ' | '), '')::tsquery`;
}

/**
 * Merge ranked semantic results with keyword matches among rows that have no
 * vector yet (the backfill has not reached them): alternate, starting with the
 * semantic list, drop duplicates, and stop at the limit. With no unindexed
 * matches the semantic order is unchanged.
 */
export function interleaveResults<T>(
  semantic: ReadonlyArray<T>,
  unindexed: ReadonlyArray<T>,
  key: (item: T) => string,
  limit: number,
): T[] {
  if (unindexed.length === 0) return semantic.slice(0, limit);
  const out: T[] = [];
  const seen = new Set<string>();
  const push = (item: T | undefined) => {
    if (item === undefined || out.length >= limit) return;
    const id = key(item);
    if (seen.has(id)) return;
    seen.add(id);
    out.push(item);
  };
  for (let i = 0; i < Math.max(semantic.length, unindexed.length) && out.length < limit; i++) {
    push(semantic[i]);
    push(unindexed[i]);
  }
  return out;
}
