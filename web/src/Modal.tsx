import type { ReactNode, RefObject } from "react";

import styles from "./Modal.module.css";

type Props = {
  ref: RefObject<HTMLDialogElement | null>;
  title: string;
  narrow?: boolean;
  children: ReactNode;
};

// A dialog with a sticky header and a close button in the corner, so closing
// never requires scrolling. Open it with ref.current.showModal().
export function Modal({ ref, title, narrow, children }: Props) {
  return (
    <dialog ref={ref} className={narrow ? `${styles.modal} ${styles.narrow}` : styles.modal}>
      <header className={styles.header}>
        <h3>{title}</h3>
        <button type="button" className={styles.close} title="Close" onClick={() => ref.current?.close()}>
          ×
        </button>
      </header>
      <div className={styles.body}>{children}</div>
    </dialog>
  );
}
