import { useCallback, useEffect, useRef, useState } from "react";

import { api, type Attribute, type Game, type Subject, type Thread } from "./api";
import styles from "./App.module.css";
import { Attributes } from "./Attributes";
import { InlineName } from "./InlineName";
import { Modal } from "./Modal";
import { Models } from "./Models";
import { Narration } from "./Narration";
import { Picker } from "./Picker";
import { Scenes } from "./Scenes";
import { Subjects } from "./Subjects";
import { Toasts, type Toast } from "./Toasts";
import corner from "./shared/corner.module.css";

// What settings mode holds: how the game is told, and what it is about.
type Tab = "narration" | "subjects" | "attributes";
const tabs: { id: Tab; label: string }[] = [
  { id: "narration", label: "Narration" },
  { id: "subjects", label: "Subjects" },
  { id: "attributes", label: "Attributes" },
];

export function App() {
  const [games, setGames] = useState<Game[]>([]);
  const [gameId, setGameId] = useState<string | null>(null);
  const [threads, setThreads] = useState<Thread[]>([]);
  const [threadId, setThreadId] = useState<string | null>(null);
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [attributes, setAttributes] = useState<Attribute[]>([]);
  // Play shows the script; settings shows the game's own tabs. The tab last looked at stays.
  const [mode, setMode] = useState<"play" | "settings">("play");
  const [tab, setTab] = useState<Tab>("narration");
  const gamesDialog = useRef<HTMLDialogElement>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const toastSeq = useRef(0);

  const report = useCallback(
    (err: unknown) => setToasts((t) => [...t, { id: ++toastSeq.current, text: err instanceof Error ? err.message : String(err) }]),
    [],
  );
  const dismiss = useCallback((id: number) => setToasts((t) => t.filter((x) => x.id !== id)), []);

  const loadGames = useCallback(async () => {
    try {
      const [list, current] = await Promise.all([api.games(), api.currentGame()]);
      setGames(list);
      setGameId(current);
    } catch (err) {
      report(err);
    }
  }, [report]);

  const loadThreads = useCallback(
    async (gameId: string) => {
      try {
        const [list, current] = await Promise.all([api.threads(gameId), api.currentThread(gameId)]);
        setThreads(list);
        setThreadId(current);
      } catch (err) {
        report(err);
      }
    },
    [report],
  );

  const loadSubjects = useCallback(async () => {
    if (!gameId) return;
    try {
      setSubjects(await api.subjects(gameId));
    } catch (err) {
      report(err);
    }
  }, [gameId, report]);

  const loadAttributes = useCallback(async () => {
    if (!gameId) return;
    try {
      setAttributes(await api.attributes(gameId));
    } catch (err) {
      report(err);
    }
  }, [gameId, report]);

  useEffect(() => {
    void loadGames();
  }, [loadGames]);

  useEffect(() => {
    if (mode !== "settings") return;
    const leave = (e: globalThis.KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (e.key !== "Escape" || e.defaultPrevented || t?.closest("input, textarea, select, dialog")) return;
      setMode("play");
    };
    window.addEventListener("keydown", leave);
    return () => window.removeEventListener("keydown", leave);
  }, [mode]);

  // The old game's threads go before the new game's arrive, so no render pairs the two.
  useEffect(() => {
    setThreads([]);
    setThreadId(null);
    if (gameId) void loadThreads(gameId);
  }, [gameId, loadThreads]);

  useEffect(() => {
    setSubjects([]);
    void loadSubjects();
  }, [loadSubjects]);

  useEffect(() => {
    setAttributes([]);
    void loadAttributes();
  }, [loadAttributes]);

  async function selectGame(id: string) {
    try {
      await api.selectGame(id);
      setGameId(id);
    } catch (err) {
      report(err);
    }
  }

  async function createGame(name: string) {
    try {
      const g = await api.createGame(name);
      await api.selectGame(g.id);
      await loadGames();
    } catch (err) {
      report(err);
    }
  }

  async function selectThread(id: string) {
    if (!gameId) return;
    try {
      await api.selectThread(gameId, id);
      setThreadId(id);
    } catch (err) {
      report(err);
    }
  }

  // The play went on in another thread, new or joined: list it and look at it.
  async function follow(id: string) {
    if (!gameId) return;
    await loadThreads(gameId);
    await selectThread(id);
  }

  async function renameGame(name: string) {
    if (!game) return;
    try {
      await api.renameGame(game.id, name);
      await loadGames();
    } catch (err) {
      report(err);
    }
  }

  async function renameThread(name: string) {
    if (!gameId || !thread) return;
    try {
      await api.renameThread(gameId, thread.id, name);
      await loadThreads(gameId);
    } catch (err) {
      report(err);
    }
  }

  async function deleteThread(id: string) {
    if (!gameId) return;
    try {
      await api.deleteThread(gameId, id);
      await loadThreads(gameId);
    } catch (err) {
      report(err);
    }
  }

  async function createThread(name: string) {
    if (!gameId) return;
    try {
      const t = await api.createThread(gameId, name);
      await api.selectThread(gameId, t.id);
      await loadThreads(gameId);
    } catch (err) {
      report(err);
    }
  }

  const game = games.find((g) => g.id === gameId) ?? null;
  const thread = threads.find((t) => t.id === threadId && t.gameId === gameId) ?? null;

  return (
    <div className={styles.layout}>
      <header className={styles.header}>
        {game ? (
          <>
            <InlineName as="h1" className={styles.gameName} value={game.name} label="Game name" onSave={renameGame} />
            <button
              type="button"
              className={styles.settings}
              title={mode === "settings" ? "Back to the script (Esc)" : "Game settings"}
              aria-label="Game settings"
              aria-pressed={mode === "settings"}
              onClick={() => setMode(mode === "settings" ? "play" : "settings")}
            >
              ⚙
            </button>
          </>
        ) : (
          <h1 className={styles.gameName}>No game chosen</h1>
        )}
      </header>
      <Modal ref={gamesDialog} title="Games" narrow>
        <Picker
          items={games}
          currentId={gameId}
          placeholder="New game"
          onSelect={(id) => {
            void selectGame(id);
            gamesDialog.current?.close();
          }}
          onCreate={async (name) => {
            await createGame(name);
            gamesDialog.current?.close();
          }}
        />
      </Modal>
      <div className={corner.corner}>
        <button type="button" className={corner.trigger} title="Games" aria-label="Games" onClick={() => gamesDialog.current?.showModal()}>
          ▤
        </button>
        <Models onError={report} />
      </div>
      <Toasts toasts={toasts} onDismiss={dismiss} />
      <main>
        {!game && (
          <p>
            <button type="button" onClick={() => gamesDialog.current?.showModal()}>
              Choose or create a game
            </button>
          </p>
        )}
        {game && mode === "settings" && (
          <>
            <nav className={styles.tabs} aria-label="Settings">
              {tabs.map((t) => (
                <button key={t.id} type="button" aria-pressed={tab === t.id} onClick={() => setTab(t.id)}>
                  {t.label}
                </button>
              ))}
            </nav>
            {tab === "narration" && <Narration game={game} onChange={loadGames} onError={report} />}
            {tab === "subjects" && (
              <Subjects gameId={game.id} subjects={subjects} attributes={attributes} onChange={loadSubjects} onError={report} />
            )}
            {tab === "attributes" && (
              <Attributes gameId={game.id} attributes={attributes} onChange={loadAttributes} onError={report} />
            )}
          </>
        )}
        {game && mode === "play" && (
          <>
            <nav>
              <Picker
                items={threads.map((t) => (t.endedByEntryId ? { ...t, name: `${t.name} (ended)` } : t))}
                currentId={threadId}
                placeholder="New thread"
                createTitle="New thread"
                horizontal
                onSelect={(id) => void selectThread(id)}
                onCreate={createThread}
                onDelete={deleteThread}
                noun="thread"
              />
            </nav>
            <section>
              {thread ? (
                <>
                  <InlineName as="h2" value={thread.name} label="Thread name" onSave={renameThread} />
                  <Scenes
                    gameId={game.id}
                    director={game.director}
                    threadId={thread.id}
                    threads={threads}
                    subjects={subjects}
                    onMoved={follow}
                    onError={report}
                  />
                </>
              ) : (
                <p>Select a thread or create one.</p>
              )}
            </section>
          </>
        )}
      </main>
    </div>
  );
}
