import { useRef, useState, type FormEvent } from "react";

import { Modal } from "./Modal";
import styles from "./Picker.module.css";
import deletable from "./shared/deletable.module.css";

type Item = { id: string; name: string };

type Props = {
  items: Item[];
  currentId: string | null;
  placeholder: string;
  onSelect: (id: string) => void;
  onCreate: (name: string) => Promise<void>;
  horizontal?: boolean;
  // With a title, creation moves into a dialog opened by a + at the head of the list.
  createTitle?: string;
  // With a handler, each item carries a delete in its corner.
  onDelete?: (id: string) => Promise<void>;
  // What an item is, for the delete's tooltip.
  noun?: string;
};

// A list of named things with one current, plus a one-field form to add another.
export function Picker({ items, currentId, placeholder, onSelect, onCreate, horizontal, createTitle, onDelete, noun }: Props) {
  const [name, setName] = useState("");
  const dialog = useRef<HTMLDialogElement>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    await onCreate(trimmed);
    setName("");
    dialog.current?.close();
  }

  const form = (
    <form className={styles.form} onSubmit={(e) => void submit(e)}>
      <input
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        autoFocus={Boolean(createTitle)}
      />
      <button type="submit" disabled={!name.trim()}>
        Create
      </button>
    </form>
  );

  return (
    <div className={horizontal ? styles.horizontal : undefined}>
      <ul className={styles.list}>
        {createTitle && (
          <li>
            <button
              type="button"
              className={`${styles.item} ${styles.add}`}
              title={createTitle}
              onClick={() => dialog.current?.showModal()}
            >
              +
            </button>
          </li>
        )}
        {items.map((item) => (
          <li key={item.id} className={onDelete ? deletable.deletable : undefined}>
            <button
              type="button"
              className={onDelete ? `${styles.item} ${styles.roomy}` : styles.item}
              aria-current={item.id === currentId ? "true" : undefined}
              onClick={() => onSelect(item.id)}
            >
              {item.name}
            </button>
            {onDelete && (
              <button type="button" className={deletable.delete} title={`Delete ${noun ?? "item"}`} onClick={() => void onDelete(item.id)}>
                ×
              </button>
            )}
          </li>
        ))}
      </ul>
      {createTitle ? (
        <Modal ref={dialog} title={createTitle} narrow>
          {form}
        </Modal>
      ) : (
        form
      )}
    </div>
  );
}
