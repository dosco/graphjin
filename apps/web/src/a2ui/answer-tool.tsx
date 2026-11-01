"use client";

import { useCallback, useEffect, useRef, useState, type CSSProperties } from "react";
import { RotateCcw } from "lucide-react";
import { ANSWER_TOOL_MAX_HTML, answerToolReachesOut } from "@neko/llm/work/answer-tool-check";
import { IconButton } from "@/components/ui/button";
import { registerComponent } from "./renderer";
import type { AnswerToolProps } from "./catalog";
import type { A2UIComponent } from "./types";

export const TOOL_SANDBOX = "allow-scripts";
export const TOOL_CSP = [
  "default-src 'none'",
  "script-src 'unsafe-inline'",
  "style-src 'unsafe-inline'",
  "img-src data:",
  "font-src data:",
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
  "frame-src 'none'",
].join("; ");
export const TOOL_HEIGHT_MESSAGE = "openneko-tool-height";
export const TOOL_MIN_HEIGHT = 120;
export const TOOL_MAX_HEIGHT = 900;
/** The tallest content height the frame records. */
export const TOOL_PRINT_MAX_HEIGHT = 4000;
/** The height a tool may take on a printed A4 page, under its card header. */
export const TOOL_PRINT_BUDGET = 860;

/** How much a tool shrinks so it prints whole on one page. */
export function toolPrintScale(contentHeight: number): number {
  return Math.min(1, TOOL_PRINT_BUDGET / Math.max(1, contentHeight));
}
export const TOOL_MAX_HTML = ANSWER_TOOL_MAX_HTML;
const TOOL_DEFAULT_HEIGHT = 320;
const TOOL_REPORT_TIMEOUT_MS = 4_000;

/** Why the tool cannot run, or null. Runtime isolation is the real boundary; this rejects tools that try to reach out. */
export function toolHtmlProblem(html: unknown): string | null {
  if (typeof html !== "string" || !html.trim()) return "The tool is empty.";
  if (html.length > TOOL_MAX_HTML) return "The tool is too large to show.";
  if (answerToolReachesOut(html)) {
    return "The tool reaches outside the answer, so OpenNeko did not run it.";
  }
  return null;
}

/** Tokens the frame copies from the app; the frame cannot read the parent's CSS. */
export const TOOL_TOKENS = [
  "--bg", "--card", "--text", "--text2", "--border", "--neutral", "--neutral-soft",
  "--accent", "--accent-hover", "--on-accent", "--radius-inner", "--radius-control",
] as const;

const BASE_CSS = `
*,*::before,*::after{box-sizing:border-box}
html{-webkit-text-size-adjust:100%}
body{margin:0;padding:16px;background:var(--card);color:var(--text);font:14px/1.55 Manrope,ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;font-variant-numeric:tabular-nums}
main{display:flex;flex-direction:column;gap:12px;min-width:0}
h1,h2,h3{margin:0;font-family:Archivo,Manrope,ui-sans-serif,system-ui,sans-serif;font-weight:800;letter-spacing:-0.01em;line-height:1.25}
h1{font-size:18px}h2{font-size:16px}h3{font-size:14px}
p{margin:0}
label{display:flex;flex-direction:column;gap:6px;color:var(--text2);font-size:12px;font-weight:600}
input,select,textarea,button{font:inherit;color:var(--text)}
input,select,textarea{width:100%;min-height:40px;padding:8px 12px;border:1px solid var(--border);border-radius:var(--radius-control);background:var(--card);font-size:14px;font-weight:500}
input[type=range]{padding:0;border:0;min-height:32px;accent-color:var(--accent)}
input[type=checkbox],input[type=radio]{width:16px;min-height:16px;accent-color:var(--accent)}
input:focus-visible,select:focus-visible,textarea:focus-visible,button:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
button{min-height:40px;padding:8px 16px;border:1px solid var(--border);border-radius:var(--radius-control);background:var(--card);font-size:14px;font-weight:600;cursor:pointer}
button:hover{background:var(--neutral-soft)}
button.primary,button[type=submit]{border-color:var(--accent);background:var(--accent);color:var(--on-accent)}
button.primary:hover,button[type=submit]:hover{background:var(--accent-hover)}
output,.result{display:block;padding:12px 14px;border-radius:var(--radius-inner);background:var(--neutral-soft);color:var(--text);font-family:Archivo,Manrope,ui-sans-serif,system-ui,sans-serif;font-size:20px;font-weight:800;letter-spacing:-0.01em}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:8px 10px;border-bottom:1px solid var(--border);text-align:left}
th{color:var(--text2);font-size:12px;font-weight:700}
small,.muted{color:var(--text2);font-size:12px}
@media (pointer:coarse){input,select,textarea,button{min-height:44px}}
@media (max-width:480px){body{padding:12px}}
@media (prefers-reduced-motion:reduce){*{animation:none!important;transition:none!important}}
`;

/** Loaded after the tool's own styles: nothing may be wider than the frame. */
const FIT_CSS = `
html,body{max-width:100%;overflow-x:hidden}
main *{max-width:100%!important;min-width:0!important;overflow-wrap:anywhere}
main table{display:block;overflow-x:auto}
@media (max-width:480px){main [style*="grid-template-columns"],main .grid{grid-template-columns:minmax(0,1fr)!important}main *{flex-wrap:wrap}}
`;

const BOOTSTRAP = `(function(){
var last=0;
function send(h){parent.postMessage({type:"${TOOL_HEIGHT_MESSAGE}",height:h},"*");}
function measure(){return Math.ceil(document.body.getBoundingClientRect().height);}
function report(){var h=measure();if(h!==last){last=h;send(h);}}
if(typeof ResizeObserver==="function"){new ResizeObserver(report).observe(document.body);}
addEventListener("load",report);report();
[100,400,1200,3000].forEach(function(ms){setTimeout(function(){last=measure();send(last);},ms);});
})();`;

const TOKEN_VALUE = /^[#(),.%\w\s-]{1,64}$/;

/** `:root` declarations for the tokens, keeping only plain color and length values. */
export function toolTokenCss(tokens: Readonly<Record<string, string>>): string {
  const declarations = TOOL_TOKENS.flatMap((name) => {
    const value = tokens[name]?.trim();
    return value && TOKEN_VALUE.test(value) ? [`${name}:${value}`] : [];
  });
  return `:root{${declarations.join(";")}}`;
}

function readAppTokens(): Record<string, string> {
  const style = getComputedStyle(document.documentElement);
  return Object.fromEntries(TOOL_TOKENS.map((name) => [name, style.getPropertyValue(name)]));
}

/** The complete frame document: CSP first, house styles, the tool, then the height reporter. */
export function buildToolDocument(html: string, tokens: Readonly<Record<string, string>> = {}): string {
  return [
    "<!doctype html><html><head>",
    `<meta http-equiv="Content-Security-Policy" content="${TOOL_CSP}">`,
    '<meta http-equiv="x-dns-prefetch-control" content="off">',
    '<meta charset="utf-8">',
    '<meta name="viewport" content="width=device-width,initial-scale=1">',
    `<style>${toolTokenCss(tokens)}${BASE_CSS}</style>`,
    "</head><body><main>",
    html,
    "</main>",
    `<style>${FIT_CSS}</style>`,
    `<script>${BOOTSTRAP}</script>`,
    "</body></html>",
  ].join("");
}

/** The frame height from a message, or null when the message is not this frame's report. */
export function readToolHeight(event: Pick<MessageEvent, "source" | "data">, frame: Window | null | undefined): number | null {
  if (!frame || event.source !== frame) return null;
  const data = event.data as { type?: unknown; height?: unknown } | null;
  if (!data || typeof data !== "object" || data.type !== TOOL_HEIGHT_MESSAGE) return null;
  if (typeof data.height !== "number" || !Number.isFinite(data.height)) return null;
  return Math.min(TOOL_PRINT_MAX_HEIGHT, Math.max(TOOL_MIN_HEIGHT, Math.round(data.height)));
}

type FrameState = "loading" | "ready" | "silent" | "navigated";

export function AnswerToolFrame({ title, html, tokens: initialTokens = null }: { title: string; html: string; tokens?: Record<string, string> | null }) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const loads = useRef(0);
  const [generation, setGeneration] = useState(0);
  const [height, setHeight] = useState(TOOL_DEFAULT_HEIGHT);
  const [state, setState] = useState<FrameState>("loading");
  const [tokens, setTokens] = useState<Record<string, string> | null>(initialTokens);
  const problem = toolHtmlProblem(html);
  const srcDoc = problem || !tokens ? "" : buildToolDocument(html, tokens);

  useEffect(() => {
    if (initialTokens) return;
    // Read after hydration so the server and first client render agree.
    const id = window.setTimeout(() => setTokens(readAppTokens()), 0);
    return () => window.clearTimeout(id);
  }, [initialTokens]);

  useEffect(() => {
    if (problem) return;
    const onMessage = (event: MessageEvent) => {
      const next = readToolHeight(event, frameRef.current?.contentWindow);
      if (next === null) return;
      setHeight(next);
      setState((current) => (current === "navigated" ? current : "ready"));
    };
    window.addEventListener("message", onMessage);
    const timer = window.setTimeout(() => {
      setState((current) => (current === "loading" ? "silent" : current));
    }, TOOL_REPORT_TIMEOUT_MS);
    return () => {
      window.removeEventListener("message", onMessage);
      window.clearTimeout(timer);
    };
  }, [generation, problem]);

  const reload = useCallback(() => {
    loads.current = 0;
    setHeight(TOOL_DEFAULT_HEIGHT);
    setState("loading");
    setGeneration((value) => value + 1);
  }, []);

  const onLoad = useCallback(() => {
    loads.current += 1;
    if (loads.current > 1) setState("navigated");
  }, []);

  const blocked = Boolean(problem) || state === "navigated";
  return (
    <section className="work-tool" aria-label={title} data-state={blocked ? "blocked" : state}>
      <header className="work-tool-head">
        <div className="work-tool-copy">
          <h3 className="work-tool-title">{title}</h3>
          <p className="work-tool-note">Interactive tool · runs in an isolated frame</p>
        </div>
        {problem ? null : (
          <IconButton label="Reload tool" size="icon-sm" variant="ghost" onClick={reload}>
            <RotateCcw aria-hidden="true" strokeWidth={1.9} />
          </IconButton>
        )}
      </header>
      {blocked ? (
        <p className="work-tool-message" role="status">
          {problem ?? "The tool tried to leave the answer, so OpenNeko stopped it. Reload it to start again."}
        </p>
      ) : (
        <div
          className="work-tool-body"
          style={{
            "--tool-content-height": `${height}px`,
            "--tool-print-scale": toolPrintScale(height),
          } as CSSProperties}
        >
          {state === "loading" ? <div className="work-tool-loading" aria-hidden="true" /> : null}
          {tokens ? <iframe
            key={generation}
            ref={frameRef}
            className="work-tool-frame"
            title={title}
            sandbox={TOOL_SANDBOX}
            referrerPolicy="no-referrer"
            allow=""
            srcDoc={srcDoc}
            onLoad={onLoad}
            style={{ height: Math.min(height, TOOL_MAX_HEIGHT) }}
          /> : null}
          {state === "silent" ? (
            <p className="work-tool-message" role="status">
              The tool did not report its size. Reload it if it looks cut off.
            </p>
          ) : null}
        </div>
      )}
    </section>
  );
}

registerComponent("AnswerTool", (comp: A2UIComponent) => {
  const props = comp as unknown as AnswerToolProps & { id: string };
  const title = typeof props.title === "string" && props.title.trim() ? props.title : "Interactive tool";
  return <AnswerToolFrame key={props.id} title={title} html={typeof props.html === "string" ? props.html : ""} />;
});
