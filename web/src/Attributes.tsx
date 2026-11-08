import { useEffect, useState, type FormEvent } from "react";

import { api, type Attribute, type AttributeInput, type AttributeKind } from "./api";
import styles from "./Attributes.module.css";
import badge from "./shared/badge.module.css";
import deletable from "./shared/deletable.module.css";
import list from "./shared/listItem.module.css";
import panes from "./shared/panes.module.css";

type Props = {
  gameId: string;
  attributes: Attribute[];
  onChange: () => Promise<void>;
  onError: (err: unknown) => void;
};

const kinds: { id: AttributeKind; label: string }[] = [
  { id: "flag", label: "Flag: yes or no" },
  { id: "integer", label: "Integer: a whole number" },
  { id: "float", label: "Float: a decimal number" },
  { id: "enum", label: "Enum: one of a list" },
];

const empty: AttributeInput = { name: "", description: "", kind: "flag", options: [] };

// The game's declared attributes: the typed, described qualities its
// subjects and links may carry. The list on the left picks one; the right
// edits it. Options are typed one per line and only mean something on enums.
export function Attributes({ gameId, attributes, onChange, onError }: Props) {
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState<AttributeInput>(empty);
  const [options, setOptions] = useState("");

  useEffect(() => {
    setEditing(null);
    setDraft(empty);
    setOptions("");
  }, [gameId]);

  function pick(a: Attribute) {
    setEditing(a.id);
    setDraft({ name: a.name, description: a.description, kind: a.kind, options: a.options });
    setOptions(a.options.join("\n"));
  }

  function reset() {
    setEditing(null);
    setDraft(empty);
    setOptions("");
  }

  async function remove(id: string) {
    try {
      await api.deleteAttribute(gameId, id);
      await onChange();
      if (id === editing) reset();
    } catch (err) {
      onError(err);
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    const input: AttributeInput = {
      ...draft,
      name: draft.name.trim(),
      description: draft.description.trim(),
      options: draft.kind === "enum" ? options.split("\n").map((o) => o.trim()).filter(Boolean) : [],
    };
    if (!input.name) return;
    try {
      if (editing) {
        await api.updateAttribute(gameId, editing, input);
      } else {
        const made = await api.createAttribute(gameId, input);
        setEditing(made.id);
      }
      await onChange();
    } catch (err) {
      onError(err);
    }
  }

  return (
    <section className={panes.view}>
      <div>
        <ul className={list.list}>
          <li>
            <button type="button" className={list.row} aria-current={editing === null ? "true" : undefined} onClick={reset}>
              <strong>+ New attribute</strong>
            </button>
          </li>
          {attributes.map((a) => (
            <li key={a.id} className={deletable.deletable}>
              <button type="button" className={list.row} aria-current={a.id === editing ? "true" : undefined} onClick={() => pick(a)}>
                <strong>
                  {a.name}
                  <em className={badge.badge} title={kinds.find((k) => k.id === a.kind)?.label}>
                    {a.kind}
                  </em>
                </strong>
                {a.description && <span className={list.meta}>{a.description}</span>}
              </button>
              <button type="button" className={deletable.delete} title="Delete attribute" onClick={() => void remove(a.id)}>
                ×
              </button>
            </li>
          ))}
        </ul>
      </div>
      <div className={panes.detail}>
        <form className={panes.form} onSubmit={(e) => void submit(e)}>
          <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} placeholder="Name" aria-label="Attribute name" />
          <textarea
            value={draft.description}
            onChange={(e) => setDraft({ ...draft, description: e.target.value })}
            placeholder="Description: what it means, how it is read"
            aria-label="Attribute description"
            rows={3}
          />
          <label className={styles.kind}>
            Kind
            <select value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value as AttributeKind })}>
              {kinds.map((k) => (
                <option key={k.id} value={k.id}>
                  {k.label}
                </option>
              ))}
            </select>
          </label>
          {draft.kind === "enum" && (
            <label className={styles.options}>
              Options, one per line
              <textarea value={options} onChange={(e) => setOptions(e.target.value)} placeholder={"calm\nangry"} rows={4} aria-label="Enum options" />
            </label>
          )}
          <div className={panes.actions}>
            <button type="submit" disabled={!draft.name.trim()}>
              {editing ? "Save" : "Add"}
            </button>
          </div>
        </form>
      </div>
    </section>
  );
}
