import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { app_user, db, library_document } from "@neko/db";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import {
  browseLibrary,
  createLibraryDocument,
  parseLibraryBrowseOptions,
  removeLibraryDocument,
} from "../../src/work/library";

it("validates bounded paging and filter values", () => {
  expect(parseLibraryBrowseOptions(new URLSearchParams("q=stock"))).toMatchObject({ page: 1, pageSize: 50, sort: "recent" });
  for (const invalid of ["page=0", "page=-1", "page=1.5", "page=Infinity", "pageSize=101", "sort=bad", "status=cataloged", `q=${"x".repeat(201)}`]) {
    expect(() => parseLibraryBrowseOptions(new URLSearchParams(invalid))).toThrow();
  }
});

const reachable = await dbReachable();
if (!reachable && process.env.LIBRARY_BROWSE_REQUIRE_DB === "1") throw new Error("Library browsing requires Postgres in CI");
(reachable ? describe : describe.skip)("Library documents against Postgres", () => {
  const orgId = uniqueOrgId("library-browser");
  const alice = `${orgId}-alice`;
  const bob = `${orgId}-bob`;
  const options = (query = "") => parseLibraryBrowseOptions(new URLSearchParams(query));

  beforeAll(async () => {
    await createTestOrg(orgId);
    await db().insert(app_user).values([
      { id: alice, org_id: orgId, email: "alice@example.com" },
      { id: bob, org_id: orgId, email: "bob@example.com" },
    ]);
    await db().insert(library_document).values(Array.from({ length: 125 }, (_, index) => ({
      id: randomUUID(), org_id: orgId, user_id: alice,
      filename: index === 7 ? "Q4 price_list 100%.xlsx" : `Report ${String(index).padStart(3, "0")}.pdf`,
      relative_path: `neko_library/users/${alice}/${index}/doc.pdf`,
      content_hash: `browser-${index}`, size_bytes: 100, status: index === 3 ? "failed" : "ready",
    })));
    await db().insert(library_document).values({
      org_id: orgId, user_id: bob, filename: "Bob secret.pdf", relative_path: `neko_library/users/${bob}/x/doc.pdf`,
      content_hash: "bob", size_bytes: 1, status: "ready",
    });
  });
  afterAll(async () => { await deleteTestOrg(orgId); });

  it("pages through the reader's own documents only", async () => {
    const first = await browseLibrary({ orgId, userId: alice }, options());
    expect(first.total).toBe(125);
    expect(first.documents).toHaveLength(50);
    const last = await browseLibrary({ orgId, userId: alice }, options("page=3"));
    expect(last.documents).toHaveLength(25);
    const bobs = await browseLibrary({ orgId, userId: bob }, options());
    expect(bobs.documents.map(d => d.filename)).toEqual(["Bob secret.pdf"]);
  });

  it("matches name words literally and filters by status", async () => {
    const found = await browseLibrary({ orgId, userId: alice }, options("q=price_list 100%"));
    expect(found.documents.map(d => d.filename)).toEqual(["Q4 price_list 100%.xlsx"]);
    expect((await browseLibrary({ orgId, userId: alice }, options("q=Bob"))).total).toBe(0);
    expect((await browseLibrary({ orgId, userId: alice }, options("status=failed"))).total).toBe(1);
  });

  it("creates once per content and removes only the owner's document", async () => {
    const input = { orgId, userId: alice, filename: "dup.md", relativePath: `neko_library/users/${alice}/h/dup.md`, contentHash: "dup", sizeBytes: 3 };
    const first = await createLibraryDocument(input);
    const second = await createLibraryDocument(input);
    expect(first.created).toBe(true);
    expect(second).toMatchObject({ created: false, document: { id: first.document.id, status: "ready" } });
    await expect(removeLibraryDocument({ orgId, id: first.document.id, userId: bob })).rejects.toThrow();
    await expect(removeLibraryDocument({ orgId, id: first.document.id, userId: alice })).resolves.toMatchObject({ filename: "dup.md" });
  });
});
