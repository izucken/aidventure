import { useEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode, type SyntheticEvent } from "react";

import { completions, type Analysis, type Context, type Line } from "./script";
import styles from "./ScriptEditor.module.css";

type Props = {
  id?: string;
  value: string;
  analysis: Analysis;
  context: Context;
  onChange: (value: string) => void;
  onSubmit: () => void;
};

const lineClass: Record<Line["kind"], string | undefined> = {
  blank: undefined,
  cue: styles.cue,
  beat: undefined,
  staging: styles.staging,
  move: styles.move,
  with: styles.move,
};

// The text, styled as it reads, under a transparent textarea with the same
// metrics: the text is typed where it is shown, and problems are marked on
// the span they concern, with the reason as a tip under the pointer. The
// textarea takes the pointer, so the tip is found by where the marked spans
// are, not by hovering them. Where a name goes, the rest of the first name
// that fits is ghosted after the caret; Tab takes it, Tab again takes the
// next. Ctrl+Enter submits.
export function ScriptEditor({ id, value, analysis, context, onChange, onSubmit }: Props) {
  const editor = useRef<HTMLDivElement>(null);
  const backdrop = useRef<HTMLPreElement>(null);
  const text = useRef<HTMLTextAreaElement>(null);
  const [tip, setTip] = useState<{ message: string; left: number; top: number } | null>(null);
  // The caret, when nothing is selected; -1 while a selection is or the field is blurred.
  const [caret, setCaret] = useState(-1);
  // Where the caret goes once the value it was set with has rendered.
  const place = useRef<number | null>(null);
  // Tabbing through the names that fit what was typed before the first Tab.
  const cycle = useRef<{ from: number; options: string[]; index: number } | null>(null);

  useEffect(() => {
    if (place.current !== null && text.current) {
      text.current.setSelectionRange(place.current, place.current);
      setCaret(place.current);
      place.current = null;
    }
  }, [value]);

  function moved(e: SyntheticEvent<HTMLTextAreaElement>) {
    const t = e.currentTarget;
    setCaret(t.selectionStart === t.selectionEnd ? t.selectionStart : -1);
  }

  const completion = caret >= 0 ? completions(value, caret, context) : null;

  function hover(e: MouseEvent<HTMLTextAreaElement>) {
    const box = editor.current?.getBoundingClientRect();
    if (!box || !backdrop.current) return;
    for (const span of backdrop.current.querySelectorAll<HTMLElement>("[data-problem]")) {
      for (const r of span.getClientRects()) {
        if (e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom) {
          const next = { message: span.dataset["problem"] ?? "", left: r.left - box.left, top: r.bottom - box.top };
          if (tip?.message !== next.message || tip.top !== next.top) setTip(next);
          return;
        }
      }
    }
    if (tip) setTip(null);
  }

  function keyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      onSubmit();
      return;
    }
    if (e.key !== "Tab" || e.shiftKey || !completion) {
      cycle.current = null;
      return;
    }
    e.preventDefault();
    const c = cycle.current;
    const same = c && c.from === completion.from && value.slice(c.from, completion.to) === c.options[c.index];
    const next =
      same && c ? { from: c.from, options: c.options, index: (c.index + 1) % c.options.length } : { from: completion.from, options: completion.options, index: 0 };
    cycle.current = next;
    const name = next.options[next.index] ?? "";
    const after = value.slice(completion.to);
    const suffix = completion.suffix && !after.trimStart().startsWith(completion.suffix) ? completion.suffix : "";
    place.current = next.from + name.length + suffix.length;
    onChange(value.slice(0, next.from) + name + suffix + after);
  }

  // The ghost: what the first fitting name adds to what is typed, when the caret ends its line.
  const ghost = (() => {
    if (!completion) return null;
    const typed = value.slice(completion.from, completion.to);
    const lineEnd = value.indexOf("\n", caret);
    if (value.slice(caret, lineEnd < 0 ? undefined : lineEnd).trim() !== "") return null;
    const first = completion.options.find((o) => o.toLowerCase() !== typed.toLowerCase()) ?? "";
    const rest = first.slice(typed.length);
    if (!rest) return null;
    const lineStart = value.lastIndexOf("\n", caret - 1) + 1;
    return { line: value.slice(0, lineStart).split("\n").length - 1, col: caret - lineStart, rest };
  })();

  const raws = value.split("\n");
  return (
    <div ref={editor} className={styles.editor}>
      <pre ref={backdrop} className={styles.backdrop} aria-hidden="true">
        {analysis.lines.map((line, i) => {
          const raw = raws[i] ?? "";
          const problems = analysis.problems.filter((p) => p.line === i);
          return (
            <span key={i} className={lineClass[line.kind] ?? (line.kind === "beat" ? styles[line.segment.kind] : undefined)}>
              {ghost?.line === i ? (
                <>
                  {marked(raw.slice(0, ghost.col), problems)}
                  <span className={styles.ghost}>{ghost.rest}</span>
                  {raw.slice(ghost.col)}
                </>
              ) : (
                marked(raw, problems)
              )}
              {"\n"}
            </span>
          );
        })}
        {/* A trailing line keeps the height in step with the textarea's. */}
        {"​"}
      </pre>
      <textarea
        ref={text}
        id={id}
        className={styles.text}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          moved(e);
        }}
        onSelect={moved}
        onKeyDown={keyDown}
        onMouseMove={hover}
        onMouseLeave={() => setTip(null)}
        onBlur={() => setCaret(-1)}
        onScroll={(e) => {
          if (backdrop.current) backdrop.current.scrollTop = e.currentTarget.scrollTop;
        }}
        rows={6}
        spellCheck={false}
        placeholder={'Name:\n(action)\n"speech"\n`thought`\nnarration\nCut -> Place, or Cut => New place: what it is | how it stands\nWith: Name, Name\n\nor: Enter: Name, Name.'}
      />
      {tip && (
        <div className={styles.tip} style={{ left: tip.left, top: tip.top }} role="tooltip">
          {tip.message}
        </div>
      )}
    </div>
  );
}

// marked wraps the problem spans of a line, so they can be underlined.
function marked(raw: string, problems: { from: number; to: number; message: string }[]): ReactNode {
  if (problems.length === 0) return raw;
  const out: ReactNode[] = [];
  let at = 0;
  for (const p of [...problems].sort((a, b) => a.from - b.from)) {
    const from = Math.max(at, p.from);
    const to = Math.min(raw.length, p.to);
    if (from >= to) continue;
    out.push(raw.slice(at, from));
    out.push(
      <span key={from} className={styles.problem} data-problem={p.message}>
        {raw.slice(from, to)}
      </span>,
    );
    at = to;
  }
  out.push(raw.slice(at));
  return out;
}
