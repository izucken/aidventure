import { Fragment, useEffect, useRef, useState } from "react";

import { api, type Direction, type Entry, type PlayEvent, type Preview, type Scene, type Subject, type Thread, type Turn } from "./api";
import styles from "./Entries.module.css";
import { EntryForm } from "./EntryForm";
import { InlineName } from "./InlineName";
import { Modal } from "./Modal";
import { analyze, complete, formatDirection, formatScript, parseScript, type Context } from "./script";
import deletable from "./shared/deletable.module.css";
import kinds from "./shared/segments.module.css";
import { SyntaxHelp } from "./SyntaxHelp";

type Props = {
  gameId: string;
  // The role whoever directs plays, as the game names it; empty means director.
  director: string;
  threadId: string;
  threads: Thread[];
  scenes: Scene[];
  subjects: Subject[];
  // Called after a transition, so the scene list can refresh.
  onChange: () => Promise<void>;
  // Called when the play went on in another thread, to follow it there.
  onMoved: (threadId: string) => Promise<void>;
  onDeleteScene: (id: string) => Promise<void>;
  onError: (err: unknown) => void;
};


const phaseLabel: Record<PlayEvent["phase"], string> = { direct: "Directing…", entry: "Writing…" };

// The thread as one continuous script: every scene in order under its slug
// line. An entry edits in place when picked; the composer at the end appends
// to the latest scene, or takes the play out of it.
export function Entries({ gameId, director, threadId, threads, scenes, subjects, onChange, onMoved, onDeleteScene, onError }: Props) {
  const [entries, setEntries] = useState<Entry[]>([]);
  const [editing, setEditing] = useState<string | null>(null);
  // The entry after which a new one is being written, in the gap below it.
  const [inserting, setInserting] = useState<string | null>(null);
  // Drafts are text alone; who acts is the cue line.
  const [insert, setInsert] = useState("");
  const [edit, setEdit] = useState("");
  const [draft, setDraft] = useState("");
  const [thought, setThought] = useState<{ title: string; text: string } | null>(null);
  const thinking = useRef<HTMLDialogElement>(null);
  const [rolling, setRolling] = useState(false);
  const inFlight = useRef<AbortController | null>(null);
  const [live, setLive] = useState<{ phase: PlayEvent["phase"]; think: string; text: string } | null>(null);
  const [turn, setTurn] = useState<Turn | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const debug = useRef<HTMLDialogElement>(null);
  const help = useRef<HTMLDialogElement>(null);

  function showThought(title: string, text: string) {
    setThought({ title, text });
    thinking.current?.showModal();
  }

  const actors = subjects.filter((s) => s.actor);
  const nameOf = (id: string) => subjects.find((s) => s.id === id)?.name ?? "?";
  const thread = threads.find((t) => t.id === threadId);
  const ended = Boolean(thread?.endedByEntryId);
  const others = threads.filter((t) => t.id !== threadId && !t.endedByEntryId);
  const latest = scenes.at(-1);
  const sceneId = latest?.id ?? "";
  // Who is in the scene: its staging lines folded, up to and including the
  // entry upTo, or all of them. Folded here from the entries, so nothing has
  // to be kept in step with them.
  function castAt(sc: Scene | undefined, upTo?: string): string[] {
    const cast: string[] = [];
    for (const e of entries) {
      if (e.sceneId !== sc?.id) continue;
      for (const id of e.actors ?? []) {
        if (e.type === "exit") {
          const at = cast.indexOf(id);
          if (at >= 0) cast.splice(at, 1);
        } else if ((e.type === "present" || e.type === "enter") && !cast.includes(id)) {
          cast.push(id);
        }
      }
      if (e.id === upTo) break;
    }
    return cast;
  }
  const presentIn = (sc: Scene | undefined) => actors.filter((a) => castAt(sc).includes(a.id));
  // What a text written at a point of the scene is read against.
  const context = (sc: Scene | undefined, upTo?: string, more: Partial<Context> = {}): Context => ({ actors, present: castAt(sc, upTo), ...more });
  // Where the composer may take the play: the game's places, the other open threads, and whether there is a scene to stay in.
  const moves = { places: subjects.filter((s) => s.scene), threads: others, inScene: Boolean(sceneId) };
  const tailContext = context(latest, undefined, { ...(turn?.user ? { locked: turn.actorId } : {}), moves });
  const tail = analyze(draft, tailContext);

  // Records who entered or exited: at the end of the scene, or right after an
  // entry. The scenes reload too, since a cut's default cast comes from them.
  async function stage(sc: Scene, type: "enter" | "exit", ids: string[], afterId?: string) {
    await api.presence(gameId, threadId, sc.id, type, ids, afterId);
    await Promise.all([reload(), onChange()]);
  }

  async function retitle(sc: Scene, title: string) {
    try {
      await api.retitleScene(gameId, threadId, sc.id, title);
      await onChange();
    } catch (err) {
      onError(err);
    }
  }

  useEffect(() => {
    setEntries([]);
    setEditing(null);
    setDraft("");
    setTurn(null);
    api.threadEntries(gameId, threadId).then(setEntries).catch(onError);
    api.turn(gameId, threadId).then(setTurn).catch(onError);
  }, [gameId, threadId, onError]);

  // Scenes change on transitions and deletes; the script follows.
  useEffect(() => {
    api.threadEntries(gameId, threadId).then(setEntries).catch(onError);
  }, [gameId, threadId, scenes, onError]);

  // A turn handed to a human comes with its direction; the composer takes it
  // up as move lines under the cue, with room between for the entry.
  useEffect(() => {
    if (!turn?.user) return;
    const lines = formatDirection(turn.direction, context(latest, undefined, { moves }));
    if (lines) setDraft(`${nameOf(turn.actorId)}:\n\n${lines}`);
  }, [turn]);

  // A blank composer gets its cue: whoever user-controlled acted last, else
  // the first user-controlled actor. A locked turn puts its actor there as
  // long as nothing else is written. Otherwise the text is the author's.
  useEffect(() => {
    const written = tail.lines.some((l) => l.kind !== "blank" && l.kind !== "cue");
    let pick = "";
    if (turn?.user) {
      if (written || tail.actorId === turn.actorId) return;
      pick = turn.actorId;
    } else {
      if (draft.trim() !== "") return;
      const present = presentIn(latest);
      const isUser = (id: string) => present.some((a) => a.id === id && a.user);
      const last = [...entries].reverse().find((e) => e.actorId && isUser(e.actorId))?.actorId;
      pick = last ?? present.find((a) => a.user)?.id ?? "";
    }
    const want = pick ? `${nameOf(pick)}:\n` : "";
    if (draft !== want) setDraft(want);
  }, [entries, subjects, scenes, latest, draft, turn]);

  async function reload() {
    setEntries(await api.threadEntries(gameId, threadId));
  }

  function pick(entry: Entry) {
    setEditing(entry.id);
    setEdit(formatScript(nameOf(entry.actorId ?? ""), entry.segments ?? []));
  }

  // An edit is read where the entry stands: who is there at that point, and no staging in a beat's place.
  const target = entries.find((e) => e.id === editing);
  const editContext = context(scenes.find((s) => s.id === target?.sceneId), target?.id, { editing: true });
  const edited = analyze(edit, editContext);

  async function save() {
    if (!editing || !target || !complete(edited)) return;
    try {
      await api.updateEntry(gameId, threadId, target.sceneId, editing, edited.actorId, edited.segments);
      await reload();
      setEditing(null);
    } catch (err) {
      onError(err);
    }
  }

  const canSubmit = complete(tail);

  // Follows the play to where an entry landed: the scene list here, or another thread.
  async function landed(threadOfEntry: string | undefined) {
    if (threadOfEntry && threadOfEntry !== threadId) await onMoved(threadOfEntry);
    else await onChange();
  }

  async function insertAfter(sc: Scene, afterId: string) {
    const a = analyze(insert, context(sc, afterId));
    if (!complete(a)) return;
    try {
      if (a.staging) await stage(sc, a.staging.type, a.staged, afterId);
      else {
        await api.appendEntry(gameId, threadId, sc.id, a.actorId, a.segments, afterId);
        await reload();
      }
      setInserting(null);
      setInsert("");
    } catch (err) {
      onError(err);
    }
  }

  async function append() {
    // Ctrl+Enter on an empty composer moves the play instead of adding nothing.
    const empty = !tail.lines.some((l) => l.kind !== "blank" && l.kind !== "cue");
    if (empty && sceneId && !turn?.user && !rolling) {
      await advance();
      return;
    }
    if (!canSubmit) return;
    try {
      if (tail.staging && latest) {
        await stage(latest, tail.staging.type, tail.staged);
      } else if (!tail.direction) {
        await api.appendEntry(gameId, threadId, sceneId, tail.actorId, tail.segments);
        await reload();
      } else {
        const res = await api.transition(gameId, threadId, { direction: tail.direction, actorId: tail.actorId, segments: tail.segments });
        await landed(res.threadId);
      }
      setDraft("");
      if (turn?.actorId === tail.actorId) setTurn(null);
    } catch (err) {
      onError(err);
    }
  }

  async function advance() {
    const controller = new AbortController();
    inFlight.current = controller;
    setRolling(true);
    setLive({ phase: "direct", think: "", text: "" });
    try {
      const out = await api.advance(
        gameId,
        threadId,
        (ev) =>
          setLive((l) => {
            const base = l && l.phase === ev.phase ? l : { phase: ev.phase, think: "", text: "" };
            return ev.part === "think" ? { ...base, think: base.think + ev.delta } : { ...base, text: base.text + ev.delta };
          }),
        controller.signal,
      );
      setTurn(out.turn ?? null);
      await landed(out.threadId);
    } catch (err) {
      if (!controller.signal.aborted) onError(err);
    } finally {
      inFlight.current = null;
      setRolling(false);
      setLive(null);
    }
  }

  function cancel() {
    inFlight.current?.abort();
  }

  // A removal is known exactly, so the list drops the entry rather than reloading.
  function dropped(id: string) {
    setEntries((es) => es.filter((e) => e.id !== id));
    if (id === editing) setEditing(null);
    if (id === turn?.entryId) setTurn(null);
  }

  async function pass() {
    if (!turn) return;
    try {
      await api.pass(gameId, threadId);
      dropped(turn.entryId);
    } catch (err) {
      onError(err);
    }
  }

  async function remove(entry: Entry) {
    try {
      await api.deleteEntry(gameId, threadId, entry.sceneId, entry.id);
      dropped(entry.id);
      // A staging line gone changes who is there; the scenes carry that too.
      if (entry.type === "enter" || entry.type === "exit") await onChange();
    } catch (err) {
      onError(err);
    }
  }

  async function showPrompt() {
    if (!tail.actorId || !sceneId) return;
    try {
      setPreview(await api.prompt(gameId, threadId, sceneId, tail.actorId));
      debug.current?.showModal();
    } catch (err) {
      onError(err);
    }
  }

  // Says where a direction takes the play, for the turn notice and the director's entry.
  function describe(d: Direction | undefined): string {
    if (!d || d.type === "narrative") return "";
    if (d.type === "converge") return ` The play converges into ${threads.find((t) => t.id === d.intoThreadId)?.name ?? "another thread"}.`;
    const where = d.newPlace ? `${d.newPlace.name} (new)` : nameOf(d.placeId ?? "");
    const who = (d.cast ?? []).map(nameOf).join(", ");
    return d.type === "cut" ? ` The play cuts to ${where}; present: ${who}.` : ` ${who} split off to ${where}.`;
  }

  const submitLabel = tail.direction ? tail.direction.type[0]!.toUpperCase() + tail.direction.type.slice(1) : "Add entry";

  return (
    <section>
      {scenes.map((sc, i) => (
        <details key={sc.id} open className={`${styles.scene} ${deletable.deletable}`}>
          <summary className={styles.slug}>
            <span className={styles.n}>{i + 1}</span>{" "}
            <InlineName value={sc.title ?? ""} placeholder={nameOf(sc.placeId)} label="Scene title" allowEmpty onSave={(title) => retitle(sc, title)} />
            {sc.title && <span className={styles.place}>{nameOf(sc.placeId)}</span>}
          </summary>
          <button
            type="button"
            className={deletable.delete}
            title="Delete scene and its entries"
            onClick={() => void onDeleteScene(sc.id)}
          >
            ×
          </button>
          {entries
            .filter((e) => e.sceneId === sc.id)
            .map((entry) => (
              <Fragment key={entry.id}>
                {entry.type === "present" || entry.type === "enter" || entry.type === "exit" ? (
                  <article
                    className={`${styles.entry} ${styles.presence} ${deletable.deletable}`}
                    data-interactive={entry.type !== "present" ? "" : undefined}
                  >
                    <p>
                      {entry.type === "present" ? "Present: " : entry.type === "enter" ? "Enter: " : "Exit: "}
                      {(entry.actors ?? []).map(nameOf).join(", ") || "nobody"}
                      {entry.type !== "present" && "."}
                    </p>
                    {entry.type !== "present" && (
                      <button type="button" className={deletable.delete} title={`Delete ${entry.type}`} onClick={() => void remove(entry)}>
                        ×
                      </button>
                    )}
                  </article>
                ) : entry.type === "direction" ? (
                  <article className={`${styles.entry} ${styles.direction} ${deletable.deletable}`} data-interactive="">
                    <h4 className={styles.cue}>
                      {director || "Director"}
                      {entry.think && (
                        <button
                          type="button"
                          className={styles.thinkToggle}
                          title="Show thinking"
                          onClick={() => showThought(`${director || "Director"} thinking`, entry.think ?? "")}
                        >
                          💭
                        </button>
                      )}
                    </h4>
                    <button type="button" className={deletable.delete} title="Delete direction" onClick={() => void remove(entry)}>
                      ×
                    </button>
                    <p>
                      {(describe(entry.direction) || " The scene goes on.").trim()} Next: {nameOf(entry.direction?.actorId ?? "")}.
                    </p>
                  </article>
                ) : entry.id === editing ? (
                  <article className={`${styles.entry} ${styles.editing}`}>
                    <EntryForm
                      text={edit}
                      analysis={edited}
                      context={editContext}
                      onText={setEdit}
                      onSubmit={() => void save()}
                      canSubmit={complete(edited)}
                      submitLabel="Save"
                      inline
                      extra={
                        <button type="button" onClick={() => setEditing(null)}>
                          Cancel
                        </button>
                      }
                    />
                  </article>
                ) : (
                  <article
                    className={`${styles.entry} ${deletable.deletable}`}
                    data-interactive=""
                    onClick={() => pick(entry)}
                  >
                    <h4 className={styles.cue}>
                      {nameOf(entry.actorId ?? "")}
                      {entry.type !== "narrative" && <span className={styles.move}>{entry.type}</span>}
                      {entry.think && (
                        <button
                          type="button"
                          className={styles.thinkToggle}
                          title="Show thinking"
                          onClick={(e) => {
                            e.stopPropagation();
                            showThought(`${nameOf(entry.actorId ?? "")} thinking`, entry.think ?? "");
                          }}
                        >
                          💭
                        </button>
                      )}
                    </h4>
                    {entry.type === "narrative" && (
                      <button
                        type="button"
                        className={deletable.delete}
                        title="Delete entry"
                        onClick={(e) => {
                          e.stopPropagation();
                          void remove(entry);
                        }}
                      >
                        ×
                      </button>
                    )}
                    {(entry.segments ?? []).map((seg, i) => (
                      <p key={i} className={kinds[seg.kind]}>
                        {seg.text}
                      </p>
                    ))}
                  </article>
                )}
                {inserting === entry.id ? (
                  <article className={`${styles.entry} ${styles.editing}`}>
                    <EntryForm
                      text={insert}
                      analysis={analyze(insert, context(sc, entry.id))}
                      context={context(sc, entry.id)}
                      onText={setInsert}
                      onSubmit={() => void insertAfter(sc, entry.id)}
                      canSubmit={complete(analyze(insert, context(sc, entry.id)))}
                      submitLabel="Insert"
                      inline
                      extra={
                        <button type="button" onClick={() => setInserting(null)}>
                          Cancel
                        </button>
                      }
                    />
                  </article>
                ) : (
                  <div
                    className={styles.gap}
                    data-interactive=""
                    role="button"
                    tabIndex={0}
                    title="Insert an entry here"
                    onClick={() => {
                      setInserting(entry.id);
                      const first = castAt(sc, entry.id)[0];
                      setInsert(first ? `${nameOf(first)}:\n` : "");
                    }}
                  >
                    <span aria-hidden="true">+</span>
                  </div>
                )}
              </Fragment>
            ))}
        </details>
      ))}
      {scenes.length === 0 && <p>No scene yet. Write the opening entry and cut to a place.</p>}
      {live && (
        <article className={styles.live}>
          <h4 className={styles.cue}>
            {phaseLabel[live.phase]}
            <button type="button" className={styles.liveCancel} onClick={cancel}>
              Cancel
            </button>
          </h4>
          {live.think && <pre className={styles.liveThink}>{live.think}</pre>}
          {live.text && live.phase !== "entry" && <pre className={styles.liveThink}>{live.text}</pre>}
          {live.text &&
            live.phase === "entry" &&
            parseScript(live.text).map((seg, i) => (
              <p key={i} className={kinds[seg.kind]}>
                {seg.text}
              </p>
            ))}
        </article>
      )}
      {ended && <p className={styles.turn}>This thread has ended; its characters went on in another thread.</p>}
      {turn?.user && (
        <div className={styles.turn}>
          <p>
            {nameOf(turn.actorId)}'s turn.{describe(turn.direction)} Write the entry, or pass.
            {turn.think && (
              <button
                type="button"
                className={styles.thinkToggle}
                title="Why this actor"
                onClick={() => showThought(`Why ${nameOf(turn.actorId)}`, turn.think ?? "")}
              >
                💭
              </button>
            )}
          </p>
        </div>
      )}
      {!ended && (
        <EntryForm
          text={draft}
          analysis={tail}
          context={tailContext}
          onText={setDraft}
          onSubmit={() => void append()}
          canSubmit={canSubmit}
          submitLabel={submitLabel}
          extra={
            <>
              {turn?.user ? (
                <button type="button" onClick={() => void pass()}>
                  Pass
                </button>
              ) : (
                <button type="button" onClick={() => void advance()} disabled={!sceneId || rolling}>
                  {rolling ? "Playing…" : "Advance"}
                </button>
              )}
              {rolling && (
                <button type="button" onClick={cancel}>
                  Cancel
                </button>
              )}
              <button type="button" className={styles.aside} onClick={() => help.current?.showModal()} title="How an entry is written">
                Help
              </button>
              <button type="button" className={styles.debug} onClick={() => void showPrompt()} disabled={!tail.actorId || !sceneId}>
                Debug
              </button>
            </>
          }
        />
      )}
      <SyntaxHelp ref={help} />
      <Modal ref={thinking} title={thought?.title ?? "Thinking"}>
        <pre className={styles.think}>{thought?.text}</pre>
      </Modal>
      <Modal ref={debug} title={preview ? `What ${preview.model} will see` : "Prompt"}>
        {preview && (
          <>
            <h4>Prompt, after the model's template</h4>
            <pre>{preview.rendered.prompt}</pre>
            <h4>Schema, as sent</h4>
            <pre>{preview.rendered.schema || "(none)"}</pre>
            <h4>Request parts</h4>
            <h5>System</h5>
            <pre>{preview.request.System}</pre>
            <h5>User</h5>
            <pre>{preview.request.User}</pre>
          </>
        )}
      </Modal>
    </section>
  );
}
