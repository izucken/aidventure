import { useId, type ReactNode } from "react";

import type { Subject } from "./api";
import styles from "./EntryForm.module.css";
import { ScriptEditor } from "./ScriptEditor";

type Props = {
  actors: Subject[];
  actorId: string;
  text: string;
  onActor: (id: string) => void;
  onText: (text: string) => void;
  onSubmit: () => void;
  canSubmit: boolean;
  submitLabel: string;
  inline?: boolean;
  // The actor is fixed, as when a turn is being answered.
  lockActor?: boolean;
  // Rendered between the entry text and the buttons: the direction controls on the tail form.
  below?: ReactNode;
  extra?: ReactNode;
};

// One entry under construction: who acts, and the script text. Fields stack
// top to bottom with their labels; the buttons sit in a row at the end.
export function EntryForm({
  actors,
  actorId,
  text,
  onActor,
  onText,
  onSubmit,
  canSubmit,
  submitLabel,
  inline,
  lockActor,
  below,
  extra,
}: Props) {
  const id = useId();
  return (
    <form
      className={inline ? `${styles.form} ${styles.inline}` : styles.form}
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      <div className={styles.field}>
        <label htmlFor={id + "-actor"}>Actor</label>
        <select id={id + "-actor"} value={actorId} onChange={(e) => onActor(e.target.value)} disabled={lockActor}>
          <option value="">Who acts…</option>
          {actors.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </select>
      </div>
      <div className={styles.field}>
        <label htmlFor={id + "-text"}>Entry</label>
        <ScriptEditor id={id + "-text"} value={text} onChange={onText} onSubmit={onSubmit} />
      </div>
      {below}
      <div className={styles.actions}>
        <button type="submit" disabled={!canSubmit}>
          {submitLabel}
        </button>
        {extra}
      </div>
    </form>
  );
}
