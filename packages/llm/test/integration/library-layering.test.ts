// Integration tests for the library domain: layered visibility (team vs
// personal), the share → approve flow, and pgvector search shape.
// Embeddings are mocked (same anchors as the memory search suite); a
// real Postgres exercises the Drizzle column types and SQL.

import { afterAll, beforeEach, describe, expect, it, vi } from "vitest";
import { app_user, db, eq, library_document, pool } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";

vi.mock("../../src/embedding", async () => {
  const EMBEDDING_DIM = 384;
  return {
    EMBEDDING_DIM,
    embedText: vi.fn(async (text: string) => {
      const seed = text
        .split("")
        .reduce((acc, c) => (acc * 31 + c.charCodeAt(0)) >>> 0, 7);
      const v = new Array<number>(EMBEDDING_DIM);
      for (let i = 0; i < EMBEDDING_DIM; i++) {
        v[i] = Math.sin(seed + i) * 0.1;
      }
      return v;
    }),
    vectorLiteral: (vec: number[]) => `[${vec.join(",")}]`,
  };
});

import {
  createLibraryDocument,
  completeLibraryExtraction,
  decideLibraryConcept,
  listLibraryTransientCleanupCandidates,
  listLibraryConcepts,
  listStaleUploadedLibraryDocuments,
  markLibraryDocumentStatus,
  searchLibraryByContext,
  shareLibraryConceptToTeam,
  sweepStaleLibraryConcepts,
  upsertLibraryConcept,
} from "../../src/work/library";
import { runEmbeddingIndexJob } from "../../src/embedding-jobs";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[library-layering] skipping: Postgres unreachable.");
}

describeIfDb("library layering and search", () => {
  afterAll(async () => {
    await pool().end();
  });

  let orgId: string;
  const ALICE = "user-alice";
  const BOB = "user-bob";

  beforeEach(async () => {
    orgId = uniqueOrgId("lib");
    await createTestOrg(orgId, "Library Test");
    await db()
      .insert(app_user)
      .values([
        { id: `${orgId}-${ALICE}`, email: "alice@example.com", org_id: orgId },
        { id: `${orgId}-${BOB}`, email: "bob@example.com", org_id: orgId },
      ]);
  });

  const alice = () => `${orgId}-${ALICE}`;
  const bob = () => `${orgId}-${BOB}`;

  it("cleans completed transient state immediately but retains recent failures", async () => {
    try {
      const makeDocument = async (suffix: string) => {
        const created = await createLibraryDocument({
          orgId,
          userId: alice(),
          filename: `${suffix}.pdf`,
          relativePath: `library/uploads/${alice()}/${suffix}.pdf`,
          contentHash: `hash-${suffix}`,
          sizeBytes: 100,
        });
        await completeLibraryExtraction({
          orgId,
          id: created.document.id,
          relativePath: `library/derived/${created.document.id}/fingerprint/hash.md`,
          contentHash: `extracted-${suffix}`,
          extractorFingerprint: "test-extractor",
        });
        return created.document.id;
      };
      const completedId = await makeDocument("completed");
      const recentFailureId = await makeDocument("recent-failure");
      const oldFailureId = await makeDocument("old-failure");
      await markLibraryDocumentStatus({ orgId, id: completedId, status: "cataloged" });
      await markLibraryDocumentStatus({ orgId, id: recentFailureId, status: "failed" });
      await markLibraryDocumentStatus({ orgId, id: oldFailureId, status: "failed" });
      await db()
        .update(library_document)
        .set({ updated_at: new Date(Date.now() - 8 * 24 * 60 * 60 * 1_000) })
        .where(eq(library_document.id, oldFailureId));

      const candidates = await listLibraryTransientCleanupCandidates(
        new Date(Date.now() - 7 * 24 * 60 * 60 * 1_000),
      );
      const ids = candidates.map((candidate) => candidate.documentId);
      expect(ids).toContain(completedId);
      expect(ids).toContain(oldFailureId);
      expect(ids).not.toContain(recentFailureId);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("finds uploaded rows stranded before queue admission", async () => {
    try {
      const { document } = await createLibraryDocument({
        orgId,
        userId: alice(),
        filename: "stranded.md",
        relativePath: `library/uploads/${alice()}/stranded.md`,
        contentHash: "hash-stranded-upload",
        sizeBytes: 100,
      });
      await db()
        .update(library_document)
        .set({ updated_at: new Date(Date.now() - 6 * 60 * 1_000) })
        .where(eq(library_document.id, document.id));

      const stale = await listStaleUploadedLibraryDocuments(
        new Date(Date.now() - 5 * 60 * 1_000),
      );
      expect(stale).toContainEqual({ orgId, documentId: document.id });
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("keeps personal concepts invisible to other members and to the team layer", async () => {
    try {
      const { concept } = await upsertLibraryConcept({
        orgId,
        userId: alice(),
        path: "policies/refund-policy.md",
        type: "Policy",
        title: "Refund policy",
        body: "Refunds within 30 days need CFO approval over $500.",
      });
      await runEmbeddingIndexJob({ kind: "concept", orgId, id: concept.id });

      const aliceView = await searchLibraryByContext({
        orgId,
        userId: alice(),
        query: "refund policy approvals",
      });
      expect(aliceView).toHaveLength(1);
      expect(aliceView[0].layer).toBe("personal");

      const bobView = await searchLibraryByContext({
        orgId,
        userId: bob(),
        query: "refund policy approvals",
      });
      expect(bobView).toEqual([]);

      const teamOnly = await searchLibraryByContext({
        orgId,
        userId: null,
        query: "refund policy approvals",
      });
      expect(teamOnly).toEqual([]);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("finds a code however the text punctuates it", async () => {
    try {
      for (const [path, body] of [
        ["materials/iteq-alternates.md", "Iteq IT-180A alternates: Isola FR370HR, Ventec VT-47."],
        ["materials/other-alternates.md", "Row 173 GIA-671(N), Polyimide: alternates EMC/Arlon 37N, Isola FR406N."],
      ]) {
        const { concept } = await upsertLibraryConcept({ orgId, userId: alice(), path, type: "Reference", title: path, body });
        await runEmbeddingIndexJob({ kind: "concept", orgId, id: concept.id });
      }
      const results = await searchLibraryByContext({ orgId, userId: alice(), query: "671N alternates" });
      expect(results[0]?.concept.path).toBe("materials/other-alternates.md");
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("share → approve promotes a concept into everyone's view with a human verification", async () => {
    try {
      const { concept: personal } = await upsertLibraryConcept({
        orgId,
        userId: alice(),
        path: "policies/refund-policy.md",
        type: "Policy",
        title: "Refund policy",
        body: "Refunds within 30 days.",
      });

      const draft = await shareLibraryConceptToTeam({
        orgId,
        id: personal.id,
        sharedBy: alice(),
      });
      expect(draft.userId).toBeNull();
      expect(draft.status).toBe("draft");
      expect(draft.promotedFromId).toBe(personal.id);

      const approved = await decideLibraryConcept({
        orgId,
        id: draft.id,
        action: "approve",
        decidedBy: alice(),
      });
      expect(approved.status).toBe("stable");
      expect(approved.verified.at(-1)?.by).toBe(`human:${alice()}`);
      await runEmbeddingIndexJob({ kind: "concept", orgId, id: draft.id });

      const bobView = await searchLibraryByContext({
        orgId,
        userId: bob(),
        query: "refund policy",
      });
      expect(bobView.some((r) => r.concept.id === draft.id && r.layer === "team")).toBe(
        true,
      );
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("decline archives the draft and double-decisions are rejected", async () => {
    try {
      const { concept: personal } = await upsertLibraryConcept({
        orgId,
        userId: alice(),
        path: "notes/scratch.md",
        type: "Notes",
        title: "Scratch",
        body: "Not for the team.",
      });
      const draft = await shareLibraryConceptToTeam({
        orgId,
        id: personal.id,
        sharedBy: alice(),
      });
      const declined = await decideLibraryConcept({
        orgId,
        id: draft.id,
        action: "decline",
        decidedBy: alice(),
      });
      expect(declined.archivedAt).not.toBeNull();

      await expect(
        decideLibraryConcept({ orgId, id: draft.id, action: "approve", decidedBy: alice() }),
      ).rejects.toThrow(/not found/i);

      const team = await listLibraryConcepts({ orgId, userId: null });
      expect(team.find((c) => c.id === draft.id)).toBeUndefined();
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("upsert revises the existing concept at a path instead of duplicating", async () => {
    try {
      const first = await upsertLibraryConcept({
        orgId,
        userId: alice(),
        path: "policies/refund-policy.md",
        type: "Policy",
        title: "Refund policy",
        body: "v1",
        sources: [{ resource: "/uploads/t/a.md" }],
      });
      expect(first.created).toBe(true);
      const second = await upsertLibraryConcept({
        orgId,
        userId: alice(),
        path: "policies/refund-policy.md",
        type: "Policy",
        title: "Refund policy (updated)",
        body: "v2",
        sources: [{ resource: "/uploads/t/b.md" }],
      });
      expect(second.created).toBe(false);
      expect(second.concept.id).toBe(first.concept.id);
      expect(second.concept.body).toBe("v2");
      expect(second.concept.sources.map((s) => s.resource).sort()).toEqual([
        "/uploads/t/a.md",
        "/uploads/t/b.md",
      ]);

      const list = await listLibraryConcepts({ orgId, userId: alice() });
      expect(list).toHaveLength(1);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("staleness sweep deprecates expired stable concepts and search drops them", async () => {
    try {
      const { concept } = await upsertLibraryConcept({
        orgId,
        userId: null,
        path: "contracts/expired-msa.md",
        type: "Contract",
        title: "Expired MSA",
        body: "This agreement ended last year.",
        status: "stable",
        staleAfter: "2020-01-01",
      });
      await runEmbeddingIndexJob({ kind: "concept", orgId, id: concept.id });
      const swept = await sweepStaleLibraryConcepts();
      expect(swept.deprecated).toBeGreaterThanOrEqual(1);
      expect(swept.orgIds).toContain(orgId);

      const results = await searchLibraryByContext({
        orgId,
        userId: null,
        query: "expired agreement",
      });
      expect(results.find((r) => r.concept.id === concept.id)).toBeUndefined();

      const listed = await listLibraryConcepts({ orgId, userId: null });
      expect(listed.find((c) => c.id === concept.id)?.status).toBe("deprecated");
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("admin deprecate retires a stable concept; drafts cannot be deprecated", async () => {
    try {
      const { concept } = await upsertLibraryConcept({
        orgId,
        userId: null,
        path: "policies/old-policy.md",
        type: "Policy",
        title: "Old policy",
        body: "Superseded.",
        status: "stable",
      });
      const deprecated = await decideLibraryConcept({
        orgId,
        id: concept.id,
        action: "deprecate",
        decidedBy: alice(),
      });
      expect(deprecated.status).toBe("deprecated");

      const { concept: draft } = await upsertLibraryConcept({
        orgId,
        userId: null,
        path: "policies/draft-policy.md",
        type: "Policy",
        title: "Draft policy",
        body: "Pending.",
      });
      await expect(
        decideLibraryConcept({
          orgId,
          id: draft.id,
          action: "deprecate",
          decidedBy: alice(),
        }),
      ).rejects.toThrow(/stable/i);
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("createLibraryDocument is idempotent on content hash per layer", async () => {
    try {
      const input = {
        orgId,
        userId: alice(),
        filename: "policy.md",
        relativePath: "uploads/t/policy.md",
        contentHash: "abc123",
        sizeBytes: 100,
      };
      const first = await createLibraryDocument(input);
      const second = await createLibraryDocument(input);
      expect(first.created).toBe(true);
      expect(second.created).toBe(false);
      expect(second.document.id).toBe(first.document.id);

      const bobCopy = await createLibraryDocument({ ...input, userId: bob() });
      expect(bobCopy.created).toBe(true);
    } finally {
      await deleteTestOrg(orgId);
    }
  });
});
