import { readdir, readFile } from "node:fs/promises";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { AxSignature } from "@ax-llm/ax";
import { describe, expect, it } from "vitest";

const SRC_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "src");
const GENERIC_FIELD_NAMES = new Set(["query", "response"]);

type LocatedSignature = {
  file: string;
  line: number;
  signature: string;
};

async function listSourceFiles(directory: string): Promise<string[]> {
  const files: string[] = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const absolute = join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listSourceFiles(absolute)));
    } else if (entry.name.endsWith(".ts") || entry.name.endsWith(".tsx")) {
      files.push(absolute);
    }
  }
  return files;
}

function decodeLiteral(raw: string): string {
  if (raw.startsWith('"')) return JSON.parse(raw) as string;
  const body = raw.slice(1, -1);
  if (raw.startsWith("`") && body.includes("${")) {
    throw new Error("Ax signatures must be static literals without interpolation");
  }
  return body
    .replace(/\\n/g, "\n")
    .replace(/\\r/g, "\r")
    .replace(/\\t/g, "\t")
    .replace(/\\([\\'`])/g, "$1");
}

function readLiteral(source: string, start: number): { raw: string; end: number } | null {
  let cursor = start;
  while (/\s/.test(source[cursor] ?? "")) cursor += 1;
  const quote = source[cursor];
  if (quote !== '"' && quote !== "'" && quote !== "`") return null;

  let escaped = false;
  for (let index = cursor + 1; index < source.length; index += 1) {
    const char = source[index];
    if (escaped) {
      escaped = false;
    } else if (char === "\\") {
      escaped = true;
    } else if (char === quote) {
      return { raw: source.slice(cursor, index + 1), end: index + 1 };
    }
  }
  throw new Error("Unterminated Ax signature literal");
}

function findAxSignatures(source: string, file: string): LocatedSignature[] {
  const signatures: LocatedSignature[] = [];
  for (const match of source.matchAll(/\bax\s*\(/g)) {
    const literal = readLiteral(source, (match.index ?? 0) + match[0].length);
    const line = source.slice(0, match.index).split("\n").length;
    if (!literal) {
      throw new Error(
        `${file}:${line}: ax() signatures must be inline static literals so this contract test can validate them`,
      );
    }
    signatures.push({ file, line, signature: decodeLiteral(literal.raw) });
  }
  return signatures;
}

function assertDescriptiveSignature(signature: string): void {
  const parsed = new AxSignature(signature);
  const fields = [...parsed.getInputFields(), ...parsed.getOutputFields()];
  const generic = fields.find((field) =>
    GENERIC_FIELD_NAMES.has(field.name.toLowerCase()),
  );
  if (generic) {
    throw new Error(`Invalid Signature: Field name "${generic.name}" is too generic`);
  }
}

describe("Ax signature contracts", () => {
  it.each([
    ["query", "query:string -> answerText:string"],
    ["response", "userQuestion:string -> response:string"],
  ])("rejects the generic %s field name", (field, signature) => {
    expect(() => assertDescriptiveSignature(signature)).toThrow(
      new RegExp(`Field name [\\"']${field}[\\"'] is too generic`),
    );
  });

  it("constructs every production signature with descriptive field names", async () => {
    const signatures: LocatedSignature[] = [];
    for (const absolute of await listSourceFiles(SRC_ROOT)) {
      const file = relative(SRC_ROOT, absolute);
      signatures.push(...findAxSignatures(await readFile(absolute, "utf8"), file));
    }

    expect(signatures.length).toBeGreaterThan(0);
    const failures: string[] = [];
    for (const located of signatures) {
      try {
        assertDescriptiveSignature(located.signature);
      } catch (error) {
        failures.push(
          `${located.file}:${located.line}: ${error instanceof Error ? error.message : String(error)}`,
        );
      }
    }
    expect(failures, failures.join("\n")).toEqual([]);
  });
});
