import { useEffect, useState } from "react";

import { api, type Scene, type Subject, type Thread } from "./api";
import { Entries } from "./Entries";

type Props = {
  gameId: string;
  // The role whoever directs plays, as the game names it; empty means director.
  director: string;
  threadId: string;
  threads: Thread[];
  subjects: Subject[];
  onMoved: (threadId: string) => Promise<void>;
  onError: (err: unknown) => void;
};

// Loads the thread's scenes and keeps them current; the script renders them.
export function Scenes({ gameId, director, threadId, threads, subjects, onMoved, onError }: Props) {
  const [scenes, setScenes] = useState<Scene[]>([]);

  useEffect(() => {
    setScenes([]);
    api.scenes(gameId, threadId).then(setScenes).catch(onError);
  }, [gameId, threadId, onError]);

  async function reload() {
    setScenes(await api.scenes(gameId, threadId));
  }

  async function remove(id: string) {
    try {
      await api.deleteScene(gameId, threadId, id);
      await reload();
    } catch (err) {
      onError(err);
    }
  }

  return (
    <Entries
      gameId={gameId}
      director={director}
      threadId={threadId}
      threads={threads}
      scenes={scenes}
      subjects={subjects}
      onChange={reload}
      onMoved={onMoved}
      onDeleteScene={remove}
      onError={onError}
    />
  );
}
