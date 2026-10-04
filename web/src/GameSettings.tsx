import { useEffect, useRef, useState, type FormEvent } from "react";

import { api, type Game, type GameSettings as Settings } from "./api";
import styles from "./GameSettings.module.css";
import { Modal } from "./Modal";

type Props = {
  game: Game;
  onChange: () => Promise<void>;
  onError: (err: unknown) => void;
};

// How the game is told, behind a button beside its name: the role whoever
// directs plays, and the voice narration is written in.
export function GameSettings({ game, onChange, onError }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [draft, setDraft] = useState<Settings>({ director: game.director, voice: game.voice });

  useEffect(() => {
    setDraft({ director: game.director, voice: game.voice });
  }, [game]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      await api.setGameSettings(game.id, draft);
      await onChange();
      dialog.current?.close();
    } catch (err) {
      onError(err);
    }
  }

  return (
    <>
      <button type="button" className={styles.trigger} title="How the game is told" aria-label="Game settings" onClick={() => dialog.current?.showModal()}>
        ≡
      </button>
      <Modal ref={dialog} title={`How ${game.name} is told`} narrow>
        <form className={styles.form} onSubmit={(e) => void submit(e)}>
          <label>
            Role of the director
            <input
              value={draft.director}
              onChange={(e) => setDraft({ ...draft, director: e.target.value })}
              placeholder="director; or dungeon master, showrunner…"
            />
          </label>
          <label>
            Narrative voice
            <textarea
              value={draft.voice}
              onChange={(e) => setDraft({ ...draft, voice: e.target.value })}
              placeholder="How narration lines are written: person, tense, distance, register"
              rows={4}
            />
          </label>
          <div className={styles.actions}>
            <button type="submit">Save</button>
            <button type="button" onClick={() => dialog.current?.close()}>
              Cancel
            </button>
          </div>
        </form>
      </Modal>
    </>
  );
}
