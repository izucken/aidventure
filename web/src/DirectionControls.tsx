import type { Direction, Subject, Thread } from "./api";
import styles from "./DirectionControls.module.css";

// Where the entry takes the play: nowhere, or out of the scene by a cut, a
// split or a converge. A cut and a split go to a place, "new" being one made
// now; a converge goes to a thread.
export type DirectionDraft = {
  type: "" | "cut" | "split" | "converge";
  target: string;
  name: string;
  description: string;
  // How a new place stands to the place left, in the author's words.
  link: string;
  cast: string[];
};

export const stay: DirectionDraft = { type: "", target: "", name: "", description: "", link: "", cast: [] };

export function toDirection(d: DirectionDraft): Direction {
  if (d.type === "converge") return { type: "converge", intoThreadId: d.target };
  const place =
    d.target === "new"
      ? { newPlace: { name: d.name, description: d.description, actor: false, scene: true, user: false, tracked: false }, link: d.link }
      : { placeId: d.target };
  return { type: d.type || "narrative", ...place, cast: d.cast };
}

export function fromDirection(d: Direction | undefined): DirectionDraft {
  if (!d || d.type === "narrative") return stay;
  if (d.type === "converge") return { ...stay, type: "converge", target: d.intoThreadId ?? "" };
  return {
    type: d.type,
    target: d.newPlace ? "new" : (d.placeId ?? ""),
    name: d.newPlace?.name ?? "",
    description: d.newPlace?.description ?? "",
    link: d.link ?? "",
    cast: d.cast ?? [],
  };
}

// canStay also says there is a place being left, which a new place must be linked to.
export function directionReady(d: DirectionDraft, canStay: boolean): boolean {
  if (d.type === "") return canStay;
  if (d.type === "converge") return d.target !== "";
  if (d.cast.length === 0 || d.target === "") return false;
  return d.target !== "new" || (d.name.trim() !== "" && (!canStay || d.link.trim() !== ""));
}

type Props = {
  subjects: Subject[];
  // Other open threads, the ones a converge can join.
  threads: Thread[];
  value: DirectionDraft;
  // Without a scene to stay in, the entry must cut somewhere.
  canStay: boolean;
  // The place being left, which a new place is linked to.
  from?: string;
  onChange: (value: DirectionDraft) => void;
};

export function DirectionControls({ subjects, threads, value, canStay, from, onChange }: Props) {
  const places = subjects.filter((s) => s.scene);
  const actors = subjects.filter((s) => s.actor);
  const toPlace = value.type === "cut" || value.type === "split";

  function toggle(id: string, on: boolean) {
    const cast = on ? [...value.cast, id] : value.cast.filter((c) => c !== id);
    onChange({ ...value, cast });
  }

  return (
    <div className={styles.direction}>
      <div className={styles.choice}>
        <select
          value={value.type}
          onChange={(e) => onChange({ ...value, type: e.target.value as DirectionDraft["type"], target: "" })}
          aria-label="Where the play goes"
        >
          <option value="" disabled={!canStay}>
            {canStay ? "Stay in this scene" : "Open the play with…"}
          </option>
          <option value="cut">Cut to a place</option>
          <option value="split" disabled={!canStay}>
            Split some off to a place
          </option>
          <option value="converge" disabled={threads.length === 0}>
            Converge into a thread
          </option>
        </select>
        {toPlace && (
          <select value={value.target} onChange={(e) => onChange({ ...value, target: e.target.value })} aria-label="Place">
            <option value="">Where…</option>
            {places.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
            <option value="new">A new place…</option>
          </select>
        )}
        {value.type === "converge" && (
          <select value={value.target} onChange={(e) => onChange({ ...value, target: e.target.value })} aria-label="Thread">
            <option value="">Which thread…</option>
            {threads.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
        )}
      </div>
      {toPlace && value.target === "new" && (
        <div className={styles.newPlace}>
          <input
            value={value.name}
            onChange={(e) => onChange({ ...value, name: e.target.value })}
            placeholder="Place name"
            aria-label="New place name"
          />
          <textarea
            value={value.description}
            onChange={(e) => onChange({ ...value, description: e.target.value })}
            placeholder="What the place is"
            aria-label="New place description"
            rows={2}
          />
          {from && (
            <input
              value={value.link}
              onChange={(e) => onChange({ ...value, link: e.target.value })}
              placeholder={`How it stands to , as in "down the steps from"`}
              aria-label={`How the new place stands to `}
            />
          )}
        </div>
      )}
      {toPlace && (
        <fieldset className={styles.cast}>
          <legend>{value.type === "cut" ? "Present" : "Leaving"}</legend>
          {actors.map((a) => (
            <label key={a.id}>
              <input type="checkbox" checked={value.cast.includes(a.id)} onChange={(e) => toggle(a.id, e.target.checked)} />{" "}
              {a.name}
            </label>
          ))}
        </fieldset>
      )}
    </div>
  );
}
