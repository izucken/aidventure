import { useEffect, useState, type FormEvent, type KeyboardEvent } from "react";

import { api, type Game, type GameSettings as Settings } from "./api";
import styles from "./Narration.module.css";

type Props = {
  game: Game;
  onChange: () => Promise<void>;
  onError: (err: unknown) => void;
};

// How the game is told: the role whoever directs plays, and the voice
// narration is written in. Saved as one, by the button or Ctrl+Enter.
export function Narration({ game, onChange, onError }: Props) {
  const [draft, setDraft] = useState<Settings>({ director: game.director, voice: game.voice });

  useEffect(() => {
    setDraft({ director: game.director, voice: game.voice });
  }, [game]);

  async function save() {
    try {
      await api.setGameSettings(game.id, draft);
      await onChange();
    } catch (err) {
      onError(err);
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault();
    void save();
  }

  function keyDown(e: KeyboardEvent) {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      void save();
    }
  }

  return (
    <form className={styles.form} onSubmit={submit} onKeyDown={keyDown}>
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
      </div>
    </form>
  );
}
