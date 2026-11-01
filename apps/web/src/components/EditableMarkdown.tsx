"use client";

import {
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import Markdown, { type Components } from "react-markdown";

type Props = {
  value: string;
  onChange: (next: string) => void;
  onCommit?: () => void;
  placeholder?: ReactNode;
  components: Components;
  readOnly?: boolean;
  ariaLabel?: string;
};

type CaretPos = { x: number; y: number } | null;

type CaretLookup = {
  caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null;
  caretRangeFromPoint?: (x: number, y: number) => Range | null;
};

export default function EditableMarkdown({
  value,
  onChange,
  onCommit,
  placeholder,
  components,
  readOnly,
  ariaLabel,
}: Props) {
  const [editing, setEditing] = useState(false);
  const editRef = useRef<HTMLDivElement | null>(null);
  const pendingCaretRef = useRef<CaretPos>(null);

  const enterEdit = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (readOnly) return;
      const target = e.target as HTMLElement;
      if (target.tagName === "A") return;
      pendingCaretRef.current = { x: e.clientX, y: e.clientY };
      setEditing(true);
    },
    [readOnly],
  );

  useLayoutEffect(() => {
    if (!editing) return;
    const node = editRef.current;
    if (!node) return;

    node.innerText = value;

    const caret = pendingCaretRef.current;
    pendingCaretRef.current = null;
    const sel = window.getSelection();
    if (!sel) {
      node.focus();
      return;
    }
    const range = document.createRange();
    let placed = false;
    if (caret) {
      const doc = document as Document & CaretLookup;
      if (typeof doc.caretPositionFromPoint === "function") {
        const pos = doc.caretPositionFromPoint(caret.x, caret.y);
        if (pos && node.contains(pos.offsetNode)) {
          range.setStart(pos.offsetNode, pos.offset);
          range.collapse(true);
          placed = true;
        }
      } else if (typeof doc.caretRangeFromPoint === "function") {
        const r = doc.caretRangeFromPoint(caret.x, caret.y);
        if (r && node.contains(r.startContainer)) {
          range.setStart(r.startContainer, r.startOffset);
          range.collapse(true);
          placed = true;
        }
      }
    }
    if (!placed) {
      range.selectNodeContents(node);
      range.collapse(false);
    }
    sel.removeAllRanges();
    sel.addRange(range);
    node.focus();
    // We intentionally only re-run when entering edit mode. Re-syncing innerText
    // on every value change would clobber the caret while typing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editing]);

  const handleInput = useCallback(
    (e: React.FormEvent<HTMLDivElement>) => {
      onChange(e.currentTarget.innerText);
    },
    [onChange],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      if (e.key === "Enter") {
        e.preventDefault();
        document.execCommand("insertText", false, "\n");
      }
    },
    [],
  );

  const handlePaste = useCallback(
    (e: React.ClipboardEvent<HTMLDivElement>) => {
      e.preventDefault();
      const text = e.clipboardData.getData("text/plain");
      if (text) document.execCommand("insertText", false, text);
    },
    [],
  );

  const handleBlur = useCallback(() => {
    // The contentEditable session mutates children directly (innerText/insertText).
    // React's reconciler doesn't track those nodes, so reusing the same <div> for
    // the idle branch would leave the raw text in place behind the rendered Markdown.
    // Clear the manual children before flipping state.
    if (editRef.current) {
      editRef.current.replaceChildren();
    }
    setEditing(false);
    onCommit?.();
  }, [onCommit]);

  useEffect(() => {
    if (editing) return;
    pendingCaretRef.current = null;
  }, [editing]);

  if (editing) {
    return (
      <div
        ref={editRef}
        className="whitespace-pre-wrap break-words text-base leading-[1.65] text-text2 caret-accent outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg rounded-control"
        contentEditable
        suppressContentEditableWarning
        role="textbox"
        aria-label={ariaLabel}
        aria-multiline="true"
        spellCheck
        onInput={handleInput}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        onBlur={handleBlur}
      />
    );
  }

  const hasContent = value.length > 0;
  return (
    <div
      className="cursor-text rounded-control focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg"
      onPointerDown={readOnly ? undefined : enterEdit}
      role={readOnly ? undefined : "textbox"}
      aria-label={ariaLabel}
    >
      {hasContent ? <Markdown components={components}>{value}</Markdown> : placeholder}
    </div>
  );
}
