import * as React from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Check, Copy } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

function MarkdownContent({ value, className }) {
  return (
    <div className={cn("grid gap-3 break-words", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          p: ({ children }) => <p className="m-0 leading-6">{children}</p>,
          h1: ({ children }) => <h1 className="m-0 text-lg font-semibold leading-7 tracking-normal">{children}</h1>,
          h2: ({ children }) => <h2 className="m-0 text-base font-semibold leading-6 tracking-normal">{children}</h2>,
          h3: ({ children }) => <h3 className="m-0 text-sm font-semibold leading-6 tracking-normal">{children}</h3>,
          ul: ({ children }) => <ul className="m-0 grid list-disc gap-1.5 pl-5">{children}</ul>,
          ol: ({ children }) => <ol className="m-0 grid list-decimal gap-1.5 pl-5">{children}</ol>,
          li: ({ children }) => <li className="pl-1 leading-6">{children}</li>,
          blockquote: ({ children }) => (
            <blockquote className="m-0 border-l-2 border-primary/45 bg-muted/38 py-2 pl-3 text-muted-foreground">
              <div className="grid gap-2">{children}</div>
            </blockquote>
          ),
          a: ({ href, children }) => (
            <a
              href={safeHref(href)}
              target="_blank"
              rel="noreferrer noopener"
              className="font-medium text-primary underline underline-offset-4"
            >
              {children}
            </a>
          ),
          img: ({ src, alt }) => (
            <a
              href={safeHref(src)}
              target="_blank"
              rel="noreferrer noopener"
              className="font-medium text-primary underline underline-offset-4"
            >
              {alt || src || "image link"}
            </a>
          ),
          table: ({ children }) => (
            <div className="overflow-x-auto rounded-md border border-white/70 bg-card/60">
              <table className="w-full border-collapse text-left text-xs">{children}</table>
            </div>
          ),
          thead: ({ children }) => <thead className="bg-muted/60 text-foreground">{children}</thead>,
          th: ({ children }) => <th className="border-b border-white/70 px-3 py-2 font-semibold">{children}</th>,
          td: ({ children }) => <td className="border-t border-white/58 px-3 py-2 align-top">{children}</td>,
          pre: ({ children }) => {
            const text = extractText(children);
            return (
              <div className="group/code relative overflow-hidden rounded-md border border-white/70 bg-muted/52">
                <CopyButton
                  text={text}
                  label="Copy code"
                  copiedLabel="Copied code"
                  className="absolute right-2 top-2 opacity-0 transition group-hover/code:opacity-100 focus-visible:opacity-100"
                />
                <pre className="m-0 max-h-96 overflow-auto p-3 pr-12 text-xs leading-5">
                  {children}
                </pre>
              </div>
            );
          },
          code: ({ node, className, children, ...props }) => {
            const isBlock = className?.startsWith("language-");
            return (
              <code
                className={cn(
                  isBlock
                    ? "font-mono text-foreground/90"
                    : "rounded bg-muted/68 px-1.5 py-0.5 font-mono text-[0.86em] text-foreground",
                  className
                )}
                {...props}
              >
                {children}
              </code>
            );
          },
        }}
      >
        {value || ""}
      </ReactMarkdown>
    </div>
  );
}

function CopyButton({ text, label = "Copy", copiedLabel = "Copied", className }) {
  const [copied, setCopied] = React.useState(false);

  async function handleCopy() {
    await copyText(text || "");
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  }

  return (
    <TooltipProvider delayDuration={250}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button type="button" variant="ghost" size="icon" className={cn("size-7 rounded-md", className)} onClick={handleCopy}>
            {copied ? <Check className="size-3.5" aria-hidden="true" /> : <Copy className="size-3.5" aria-hidden="true" />}
            <span className="sr-only">{copied ? copiedLabel : label}</span>
          </Button>
        </TooltipTrigger>
        <TooltipContent>{copied ? copiedLabel : label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

async function copyText(text) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Fall back for browsers that expose clipboard but deny the async call.
    }
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  document.body.removeChild(textarea);
}

function extractText(value) {
  if (typeof value === "string" || typeof value === "number") {
    return String(value);
  }
  if (Array.isArray(value)) {
    return value.map(extractText).join("");
  }
  if (React.isValidElement(value)) {
    return extractText(value.props.children);
  }
  return "";
}

function safeHref(href) {
  if (!href) {
    return "#";
  }
  if (/^(https?:|mailto:|\/(?!\/)|#)/i.test(href)) {
    return href;
  }
  return "#";
}

export { CopyButton, MarkdownContent };
