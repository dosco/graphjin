// Dependency manifest for Neko's bundled skills (packages/llm/assets/builtin-skills/*).
// The upstream Anthropic skills don't ship a machine-readable dep list — they
// mention requirements in SKILL.md prose. Mirroring those here lets the Dockerfile
// and scripts/skill-doctor.ts know what to install.
//
// User-authored skills are not in this manifest. They typically use the runtime's
// existing tools (Bash, Read/Write) rather than external Python/binary deps.

export interface SkillDeps {
  python: string[];
  pip: string[];
  node?: string[];
  npm?: string[];
  binaries: string[];
  apt: string[];
  brew: string[];
}

export const KNOWN_SKILL_DEPS: Record<string, SkillDeps> = {
  xlsx: {
    python: ["openpyxl"],
    pip: ["openpyxl"],
    binaries: ["soffice"],
    apt: ["libreoffice"],
    brew: ["--cask libreoffice"],
  },
  pptx: {
    python: ["pptx", "PIL"],
    pip: ["python-pptx", "Pillow"],
    node: ["pptxgenjs", "react", "react-dom", "react-icons", "sharp"],
    npm: [
      "pptxgenjs@4.0.1",
      "react@19.2.8",
      "react-dom@19.2.8",
      "react-icons@5.7.0",
      "sharp@0.35.3",
    ],
    binaries: ["soffice"],
    apt: ["libreoffice"],
    brew: ["--cask libreoffice"],
  },
  docx: {
    python: ["docx"],
    pip: ["python-docx"],
    node: ["docx"],
    npm: ["docx@9.7.1"],
    binaries: ["soffice"],
    apt: ["libreoffice"],
    brew: ["--cask libreoffice"],
  },
  pdf: {
    python: ["pypdf", "pdfplumber", "reportlab", "PIL"],
    pip: ["pypdf", "pdfplumber", "reportlab", "Pillow"],
    binaries: ["pdftotext", "qpdf"],
    apt: ["poppler-utils", "qpdf"],
    brew: ["poppler", "qpdf"],
  },
  "document-extraction": {
    // The bundled extract_text.py degrades to stdlib zipfile parsing for
    // docx/pptx/xlsx, so only the PDF/OCR path truly needs these.
    python: ["pypdf", "docx", "pptx", "openpyxl"],
    pip: ["pypdf", "python-docx", "python-pptx", "openpyxl"],
    binaries: ["pdftotext", "tesseract", "file"],
    apt: ["poppler-utils", "tesseract-ocr", "file"],
    brew: ["poppler", "tesseract"],
  },
  "internal-comms": {
    python: [],
    pip: [],
    binaries: [],
    apt: [],
    brew: [],
  },
  "skill-creator": {
    python: ["yaml"],
    pip: ["PyYAML"],
    binaries: [],
    apt: [],
    brew: [],
  },
  // ─── Tier-A finance skills (Hermes-sourced, Apache-2.0) ────────────
  // All five model skills pair with excel-author and use openpyxl — same
  // dep set as the generic xlsx builtin, so no new pip/apt requirements
  // beyond what's already baked.
  "excel-author": {
    python: ["openpyxl"],
    pip: ["openpyxl"],
    binaries: [],
    apt: [],
    brew: [],
  },
  "pptx-author": {
    python: ["pptx", "PIL"],
    pip: ["python-pptx", "Pillow"],
    binaries: [],
    apt: [],
    brew: [],
  },
  "3-statement-model": {
    python: ["openpyxl"],
    pip: ["openpyxl"],
    binaries: [],
    apt: [],
    brew: [],
  },
  "dcf-model": {
    python: ["openpyxl"],
    pip: ["openpyxl"],
    binaries: [],
    apt: [],
    brew: [],
  },
  "lbo-model": {
    python: ["openpyxl"],
    pip: ["openpyxl"],
    binaries: [],
    apt: [],
    brew: [],
  },
  "comps-analysis": {
    python: ["openpyxl"],
    pip: ["openpyxl"],
    binaries: [],
    apt: [],
    brew: [],
  },
  "merger-model": {
    python: ["openpyxl"],
    pip: ["openpyxl"],
    binaries: [],
    apt: [],
    brew: [],
  },
  "graphjin-config": {
    python: [],
    pip: [],
    binaries: [],
    apt: [],
    brew: [],
  },
};

// Note: the Library sends digital PDF/Office/CSV inputs only to the owned
// `librarian` service over NEKO_LIBRARIAN_URL; Markdown/text are decoded by the
// worker. The bundled document-extraction skill below stays for the AGENT's
// own in-sandbox use and is not a Library fallback. Docling is therefore not a
// Python dependency in this manifest.

// Union of all pip / apt / brew deps across the manifest. Used by the
// Dockerfile-time installer to bake everything into the image, and by the
// dev-side doctor script to print fresh-machine setup commands.
export function aggregateSkillDeps(): {
  pip: string[];
  npm: string[];
  apt: string[];
  brewFormulas: string[];
  brewCasks: string[];
} {
  const pip = new Set<string>();
  const npm = new Set<string>();
  const apt = new Set<string>();
  const brewFormulas = new Set<string>();
  const brewCasks = new Set<string>();
  for (const deps of Object.values(KNOWN_SKILL_DEPS)) {
    for (const pkg of deps.pip) pip.add(pkg);
    for (const pkg of deps.npm ?? []) npm.add(pkg);
    for (const pkg of deps.apt) apt.add(pkg);
    for (const entry of deps.brew) {
      if (entry.startsWith("--cask ")) brewCasks.add(entry.slice(7));
      else brewFormulas.add(entry);
    }
  }
  return {
    pip: [...pip],
    npm: [...npm],
    apt: [...apt],
    brewFormulas: [...brewFormulas],
    brewCasks: [...brewCasks],
  };
}

/**
 * Synthesise a SkillDeps record from raw SKILL.md frontmatter — used
 * by the doctor for community skills installed under ~/.openneko/
 * skills/ that don't have a hand-maintained KNOWN_SKILL_DEPS entry.
 *
 * Only the binary-presence check matters here: we can't reliably map
 * a binary name (e.g. `blogwatcher-cli`) to an apt/brew package name,
 * so we record the binary for the OK/MISSING report and leave apt/brew
 * empty (operators install community-skill binaries manually).
 *
 * Python imports declared in skill prose aren't parsed — too varied
 * across community skills to extract reliably.
 */
export function synthesizeSkillDeps(prereq: {
  commands?: string[];
  envVars?: string[];
}): SkillDeps {
  return {
    python: [],
    pip: [],
    binaries: [...(prereq.commands ?? [])],
    apt: [],
    brew: [],
  };
}
