import type { KeyboardEvent } from "react";

import { parseScript } from "./script";
import styles from "./ScriptEditor.module.css";
import kinds from "./shared/segments.module.css";

type Props = {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
};

// One line per segment. Start a line with ( for action, " for speech, ` for
// thought; anything else is narration. Enter closes the segment, Ctrl+Enter submits.
export function ScriptEditor({ id, value, onChange, onSubmit }: Props) {
  function keyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      onSubmit();
    }
  }

  const preview = parseScript(value);

  return (
    <div className={styles.editor}>
      <textarea
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={keyDown}
        rows={6}
        placeholder={'(action)\n"speech"\n`thought`\nnarration'}
      />
      <div className={styles.preview}>
        {preview.map((seg, i) => (
          <p key={i} className={kinds[seg.kind]}>
            {seg.text}
          </p>
        ))}
      </div>
    </div>
  );
}
