import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { app_user, db, eq, library_concept, library_document, library_event, pool, sql } from "@neko/db";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";

const embedding = vi.hoisted(() => ({ unavailable: false }));
vi.mock("../../src/embedding", () => ({
  embedText: async () => {
    if (embedding.unavailable) throw new Error("Embedding unavailable");
    return [1, ...Array(383).fill(0)];
  },
  vectorLiteral: (vector: number[]) => `[${vector.join(",")}]`,
}));
import { browseLibrary, editLibraryConcept, listLibraryConcepts, parseLibraryBrowseOptions, readLibraryConcept } from "../../src/work/library";

it("validates bounded paging and filter values", () => {
  expect(parseLibraryBrowseOptions(new URLSearchParams("q=stock"))).toMatchObject({ page: 1, pageSize: 50, sort: "relevance" });
  for (const invalid of ["page=0", "page=-1", "page=1.5", "page=Infinity", "pageSize=101", "view=secret", "layer=other", "sort=bad", "status=bad", `q=${"x".repeat(201)}`]) {
    expect(() => parseLibraryBrowseOptions(new URLSearchParams(invalid))).toThrow();
  }
});

const reachable = await dbReachable();
if (!reachable && process.env.LIBRARY_BROWSE_REQUIRE_DB === "1") throw new Error("Library search requires Postgres in CI");
(reachable ? describe : describe.skip)("paginated Library search against Postgres", () => {
  const orgId = uniqueOrgId("library-browser");
  const otherOrgId = uniqueOrgId("library-browser-other");
  const alice = `${orgId}-alice`;
  const bob = `${orgId}-bob`;
  const reader = { orgId, userId: alice, isAdmin: false };
  const semanticId = randomUUID();
  const hiddenId = randomUUID();
  const draftId = randomUUID();
  const foreignId = randomUUID();
  const archivedId = randomUUID();
  const documentId = randomUUID();
  const teamId = randomUUID();
  const vector = sql`${JSON.stringify([1, ...Array(383).fill(0)])}::vector`;
  const options = (query = "") => parseLibraryBrowseOptions(new URLSearchParams(query));

  beforeAll(async () => {
    await createTestOrg(orgId);
    await createTestOrg(otherOrgId);
    await db().insert(app_user).values([
      { id: alice, org_id: orgId, email: "alice@example.com" },
      { id: bob, org_id: orgId, email: "bob@example.com" },
    ]);
    await db().insert(library_document).values(Array.from({ length: 125 }, (_, index) => ({
      id: index === 124 ? documentId : randomUUID(), org_id: orgId, user_id: alice,
      filename: `Report ${String(index).padStart(3, "0")}.pdf`, relative_path: `library/uploads/${index}.pdf`,
      content_hash: `browser-${index}`, size_bytes: 100, status: "cataloged",
    })));
    await db().insert(library_concept).values(Array.from({ length: 1005 }, (_, index) => ({
      org_id: orgId, user_id: alice, title: `Concept ${String(index).padStart(4, "0")}`, path: `concepts/${index}.md`,
      body: index === 1004 ? "rareword 100% underscore_value" : "Synthetic content", type: "policy", status: "stable",
    })));
    await db().insert(library_concept).values([
      { id: semanticId, org_id: orgId, user_id: alice, title: "Replenishment", path: "semantic.md", type: "playbook", body: "No literal match", status: "stable", embedding: vector, source_document_id: documentId },
      { id: hiddenId, org_id: orgId, user_id: bob, title: "Private", path: "private.md", type: "secret-type", body: "rareword", status: "stable", embedding: vector },
      { id: draftId, org_id: orgId, user_id: null, title: "Pending team", path: "draft.md", type: "review-type", body: "rareword", status: "draft", embedding: vector, promoted_from_id: semanticId },
      { id: foreignId, org_id: otherOrgId, user_id: null, title: "Other organization", path: "foreign.md", type: "foreign-type", body: "rareword", status: "stable", embedding: vector },
      { id: archivedId, org_id: orgId, user_id: alice, title: "Archived", path: "archived.md", type: "archived-type", body: "rareword", status: "stable", archived_at: new Date(), embedding: vector },
      { id: teamId, org_id: orgId, user_id: null, title: "Team knowledge", path: "team.md", type: "policy", body: "Synthetic team", status: "stable" },
    ]);
  }, 30_000);
  afterAll(async () => {
    await deleteTestOrg(orgId);
    await deleteTestOrg(otherOrgId);
    await pool().end();
  });

  it("pages beyond the old caps, reports real totals and omits concept bodies", async () => {
    const first = await browseLibrary(reader, options("view=concepts&sort=name"));
    const last = await browseLibrary(reader, options("view=concepts&sort=name&page=21"));
    expect(first.counts).toEqual({ documents: 125, concepts: 1007, review: 0 });
    expect(first.concepts).toHaveLength(50);
    expect(last.concepts).toHaveLength(7);
    expect(new Set([...first.concepts, ...last.concepts].map(row => row.id)).size).toBe(57);
    expect(first.concepts[0]).not.toHaveProperty("body");
    expect(first.types).toEqual(["playbook", "policy"]);
    const documents = await browseLibrary(reader, options("view=documents&page=3&sort=name"));
    expect(documents.documents).toHaveLength(25);
    expect(documents.total).toBe(125);
    expect((await browseLibrary(reader, options("view=documents&page=999"))).page).toBe(3);
    expect(await listLibraryConcepts({ orgId, userId: alice, limit: null })).toHaveLength(1006);
    expect((await browseLibrary(reader, options(`view=concepts&documentId=${documentId}`))).concepts.map(row => row.id)).toEqual([semanticId]);
  });

  it("combines semantic matches with exact terms outside the first 1000 rows", async () => {
    const results = await browseLibrary(reader, options("view=concepts&q=rareword"));
    expect(results.searchMode).toBe("hybrid");
    expect(results.total).toBe(2);
    expect(results.concepts.some(row => row.id === semanticId)).toBe(true);
    expect(results.concepts.some(row => row.title === "Concept 1004")).toBe(true);
    const documents = await browseLibrary(reader, options("q=inventory"));
    expect(documents.documents.map(row => row.id)).toEqual([documentId]);
  });

  it("does not leak personal, foreign, archived or unapproved team content through results, counts, facets or detail", async () => {
    for (const id of [hiddenId, foreignId, archivedId, draftId]) expect(await readLibraryConcept(reader, id)).toBeNull();
    const admin = { ...reader, isAdmin: true };
    expect(await readLibraryConcept(admin, hiddenId)).toBeNull();
    expect(await readLibraryConcept(admin, draftId)).not.toBeNull();
    const review = await browseLibrary(admin, options("view=review"));
    expect(review.concepts.map(row => row.id)).toEqual([draftId]);
    expect(review.counts.review).toBe(1);
    const team = await browseLibrary(reader, options("view=concepts&layer=team"));
    expect(team.total).toBe(1);
    expect(team.concepts[0].title).toBe("Team knowledge");
    expect(team.concepts[0].sharing).toBe("shared");
    expect(review.concepts[0].sharing).toBe("awaiting_approval");
    const mine = await browseLibrary(reader, options(`view=concepts&documentId=${documentId}`));
    expect(mine.concepts[0].sharing).toBe("awaiting_approval");
  });

  it("keeps literal search working without embeddings, including wildcard characters and empty results", async () => {
    embedding.unavailable = true;
    try {
      const results = await browseLibrary(reader, options("view=concepts&q=100%25+underscore_value"));
      expect(results.searchMode).toBe("keyword");
      expect(results.total).toBe(1);
      expect(results.concepts[0].title).toBe("Concept 1004");
      expect((await browseLibrary(reader, options("view=concepts&q=nonexistent"))).total).toBe(0);
      expect((await browseLibrary(reader, options("q=Report+124"))).documents[0].id).toBe(documentId);
    } finally { embedding.unavailable = false; }
  });

  it("edits only owned or admin team concepts, preserves lineage and detects concurrent revisions", async () => {
    const admin = { ...reader, isAdmin: true };
    const original = (await readLibraryConcept(reader, semanticId))!;
    const input = { id: semanticId, title: "Updated replenishment", description: "Human correction", type: original.type, body: "Corrected knowledge", updatedAt: original.updatedAt };
    expect((await editLibraryConcept({ ...reader, userId: bob }, input)).status).toBe("not_found");
    expect((await editLibraryConcept({ ...admin, userId: bob }, input)).status).toBe("not_found");
    const saved = await editLibraryConcept(reader, input);
    expect(saved.status).toBe("saved");
    if (saved.status !== "saved") throw new Error("Expected saved concept");
    expect(saved.concept.sourceDocumentId).toBe(documentId);
    expect(saved.concept.path).toBe(original.path);
    expect(saved.searchIndexed).toBe(false);
    expect((await editLibraryConcept(reader, input)).status).toBe("conflict");
    const events = await db().select().from(library_event).where(eq(library_event.concept_id, semanticId));
    expect(events.some(event => event.action === "concept_edited" && event.user_id === alice)).toBe(true);
    const team = (await readLibraryConcept(reader, teamId))!;
    const teamInput = { ...input, id: teamId, updatedAt: team.updatedAt };
    expect((await editLibraryConcept(reader, teamInput)).status).toBe("not_found");
    const teamSaved = await editLibraryConcept(admin, teamInput);
    expect(teamSaved.status).toBe("saved");
    if (teamSaved.status === "saved") {
      expect(teamSaved.concept.status).toBe("stable");
      expect(teamSaved.concept.verified[0].by).toBe(`human:${alice}`);
    }
    const draft = (await readLibraryConcept(admin, draftId))!;
    const draftSaved = await editLibraryConcept(admin, { ...input, id: draftId, updatedAt: draft.updatedAt });
    if (draftSaved.status !== "saved") throw new Error("Expected saved draft");
    expect(draftSaved.concept.status).toBe("draft");
    expect(draftSaved.concept.verified).toEqual([]);
    embedding.unavailable = true;
    try {
      const fallback = await editLibraryConcept(reader, { ...input, title: "Fresh fallback wording", updatedAt: saved.concept.updatedAt });
      expect(fallback.status).toBe("saved");
      if (fallback.status === "saved") expect(fallback.searchIndexed).toBe(false);
      const rows = await db().select({ embedding: library_concept.embedding }).from(library_concept).where(eq(library_concept.id, semanticId));
      expect(rows[0].embedding).toBeNull();
      expect((await browseLibrary(reader, options("view=concepts&q=Fresh+fallback"))).total).toBe(1);
    } finally { embedding.unavailable = false; }
  });
});
