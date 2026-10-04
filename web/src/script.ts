import type { Kind, Segment } from "./api";

// The composer's text form of an entry: one segment per line, kind by the
// line's first character. Narration has no mark. Closing marks are optional.
const marks: Record<Exclude<Kind, "narration">, [open: string, close: string]> = {
  action: ["(", ")"],
  speech: ['"', '"'],
  thought: ["`", "`"],
};

export function parseScript(text: string): Segment[] {
  const segments: Segment[] = [];
  for (const raw of text.split("\n")) {
    let line = raw.trim();
    if (!line) continue;
    let kind: Kind = "narration";
    for (const [k, [open, close]] of Object.entries(marks) as [Exclude<Kind, "narration">, [string, string]][]) {
      if (line.startsWith(open)) {
        kind = k;
        line = line.slice(open.length);
        if (line.endsWith(close)) line = line.slice(0, -close.length);
        line = line.trim();
        break;
      }
    }
    if (line) segments.push({ kind, text: line });
  }
  return segments;
}

export function formatScript(segments: Segment[]): string {
  return segments
    .map((s) => (s.kind === "narration" ? s.text : `${marks[s.kind][0]}${s.text}${marks[s.kind][1]}`))
    .join("\n");
}
