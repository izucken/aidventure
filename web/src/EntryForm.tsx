import { useId, type ReactNode } from "react";

import styles from "./EntryForm.module.css";
import type { Analysis, Context } from "./script";
import { ScriptEditor } from "./ScriptEditor";

type Props = {
  text: string;
  // The text as read against its context; the parent reads it too, to send it.
  analysis: Analysis;
  // What the analysis read the text against; the editor completes names from it.
  context: Context;
  onText: (text: string) => void;
  onSubmit: () => void;
  // Whether what surrounds the text allows sending; the text itself must be complete too.
  canSubmit: boolean;
  submitLabel: string;
  inline?: boolean;
  extra?: ReactNode;
};

// One entry under construction: the script text, who acts in its cue line,
// with the buttons in a row at the end.
export function EntryForm({ text, analysis, context, onText, onSubmit, canSubmit, submitLabel, inline, extra }: Props) {
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
        <label htmlFor={id + "-text"}>Entry</label>
        <ScriptEditor id={id + "-text"} value={text} analysis={analysis} context={context} onChange={onText} onSubmit={onSubmit} />
      </div>
      <div className={styles.actions}>
        <button type="submit" disabled={!canSubmit}>
          {submitLabel}
        </button>
        {extra}
      </div>
    </form>
  );
}
