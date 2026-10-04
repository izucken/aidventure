import { useEffect, useRef, useState } from "react";

import styles from "./Toasts.module.css";

export type Toast = { id: number; text: string };

type Props = {
  toasts: Toast[];
  onDismiss: (id: number) => void;
};

// How long a toast stays unless held.
const ttl = 8000;

// Errors as a stack of toasts in the corner. Each goes away on its own after a
// while, but not while the pointer or focus is on it, and can be dismissed.
export function Toasts({ toasts, onDismiss }: Props) {
  return (
    <div className={styles.stack} aria-live="polite">
      {toasts.map((t) => (
        <ToastView key={t.id} toast={t} onDismiss={onDismiss} />
      ))}
    </div>
  );
}

function ToastView({ toast, onDismiss }: { toast: Toast; onDismiss: (id: number) => void }) {
  const [held, setHeld] = useState(false);
  // Time left runs down only while the toast is not held.
  const left = useRef(ttl);

  useEffect(() => {
    if (held) return;
    const since = Date.now();
    const timer = setTimeout(() => onDismiss(toast.id), left.current);
    return () => {
      clearTimeout(timer);
      left.current = Math.max(0, left.current - (Date.now() - since));
    };
  }, [held, toast.id, onDismiss]);

  return (
    <div
      role="alert"
      className={styles.toast}
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)}
      onBlur={() => setHeld(false)}
    >
      <span className={styles.text}>{toast.text}</span>
      <button type="button" className={styles.close} title="Dismiss" onClick={() => onDismiss(toast.id)}>
        ×
      </button>
    </div>
  );
}
