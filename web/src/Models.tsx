import { useEffect, useRef, useState, type FormEvent } from "react";

import { api, type Engine, type Model, type ModelInput } from "./api";
import { Modal } from "./Modal";
import styles from "./Models.module.css";
import badge from "./shared/badge.module.css";
import corner from "./shared/corner.module.css";
import list from "./shared/listItem.module.css";

type Props = { onError: (err: unknown) => void };

const engines: { id: Engine; label: string }[] = [
  { id: "llamacpp", label: "llama.cpp server" },
  { id: "openrouter", label: "OpenRouter" },
];
const empty: ModelInput = { name: "", engine: "llamacpp", endpoint: "", model: "", think: true, thinkBudget: 0, key: "" };

// Model entries the server can talk to, in a dialog behind a corner control.
// Pick one to edit it, or leave none picked to add a new one.
export function Models({ onError }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [models, setModels] = useState<Model[]>([]);
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState<ModelInput>(empty);
  const [activeId, setActiveId] = useState<string | null>(null);

  useEffect(() => {
    api.models().then(setModels).catch(onError);
    api.currentModel().then(setActiveId).catch(onError);
  }, [onError]);

  async function activate(id: string) {
    try {
      await api.selectModel(id);
      setActiveId(id);
    } catch (err) {
      onError(err);
    }
  }

  function pick(m: Model) {
    setEditing(m.id);
    setDraft({ name: m.name, engine: m.engine, endpoint: m.endpoint, model: m.model, think: m.think, thinkBudget: m.thinkBudget, key: "" });
  }

  function reset() {
    setEditing(null);
    setDraft(empty);
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      if (editing) {
        await api.updateModel(editing, draft);
      } else {
        await api.createModel(draft);
      }
      setModels(await api.models());
      reset();
    } catch (err) {
      onError(err);
    }
  }

  // A hosted model needs a key when first made; its endpoint has a default.
  const complete =
    draft.name.trim() && draft.model.trim() && (draft.engine === "openrouter" ? editing || draft.key.trim() : draft.endpoint.trim());

  return (
    <>
      <button type="button" className={corner.trigger} title="Models" aria-label="Models" onClick={() => dialog.current?.showModal()}>
        ⚙
      </button>
      <Modal ref={dialog} title="Models" narrow>
        <ul className={list.list}>
          {models.map((m) => (
            <li key={m.id} className={styles.item}>
              <button
                type="button"
                className={list.row}
                aria-current={m.id === editing ? "true" : undefined}
                onClick={() => pick(m)}
              >
                <strong>
                  {m.name}
                  {m.id === activeId && <em className={badge.badge}>active</em>}
                </strong>
                <span className={list.meta}>
                  {m.engine} · {m.model}
                  {m.think && (m.thinkBudget ? ` · thinking, up to ${m.thinkBudget} tokens` : " · thinking")}
                </span>
              </button>
              {m.id !== activeId && (
                <button type="button" className={styles.use} onClick={() => void activate(m.id)}>
                  Use
                </button>
              )}
            </li>
          ))}
        </ul>
        <form className={styles.form} onSubmit={(e) => void submit(e)}>
          <input
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            placeholder="Name"
            aria-label="Model entry name"
          />
          <select
            value={draft.engine}
            onChange={(e) => setDraft({ ...draft, engine: e.target.value as Engine })}
            aria-label="Engine"
          >
            {engines.map((eng) => (
              <option key={eng.id} value={eng.id}>
                {eng.label}
              </option>
            ))}
          </select>
          <input
            value={draft.endpoint}
            onChange={(e) => setDraft({ ...draft, endpoint: e.target.value })}
            placeholder={draft.engine === "openrouter" ? "Endpoint, blank for openrouter.ai" : "Endpoint, e.g. http://host:11434"}
            aria-label="Endpoint"
          />
          <input
            value={draft.model}
            onChange={(e) => setDraft({ ...draft, model: e.target.value })}
            placeholder={draft.engine === "openrouter" ? "Model, e.g. z-ai/glm-5.3" : "Model"}
            aria-label="Model"
          />
          {draft.engine === "openrouter" && (
            <input
              type="password"
              value={draft.key}
              onChange={(e) => setDraft({ ...draft, key: e.target.value })}
              placeholder={editing && models.find((m) => m.id === editing)?.keySet ? "API key, blank to keep the stored one" : "API key"}
              aria-label="API key"
              autoComplete="off"
            />
          )}
          <label>
            <input type="checkbox" checked={draft.think} onChange={(e) => setDraft({ ...draft, think: e.target.checked })} />{" "}
            Thinking: reason before answering
          </label>
          {draft.think && (
            <label>
              Thinking budget, in tokens; 0 lets it run{" "}
              <input
                type="number"
                min={0}
                step={100}
                value={draft.thinkBudget}
                onChange={(e) => setDraft({ ...draft, thinkBudget: Math.max(0, Number(e.target.value) || 0) })}
                aria-label="Thinking budget in tokens"
              />
            </label>
          )}
          <div className={styles.actions}>
            <button type="submit" disabled={!complete}>
              {editing ? "Save" : "Add"}
            </button>
            {editing && (
              <button type="button" onClick={reset}>
                Cancel
              </button>
            )}
          </div>
        </form>
      </Modal>
    </>
  );
}
