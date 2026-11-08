import type { Direction, Kind, Segment } from "./api";

// The composer's text form of an entry. The first line may be a cue, who
// acts, as the script reads it: "Mara:". Then one segment per line, kind by
// the line's first character; narration has no mark, closing marks are
// optional. A staging line, "Enter: Mara, Fen." or "Exit: Mara.", stands
// alone: it is who comes or goes, not a beat. Last may come a move, where
// the entry takes the play: "Cut -> Tavern" to a place of the game,
// "Cut => Cellar: what it is | how it stands to the place left" to a new
// one, "Split" likewise for those who leave, "Converge -> Thread" to join
// another thread; "With: Mara, Fen" under a cut or split says who.
const marks: Record<Exclude<Kind, "narration">, [open: string, close: string]> = {
  action: ["(", ")"],
  speech: ['"', '"'],
  thought: ["`", "`"],
};

export type Staging = { type: "enter" | "exit"; names: string[] };

type Span = { from: number; to: number };

// One line of the text as it reads. Columns are where the meaningful part
// sits, for marking a problem on it.
type Named = { name: string } & Span;

export type Move = {
  type: "cut" | "split" | "converge";
  // Known is a place or thread of the game, by name; new is a place made now.
  known: boolean;
  target: Named;
  description: string;
  link: Named;
};

export type Line =
  | { kind: "blank" }
  | ({ kind: "cue"; name: string } & Span)
  | ({ kind: "beat"; segment: Segment } & Span)
  | { kind: "staging"; staging: Staging; names: Named[] }
  | { kind: "move"; move: Move }
  | { kind: "with"; names: Named[] };

// What the text is checked against: the game's actors, who is in the scene
// at that point, and, during a locked turn, the one actor who may act.
export type Context = {
  actors: { id: string; name: string }[];
  present: string[];
  locked?: string;
  // Editing a beat in place: staging cannot take its place.
  editing?: boolean;
  // Where the play may go from here; only the composer at the tail has it.
  // inScene says there is a scene to stay in, and one to link a new place to.
  moves?: { places: { id: string; name: string }[]; threads: { id: string; name: string }[]; inScene: boolean };
};

export type Problem = { line: number; message: string } & Span;

export type Analysis = {
  lines: Line[];
  // Who the cue names, as an id; empty without a cue or with an unknown one.
  actorId: string;
  segments: Segment[];
  staging: Staging | null;
  // The staging line's actors, as ids, once every name is known and fits.
  staged: string[];
  // Where the entry takes the play, once the move reads whole; null to stay.
  direction: Direction | null;
  problems: Problem[];
};

const cueMark = /^(\s*)([^("`\s][^:]*?)\s*:\s*$/;
const stagingMark = /^(\s*)(enter|exit):(\s*)(.*?)\.?\s*$/i;
const withMark = /^(\s*)(with):(\s*)(.*?)\.?\s*$/i;
const moveMark = /^(\s*)(cut|split|converge)\s*(->|=>)(\s*)(.*?)\s*$/i;

// names reads "Mara, Fen and Innkeeper" starting at column at, each with its span.
function names(rest: string, at: number): Named[] {
  const out: Named[] = [];
  // Separators ride along as the odd parts of the split.
  for (const part of rest.split(/(\s*(?:,|\band\b)\s*)/i)) {
    const name = part.trim();
    if (name && !/^(,|and)$/i.test(name)) {
      const from = at + part.indexOf(name);
      out.push({ name, from, to: from + name.length });
    }
    at += part.length;
  }
  return out;
}

function readLine(raw: string, first: boolean): Line {
  if (!raw.trim()) return { kind: "blank" };
  const staging = stagingMark.exec(raw);
  if (staging) {
    const [, lead = "", type = "", gap = "", rest = ""] = staging;
    const who = names(rest, lead.length + type.length + 1 + gap.length);
    return { kind: "staging", staging: { type: type.toLowerCase() as Staging["type"], names: who.map((n) => n.name) }, names: who };
  }
  const withLine = withMark.exec(raw);
  if (withLine) {
    const [, lead = "", word = "", gap = "", rest = ""] = withLine;
    return { kind: "with", names: names(rest, lead.length + word.length + 1 + gap.length) };
  }
  const move = moveMark.exec(raw);
  if (move) {
    const [, lead = "", type = "", arrow = "", gap = "", rest = ""] = move;
    const at = lead.length + type.length + (raw.slice(lead.length + type.length).indexOf(arrow) + arrow.length) + gap.length;
    // A new place reads "Name: what it is | how it stands"; a known one is its name.
    const colon = arrow === "=>" ? rest.indexOf(":") : -1;
    const name = colon >= 0 ? rest.slice(0, colon).trim() : rest.trim();
    const tail = colon >= 0 ? rest.slice(colon + 1) : "";
    const bar = tail.indexOf("|");
    const description = (bar >= 0 ? tail.slice(0, bar) : tail).trim();
    const linkText = bar >= 0 ? tail.slice(bar + 1) : "";
    const linkFrom = at + (colon >= 0 ? colon + 1 : rest.length) + (bar >= 0 ? bar + 1 : tail.length) + (linkText.length - linkText.trimStart().length);
    return {
      kind: "move",
      move: {
        type: type.toLowerCase() as Move["type"],
        known: arrow === "->",
        target: { name, from: at + rest.indexOf(name), to: at + rest.indexOf(name) + name.length },
        description,
        link: { name: linkText.trim(), from: linkFrom, to: linkFrom + linkText.trim().length },
      },
    };
  }
  const cue = first ? cueMark.exec(raw) : null;
  if (cue) {
    const [, lead = "", name = ""] = cue;
    return { kind: "cue", name, from: lead.length, to: lead.length + name.length };
  }
  let kind: Kind = "narration";
  let from = raw.length - raw.trimStart().length;
  let to = raw.trimEnd().length;
  for (const [k, [open, close]] of Object.entries(marks) as [Exclude<Kind, "narration">, [string, string]][]) {
    if (raw.slice(from).startsWith(open)) {
      kind = k;
      from += open.length;
      if (raw.slice(from, to).endsWith(close)) to -= close.length;
      break;
    }
  }
  const inner = raw.slice(from, to);
  const text = inner.trim();
  from += inner.length - inner.trimStart().length;
  return { kind: "beat", segment: { kind, text }, from, to: from + text.length };
}

function readLines(text: string): Line[] {
  const lines: Line[] = [];
  let first = true;
  for (const raw of text.split("\n")) {
    const line = readLine(raw, first);
    if (line.kind !== "blank") first = false;
    lines.push(line);
  }
  return lines;
}

// analyze reads the text against its context and says what is wrong where.
// Everything the form does with the text comes from here, so what is shown
// and what is sent never disagree.
export function analyze(text: string, ctx: Context): Analysis {
  const lines = readLines(text);
  const problems: Problem[] = [];
  const byName = (name: string) => ctx.actors.find((a) => a.name.toLowerCase() === name.toLowerCase());
  const nameOf = (id: string) => ctx.actors.find((a) => a.id === id)?.name ?? "?";
  const flag = (line: number, span: Span, message: string) => problems.push({ line, ...span, message });
  const whole = (raw: string): Span => ({ from: 0, to: raw.trimEnd().length });
  const raws = text.split("\n");

  let actorId = "";
  const segments: Segment[] = [];
  let staging: Staging | null = null;
  let staged: string[] = [];
  let stagingAt = -1;
  let move: Move | null = null;
  let moveAt = -1;
  let withNames: Named[] | null = null;
  let withAt = -1;
  lines.forEach((line, i) => {
    const raw = raws[i] ?? "";
    if (moveAt >= 0 && (line.kind === "beat" || line.kind === "staging" || line.kind === "cue")) {
      flag(i, whole(raw), "the move goes last; nothing follows it");
      return;
    }
    switch (line.kind) {
      case "move":
        if (!ctx.moves) flag(i, whole(raw), ctx.editing ? "a beat cannot become a move" : "a move is written at the end of the script, in the composer");
        else if (move) flag(i, whole(raw), "one move at a time");
        else {
          move = line.move;
          moveAt = i;
        }
        break;
      case "with":
        if (!move) flag(i, whole(raw), "With: says who goes on a cut or split, under the move line");
        else if (withNames) flag(i, whole(raw), "one With: line");
        else {
          withNames = line.names;
          withAt = i;
        }
        break;
      case "cue": {
        const actor = byName(line.name);
        if (!actor) flag(i, line, `no actor named "${line.name}"`);
        else if (ctx.locked && actor.id !== ctx.locked) flag(i, line, `it is ${nameOf(ctx.locked)}'s turn`);
        else if (!ctx.present.includes(actor.id)) flag(i, line, `${actor.name} is not in the scene`);
        else actorId = actor.id;
        break;
      }
      case "beat":
        if (!line.segment.text) flag(i, whole(raw), `nothing in the ${line.segment.kind}`);
        else segments.push(line.segment);
        break;
      case "staging": {
        if (ctx.editing) {
          flag(i, whole(raw), "a beat cannot become staging; delete it and add a line instead");
          break;
        }
        if (staging) {
          flag(i, whole(raw), "one staging line at a time");
          break;
        }
        staging = line.staging;
        stagingAt = i;
        const ids: string[] = [];
        for (const n of line.names) {
          const actor = byName(n.name);
          if (!actor) flag(i, n, `no actor named "${n.name}"`);
          else if (ctx.present.includes(actor.id) === (line.staging.type === "enter")) {
            flag(i, n, `${actor.name} is ${line.staging.type === "enter" ? "already" : "not"} in the scene`);
          } else ids.push(actor.id);
        }
        if (line.names.length === 0) flag(i, whole(raw), `${line.staging.type} names nobody`);
        if (ids.length === line.names.length) staged = ids;
        break;
      }
    }
  });
  if (staging && segments.length > 0) {
    flag(stagingAt, whole(raws[stagingAt] ?? ""), "staging stands alone, with no beats beside it");
  }
  const direction = move && ctx.moves ? read(move, moveAt, withNames, withAt) : null;
  if (ctx.moves && !ctx.moves.inScene && !move && !staging && lines.some((l) => l.kind !== "blank")) {
    flag(lines.length - 1, { from: 0, to: Infinity }, "no scene yet: the entry opens the play with a cut somewhere");
  }
  return { lines, actorId, segments, staging, staged, direction, problems };

  // read turns the move and its With: line into the direction the entry
  // carries, flagging what does not fit; null until it reads whole.
  function read(m: Move, at: number, cast: Named[] | null, castAt: number): Direction | null {
    const moves = ctx.moves!;
    const raw = raws[at] ?? "";
    let bad = false;
    const miss = (span: Span, message: string) => {
      flag(at, span, message);
      bad = true;
    };
    if (staging) miss(whole(raw), "staging stands alone; no move with it");
    if (segments.length === 0) miss(whole(raw), "a move needs the entry that arrives under it");
    if (m.type === "converge") {
      if (!m.known) miss(whole(raw), "a converge names a thread, with ->");
      if (cast) {
        flag(castAt, whole(raws[castAt] ?? ""), "a converge takes everyone present; no With:");
        bad = true;
      }
      const thread = moves.threads.find((t) => t.name.toLowerCase() === m.target.name.toLowerCase());
      if (!thread) miss(m.target, m.target.name ? `no other open thread named "${m.target.name}"` : "which thread?");
      return bad || !thread ? null : { type: "converge", intoThreadId: thread.id };
    }
    let placeId = "";
    if (m.known) {
      const place = moves.places.find((p) => p.name.toLowerCase() === m.target.name.toLowerCase());
      if (!place) miss(m.target, m.target.name ? `no place named "${m.target.name}"` : "where to?");
      else placeId = place.id;
    } else {
      if (!m.target.name) miss(whole(raw), "a new place needs a name: Cut => Name: what it is | how it stands");
      if (moves.inScene && !m.link.name) miss(whole(raw), "say how the new place stands to the one left, after |");
    }
    let ids: string[] = ctx.present;
    if (cast) {
      ids = [];
      for (const n of cast) {
        const actor = byName(n.name);
        if (!actor) {
          flag(castAt, n, `no actor named "${n.name}"`);
          bad = true;
        } else if (!ids.includes(actor.id)) ids.push(actor.id);
      }
    } else if (m.type === "split") miss(whole(raw), "a split says who leaves: With: Name, Name");
    if (ids.length === 0 && !bad) miss(whole(raw), "nobody goes; name someone under With:");
    if (bad) return null;
    const place = m.known
      ? { placeId }
      : {
          newPlace: { name: m.target.name, description: m.description, actor: false, scene: true, user: false, tracked: false, attributes: [] },
          link: m.link.name,
        };
    return { type: m.type, ...place, cast: ids };
  }
}

// A name the caret may be completing: the partial name's span in the text,
// and the names it could be, those fitting what is typed first. A cue takes
// its colon along when none follows.
export type Completion = { from: number; to: number; options: string[]; suffix: string };

export function completions(text: string, caret: number, ctx: Context): Completion | null {
  const start = text.lastIndexOf("\n", caret - 1) + 1;
  const raw = text.slice(start, caret);
  const isFirst = !text.slice(0, start).trim();
  const after = text.slice(caret, text.indexOf("\n", caret) < 0 ? undefined : text.indexOf("\n", caret));
  const named = (names: string[], taken: string[] = []) =>
    names.filter((n) => !taken.some((t) => t.toLowerCase() === n.toLowerCase()));
  const actorNames = (ids: string[]) => ids.map((id) => ctx.actors.find((a) => a.id === id)?.name ?? "").filter(Boolean);
  let token = "";
  let options: string[] = [];
  let suffix = "";

  const list = /^(\s*)(enter|exit|with):(.*)$/i.exec(raw);
  const move = /^(\s*)(cut|split|converge)\s*->(.*)$/i.exec(raw);
  if (list) {
    const [, , word = "", rest = ""] = list;
    // The name under the caret starts after the last separator.
    const parts = rest.split(/(\s*(?:,|\band\b)\s*)/i);
    token = (parts.at(-1) ?? "").trimStart();
    const taken = parts.filter((_, i) => i % 2 === 0 && i < parts.length - 1).map((p) => p.trim());
    const absent = ctx.actors.filter((a) => !ctx.present.includes(a.id)).map((a) => a.name);
    const all = ctx.actors.map((a) => a.name);
    options = named(word.toLowerCase() === "enter" ? absent : word.toLowerCase() === "exit" ? actorNames(ctx.present) : all, taken);
  } else if (move && ctx.moves) {
    const [, , word = "", rest = ""] = move;
    token = rest.trimStart();
    options = (word.toLowerCase() === "converge" ? ctx.moves.threads : ctx.moves.places).map((p) => p.name);
  } else if (isFirst && /^\s*[^("`\s][^:]*$/.test(raw) && !raw.includes(":")) {
    token = raw.trimStart();
    options = actorNames(ctx.locked ? [ctx.locked] : ctx.present);
    suffix = after.trimStart().startsWith(":") ? "" : ":";
  } else return null;

  const fitting = options.filter((o) => o.toLowerCase().startsWith(token.toLowerCase()));
  if (fitting.length === 0) return null;
  return { from: caret - token.length, to: caret, options: fitting, suffix };
}

// Whether the text is an entry that can go: a beat or more under a cue, or
// one staging line, with nothing wrong.
export function complete(a: Analysis): boolean {
  return a.problems.length === 0 && (a.staging !== null || (a.actorId !== "" && a.segments.length > 0));
}

// formatDirection writes a direction as the move lines the composer reads,
// for a turn handed over with one; nothing for staying.
export function formatDirection(d: Direction | undefined, ctx: Context): string {
  if (!d || d.type === "narrative" || !ctx.moves) return "";
  const nameOf = (list: { id: string; name: string }[], id: string | undefined) => list.find((x) => x.id === id)?.name ?? "?";
  if (d.type === "converge") return `Converge -> ${nameOf(ctx.moves.threads, d.intoThreadId)}`;
  const word = d.type === "cut" ? "Cut" : "Split";
  const where = d.newPlace
    ? `${word} => ${d.newPlace.name}: ${d.newPlace.description}${d.link ? ` | ${d.link}` : ""}`
    : `${word} -> ${nameOf(ctx.moves.places, d.placeId)}`;
  const cast = (d.cast ?? []).map((id) => nameOf(ctx.actors, id));
  return cast.length ? `${where}\nWith: ${cast.join(", ")}` : where;
}

// parseScript reads only the beats, as a model's answer or a stored entry is read back.
export function parseScript(text: string): Segment[] {
  const segments: Segment[] = [];
  for (const line of readLines(text)) {
    if (line.kind === "beat" && line.segment.text) segments.push(line.segment);
  }
  return segments;
}

// formatScript is the text of stored beats under a cue, for editing in place.
export function formatScript(actor: string, segments: Segment[]): string {
  const beats = segments.map((s) => (s.kind === "narration" ? s.text : `${marks[s.kind][0]}${s.text}${marks[s.kind][1]}`));
  return [`${actor}:`, ...beats].join("\n");
}
