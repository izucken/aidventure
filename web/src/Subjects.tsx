import { useEffect, useState, type FormEvent, type ReactNode } from "react";

import { api, type Attribute, type AttributeValue, type Link, type Subject, type SubjectInput } from "./api";
import { AttributeFields } from "./AttributeFields";
import badge from "./shared/badge.module.css";
import deletable from "./shared/deletable.module.css";
import list from "./shared/listItem.module.css";
import panes from "./shared/panes.module.css";
import styles from "./Subjects.module.css";

type Props = {
  gameId: string;
  subjects: Subject[];
  // The game's declared attributes, offered on subjects and links alike.
  attributes: Attribute[];
  onChange: () => Promise<void>;
  onError: (err: unknown) => void;
};

type Kind = "all" | "places" | "actors" | "other";
const kinds: { id: Kind; label: string }[] = [
  { id: "all", label: "All" },
  { id: "places", label: "Places" },
  { id: "actors", label: "Actors" },
  { id: "other", label: "Other" },
];
const ofKind = (s: Subject, k: Kind) =>
  k === "all" || (k === "places" && s.scene) || (k === "actors" && s.actor) || (k === "other" && !s.scene && !s.actor);

const empty: SubjectInput = { name: "", description: "", actor: false, scene: false, user: false, tracked: false, attributes: [] };
// A link under construction, seen from the selected subject: outward reads
// "this relation other", inward "other relation this".
type LinkDraft = { inward: boolean; relation: string; otherId: string; attributes: AttributeValue[] };
const noLink: LinkDraft = { inward: false, relation: "", otherId: "", attributes: [] };
const swap = (d: LinkDraft): LinkDraft => ({ ...d, inward: !d.inward });

// The game's subjects: everything the play is about, and how they stand to
// each other. The list on the left picks one; the right edits it and its links.
export function Subjects({ gameId, subjects, attributes, onChange, onError }: Props) {
  const [kind, setKind] = useState<Kind>("all");
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState<SubjectInput>(empty);
  const [links, setLinks] = useState<Link[]>([]);
  const [link, setLink] = useState<LinkDraft>(noLink);
  const [editingLink, setEditingLink] = useState<string | null>(null);
  const [linkEdit, setLinkEdit] = useState<LinkDraft>(noLink);

  const nameOf = (id: string) => subjects.find((s) => s.id === id)?.name ?? "?";

  async function reloadLinks() {
    setLinks(await api.links(gameId));
  }

  useEffect(() => {
    setEditing(null);
    setDraft(empty);
    setLinks([]);
    api.links(gameId).then(setLinks).catch(onError);
  }, [gameId, onError]);

  function pick(s: Subject) {
    setEditing(s.id);
    setDraft({ name: s.name, description: s.description, actor: s.actor, scene: s.scene, user: s.user, tracked: s.tracked, attributes: s.attributes });
    setLink(noLink);
    setEditingLink(null);
  }

  function reset() {
    setEditing(null);
    setDraft(empty);
    setLink(noLink);
    setEditingLink(null);
  }

  async function remove(id: string) {
    try {
      await api.deleteSubject(gameId, id);
      await Promise.all([onChange(), reloadLinks()]);
      if (id === editing) reset();
    } catch (err) {
      onError(err);
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    const input = { ...draft, name: draft.name.trim(), description: draft.description.trim() };
    if (!input.name) return;
    try {
      if (editing) {
        await api.updateSubject(gameId, editing, input);
      } else {
        const made = await api.createSubject(gameId, input);
        setEditing(made.id);
      }
      await onChange();
    } catch (err) {
      onError(err);
    }
  }

  async function addLink(e: FormEvent) {
    e.preventDefault();
    if (!editing || !link.otherId || !link.relation.trim()) return;
    const [fromId, toId] = link.inward ? [link.otherId, editing] : [editing, link.otherId];
    try {
      await api.createLink(gameId, { fromId, toId, relation: link.relation.trim(), attributes: link.attributes });
      await reloadLinks();
      setLink({ ...link, relation: "", otherId: "", attributes: [] });
    } catch (err) {
      onError(err);
    }
  }

  // A link edits in place: picked, its row becomes the form.
  function pickLink(l: Link) {
    setEditingLink(l.id);
    setLinkEdit({ inward: l.toId === editing, relation: l.relation, otherId: l.fromId === editing ? l.toId : l.fromId, attributes: l.attributes });
  }

  async function saveLink(e: FormEvent) {
    e.preventDefault();
    if (!editing || !editingLink || !linkEdit.otherId || !linkEdit.relation.trim()) return;
    const [fromId, toId] = linkEdit.inward ? [linkEdit.otherId, editing] : [editing, linkEdit.otherId];
    try {
      await api.updateLink(gameId, editingLink, { fromId, toId, relation: linkEdit.relation.trim(), attributes: linkEdit.attributes });
      await reloadLinks();
      setEditingLink(null);
    } catch (err) {
      onError(err);
    }
  }

  async function removeLink(id: string) {
    try {
      await api.deleteLink(gameId, id);
      await reloadLinks();
      if (id === editingLink) setEditingLink(null);
    } catch (err) {
      onError(err);
    }
  }

  const shown = subjects.filter((s) => ofKind(s, kind) && s.name.toLowerCase().includes(search.trim().toLowerCase()));
  const own = links.filter((l) => l.fromId === editing || l.toId === editing);
  const others = subjects.filter((s) => s.id !== editing);

  return (
    <section className={panes.view}>
      <div>
        <div className={styles.filters}>
          {kinds.map((k) => (
            <button key={k.id} type="button" aria-pressed={kind === k.id} onClick={() => setKind(k.id)}>
              {k.label}
            </button>
          ))}
        </div>
        <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search" aria-label="Search subjects" />
        <ul className={list.list}>
          <li>
            <button type="button" className={list.row} aria-current={editing === null ? "true" : undefined} onClick={reset}>
              <strong>+ New subject</strong>
            </button>
          </li>
          {shown.map((s) => (
            <li key={s.id} className={deletable.deletable}>
              <button
                type="button"
                className={list.row}
                aria-current={s.id === editing ? "true" : undefined}
                onClick={() => pick(s)}
              >
                <strong>
                  {s.name}
                  {s.actor && (
                    <em className={badge.badge} title="Actor: may take turns">
                      A
                    </em>
                  )}
                  {s.scene && (
                    <em className={badge.badge} title="Scene: may host a scene">
                      S
                    </em>
                  )}
                  {s.actor && s.user && (
                    <em className={badge.badge} title="User: a human answers for this actor">
                      U
                    </em>
                  )}
                  {s.actor && s.tracked && (
                    <em className={badge.badge} title="Tracked: the story follows this actor">
                      T
                    </em>
                  )}
                </strong>
                {s.description && <span className={list.meta}>{s.description}</span>}
              </button>
              <button type="button" className={deletable.delete} title="Delete subject" onClick={() => void remove(s.id)}>
                ×
              </button>
            </li>
          ))}
        </ul>
      </div>
      <div className={panes.detail}>
        <form className={panes.form} onSubmit={(e) => void submit(e)}>
          <input
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            placeholder="Name"
            aria-label="Subject name"
          />
          <textarea
            value={draft.description}
            onChange={(e) => setDraft({ ...draft, description: e.target.value })}
            placeholder="Description"
            aria-label="Subject description"
            rows={5}
          />
          <label>
            <input type="checkbox" checked={draft.actor} onChange={(e) => setDraft({ ...draft, actor: e.target.checked })} />{" "}
            Actor: may take turns
          </label>
          <label>
            <input type="checkbox" checked={draft.scene} onChange={(e) => setDraft({ ...draft, scene: e.target.checked })} />{" "}
            Scene: may host a scene
          </label>
          <label>
            <input
              type="checkbox"
              checked={draft.user}
              disabled={!draft.actor}
              onChange={(e) => setDraft({ ...draft, user: e.target.checked })}
            />{" "}
            User: a human answers for this actor
          </label>
          <label>
            <input
              type="checkbox"
              checked={draft.tracked}
              disabled={!draft.actor}
              onChange={(e) => setDraft({ ...draft, tracked: e.target.checked })}
            />{" "}
            Tracked: the story follows this actor
          </label>
          <AttributeFields value={draft.attributes} attributes={attributes} onChange={(vs) => setDraft({ ...draft, attributes: vs })} />
          <div className={panes.actions}>
            <button type="submit" disabled={!draft.name.trim()}>
              {editing ? "Save" : "Add"}
            </button>
          </div>
        </form>
        {editing && (
          <section className={styles.links}>
            <h3>Links</h3>
            {own.length === 0 && <p className={list.meta}>Nothing links here yet.</p>}
            <ul className={list.list}>
              {own.map((l) =>
                l.id === editingLink ? (
                  <li key={l.id} className={`${styles.link} ${styles.editingLink}`} aria-current="true">
                    <form className={styles.linkForm} onSubmit={(e) => void saveLink(e)}>
                      <LinkFields value={linkEdit} self={draft.name} others={others} onChange={setLinkEdit} />
                      <AttributeFields value={linkEdit.attributes} attributes={attributes} onChange={(vs) => setLinkEdit({ ...linkEdit, attributes: vs })} />
                      <div className={styles.linkActions}>
                        <button type="submit" disabled={!linkEdit.otherId || !linkEdit.relation.trim()}>
                          Save
                        </button>
                        <button type="button" onClick={() => setEditingLink(null)}>
                          Cancel
                        </button>
                      </div>
                    </form>
                  </li>
                ) : (
                  <li key={l.id} className={`${styles.link} ${deletable.deletable}`} data-interactive="" onClick={() => pickLink(l)}>
                    <LinkLines
                      from={<span className={l.fromId === editing ? styles.self : undefined}>{nameOf(l.fromId)}</span>}
                      relation={<em className={styles.relation}>{l.relation}</em>}
                      to={<span className={l.toId === editing ? styles.self : undefined}>{nameOf(l.toId)}</span>}
                    />
                    <button
                      type="button"
                      className={deletable.delete}
                      title="Delete link"
                      onClick={(e) => {
                        e.stopPropagation();
                        void removeLink(l.id);
                      }}
                    >
                      ×
                    </button>
                  </li>
                ),
              )}
            </ul>
            <form className={styles.linkForm} onSubmit={(e) => void addLink(e)}>
              <LinkFields value={link} self={draft.name} others={others} onChange={setLink} />
              <AttributeFields value={link.attributes} attributes={attributes} onChange={(vs) => setLink({ ...link, attributes: vs })} />
              <div className={styles.linkActions}>
                <button type="submit" disabled={!link.otherId || !link.relation.trim()}>
                  Link
                </button>
              </div>
            </form>
          </section>
        )}
      </div>
    </section>
  );
}

type LinkLinesProps = { from: ReactNode; relation: ReactNode; to: ReactNode };

// A link always reads top to bottom, from one end through the relation to
// the other, each line hanging off the one above.
function LinkLines({ from, relation, to }: LinkLinesProps) {
  return (
    <>
      <span className={styles.line}>{from}</span>
      <span className={`${styles.line} ${styles.hang1}`}>
        <span className={styles.glyph} aria-hidden="true">
          └
        </span>
        {relation}
      </span>
      <span className={`${styles.line} ${styles.hang2}`}>
        <span className={styles.glyph} aria-hidden="true">
          └
        </span>
        {to}
      </span>
    </>
  );
}

type LinkFieldsProps = {
  value: LinkDraft;
  // The selected subject's name, one end of the link.
  self: string;
  others: Subject[];
  onChange: (value: LinkDraft) => void;
};

// The link under construction in the same lines as it displays: the selected
// subject fixed at one end, a picker at the other, the relation between.
function LinkFields({ value, self, others, onChange }: LinkFieldsProps) {
  // The fixed end carries the control that sends it to the other end.
  const me = (
    <span className={styles.fixed}>
      {self || "this"}
      <button type="button" className={styles.swap} onClick={() => onChange(swap(value))} title="Put at the other end">
        ⟳
      </button>
    </span>
  );
  const picker = (
    <select value={value.otherId} onChange={(e) => onChange({ ...value, otherId: e.target.value })} aria-label="Other subject">
      <option value="">subject…</option>
      {others.map((s) => (
        <option key={s.id} value={s.id}>
          {s.name}
        </option>
      ))}
    </select>
  );
  return (
    <LinkLines
      from={value.inward ? picker : me}
      relation={
        <input
          value={value.relation}
          onChange={(e) => onChange({ ...value, relation: e.target.value })}
          placeholder={value.inward ? 'relation, as in "owes"' : 'relation, as in "north of"'}
          aria-label="Relation"
        />
      }
      to={value.inward ? me : picker}
    />
  );
}
