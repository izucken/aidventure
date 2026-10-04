import { useState, type KeyboardEvent, type MouseEvent } from "react";

import styles from "./InlineName.module.css";

type Props = {
  value: string;
  // Shown in place of an empty value, dimmed: what the thing goes by otherwise.
  placeholder?: string;
  label: string;
  onSave: (value: string) => Promise<void>;
  // The element the resting text is: a heading where the name is one.
  as?: "h1" | "h2" | "span";
  // Whether saving an empty value is meaningful, as for a title that can be taken away.
  allowEmpty?: boolean;
  className?: string;
};

// A name edited where it stands: pick it to get a field, Enter or Ctrl+Enter
// keeps the change, Esc drops it, and leaving the field keeps it too.
export function InlineName({ value, placeholder, label, onSave, as: Tag = "span", allowEmpty, className }: Props) {
  const [draft, setDraft] = useState<string | null>(null);

  // Inside a summary, a click would also toggle the details; the name takes it.
  function start(e: MouseEvent | KeyboardEvent) {
    e.preventDefault();
    e.stopPropagation();
    setDraft(value);
  }

  async function finish() {
    if (draft === null) return;
    const next = draft.trim();
    setDraft(null);
    if (next === value || (next === "" && !allowEmpty)) return;
    await onSave(next);
  }

  function key(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter") {
      e.preventDefault();
      void finish();
    } else if (e.key === "Escape") {
      e.preventDefault();
      setDraft(null);
    }
  }

  if (draft !== null) {
    return (
      <input
        className={`${styles.input} ${className ?? ""}`}
        value={draft}
        placeholder={placeholder}
        aria-label={label}
        autoFocus
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={key}
        onBlur={() => void finish()}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
        }}
      />
    );
  }
  return (
    <Tag
      className={`${styles.name} ${value ? "" : styles.empty} ${className ?? ""}`}
      data-interactive=""
      role="button"
      tabIndex={0}
      title={`Edit ${label.toLowerCase()}`}
      onClick={start}
      onKeyDown={(e) => {
        if (e.key === "Enter") start(e);
      }}
    >
      {value || placeholder}
    </Tag>
  );
}
