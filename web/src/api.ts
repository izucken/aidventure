// How a game is told: the role whoever directs plays, and the voice narration is written in. Both free text.
export type GameSettings = { director: string; voice: string };
export type Game = GameSettings & { id: string; name: string; createdAt: string };
export type Engine = "llamacpp" | "openrouter";
// thinkBudget is how many tokens the thought may run before the model is asked to answer; 0 is unbounded.
// key is sent in and never comes back; empty on update keeps the one stored.
export type ModelInput = { name: string; engine: Engine; endpoint: string; model: string; think: boolean; thinkBudget: number; key: string };
export type Model = Omit<ModelInput, "key"> & { id: string; createdAt: string; keySet: boolean };
export type Thread = {
  id: string;
  gameId: string;
  name: string;
  createdAt: string;
  // The scene a split took this thread from, and the entry in another thread it converged on; an ended thread is history.
  parentSceneId?: string;
  endedByEntryId?: string;
};
// What a declared attribute may be: a yes/no, a whole number, a decimal, or one of its options.
export type AttributeKind = "flag" | "integer" | "float" | "enum";
export type AttributeInput = { name: string; description: string; kind: AttributeKind; options: string[] };
export type Attribute = AttributeInput & { id: string; gameId: string; createdAt: string };
// A quality a subject or link carries: under a declared attribute, or freeform under its own name. The value is text whatever the kind.
export type AttributeValue = { attributeId?: string; name?: string; value: string };
// tracked marks an actor the story follows: a split keeps following tracked actors who part.
export type SubjectInput = { name: string; description: string; actor: boolean; scene: boolean; user: boolean; tracked: boolean; attributes: AttributeValue[] };
export type Subject = SubjectInput & { id: string; gameId: string; createdAt: string };
// How one subject stands to another, in the author's words, read as "from relation to".
export type LinkInput = { fromId: string; toId: string; relation: string; attributes: AttributeValue[] };
export type Link = LinkInput & { id: string; gameId: string; createdAt: string };
export type Scene = {
  id: string;
  threadId: string;
  placeId: string;
  createdAt: string;
  openingEntryId?: string;
  // The author's name for the scene, if any; the place stays the place.
  title?: string;
  cast: string[];
};
// How an entry moves the play: on in its scene, or into another scene or thread.
// A direction is the director's decision, recorded where it was made; the beats are the rest.
// Present, enter and exit say who is in a scene: at its head, and as they come and go.
export type EntryType = "narrative" | "cut" | "split" | "converge" | "direction" | "present" | "enter" | "exit";
// Where things go next, as directed: a cut or split names a place and a cast, a converge a thread.
// A new place comes with its link to the place left.
export type Direction = {
  type: EntryType;
  placeId?: string;
  newPlace?: SubjectInput;
  link?: string;
  cast?: string[];
  intoThreadId?: string;
  // Who acts first under it.
  actorId?: string;
  think?: string;
};
export type Transition = { direction: Direction; actorId: string; segments: Segment[] };
// An entry and where it landed: a split or converge lands it in another thread.
export type Result = { entry: Entry; scene: Scene; threadId: string };
// A turn is a direction nothing has followed yet: whose entry is awaited, and under what.
export type Turn = { entryId: string; actorId: string; user: boolean; think?: string; direction?: Direction };
export type Outcome = Partial<Result> & { directed?: Entry; turn?: Turn };
export type PlayEvent = { phase: "direct" | "entry"; part: "think" | "text"; delta: string };
export type Preview = {
  model: string;
  request: { System: string; User: string; Schema: Record<string, unknown> | null };
  rendered: { prompt: string; schema: string };
};
export type Kind = "narration" | "action" | "speech" | "thought";
export type Segment = { kind: Kind; text: string; to?: string };
export type Entry = {
  id: string;
  sceneId: string;
  // Who wrote a beat; a direction has no actor.
  actorId?: string;
  seq: number;
  type: EntryType;
  createdAt: string;
  think?: string;
  direction?: Direction;
  // Who a present, enter or exit entry is about.
  actors?: string[];
  segments?: Segment[];
};

type Method = "GET" | "POST" | "PUT" | "DELETE";

// What a failed call says: the server's message when it gave one, else the
// status, and always which call it was.
async function failure(res: Response, call: string): Promise<string> {
  const err = (await res.json().catch(() => null)) as { error?: string } | null;
  return `${err?.error ?? `${res.status} ${res.statusText}`} (${call})`;
}

async function request<T>(method: Method, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const init: RequestInit = { method };
  if (signal) init.signal = signal;
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
  const res = await fetch(path, init);
  if (!res.ok) {
    throw new Error(await failure(res, `${method} ${path}`));
  }
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
}

export const api = {
  models: () => request<Model[]>("GET", "/api/models"),
  createModel: (input: ModelInput) => request<Model>("POST", "/api/models", input),
  updateModel: (id: string, input: ModelInput) => request<void>("PUT", `/api/models/${id}`, input),
  copyModel: (id: string) => request<Model>("POST", `/api/models/${id}/copy`),
  currentModel: async () => (await request<{ modelId: string | null }>("GET", "/api/models/selection")).modelId,
  selectModel: (modelId: string) => request<void>("PUT", "/api/models/selection", { modelId }),

  games: () => request<Game[]>("GET", "/api/games"),
  createGame: (name: string) => request<Game>("POST", "/api/games", { name }),
  renameGame: (id: string, name: string) => request<void>("PUT", `/api/games/${id}`, { name }),
  setGameSettings: (id: string, settings: GameSettings) => request<void>("PUT", `/api/games/${id}/settings`, settings),
  currentGame: async () => (await request<{ gameId: string | null }>("GET", "/api/selection")).gameId,
  selectGame: (gameId: string) => request<void>("PUT", "/api/selection", { gameId }),

  threads: (gameId: string) => request<Thread[]>("GET", `/api/games/${gameId}/threads`),
  createThread: (gameId: string, name: string) =>
    request<Thread>("POST", `/api/games/${gameId}/threads`, { name }),
  renameThread: (gameId: string, id: string, name: string) =>
    request<void>("PUT", `/api/games/${gameId}/threads/${id}`, { name }),
  deleteThread: (gameId: string, id: string) => request<void>("DELETE", `/api/games/${gameId}/threads/${id}`),
  currentThread: async (gameId: string) =>
    (await request<{ threadId: string | null }>("GET", `/api/games/${gameId}/selection`)).threadId,
  selectThread: (gameId: string, threadId: string) =>
    request<void>("PUT", `/api/games/${gameId}/selection`, { threadId }),

  subjects: (gameId: string) => request<Subject[]>("GET", `/api/games/${gameId}/subjects`),
  createSubject: (gameId: string, input: SubjectInput) =>
    request<Subject>("POST", `/api/games/${gameId}/subjects`, input),
  updateSubject: (gameId: string, id: string, input: SubjectInput) =>
    request<void>("PUT", `/api/games/${gameId}/subjects/${id}`, input),

  deleteSubject: (gameId: string, id: string) => request<void>("DELETE", `/api/games/${gameId}/subjects/${id}`),

  links: (gameId: string) => request<Link[]>("GET", `/api/games/${gameId}/links`),
  createLink: (gameId: string, input: LinkInput) => request<Link>("POST", `/api/games/${gameId}/links`, input),
  updateLink: (gameId: string, id: string, input: LinkInput) =>
    request<void>("PUT", `/api/games/${gameId}/links/${id}`, input),
  deleteLink: (gameId: string, id: string) => request<void>("DELETE", `/api/games/${gameId}/links/${id}`),

  attributes: (gameId: string) => request<Attribute[]>("GET", `/api/games/${gameId}/attributes`),
  createAttribute: (gameId: string, input: AttributeInput) =>
    request<Attribute>("POST", `/api/games/${gameId}/attributes`, input),
  updateAttribute: (gameId: string, id: string, input: AttributeInput) =>
    request<void>("PUT", `/api/games/${gameId}/attributes/${id}`, input),
  deleteAttribute: (gameId: string, id: string) => request<void>("DELETE", `/api/games/${gameId}/attributes/${id}`),

  scenes: (gameId: string, threadId: string) =>
    request<Scene[]>("GET", `/api/games/${gameId}/threads/${threadId}/scenes`),
  // Takes the play out of the thread's current scene: a cut, split or converge with the entry that arrives.
  transition: (gameId: string, threadId: string, transition: Transition) =>
    request<Result>("POST", `/api/games/${gameId}/threads/${threadId}/transitions`, transition),

  retitleScene: (gameId: string, threadId: string, sceneId: string, title: string) =>
    request<void>("PUT", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}`, { title }),
  // Records who entered or exited the scene: at its end, or right after afterId.
  presence: (gameId: string, threadId: string, sceneId: string, type: "enter" | "exit", actors: string[], afterId?: string) =>
    request<Entry>("POST", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}/presence`, { type, actors, afterId }),
  deleteScene: (gameId: string, threadId: string, sceneId: string) =>
    request<void>("DELETE", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}`),

  threadEntries: (gameId: string, threadId: string) =>
    request<Entry[]>("GET", `/api/games/${gameId}/threads/${threadId}/entries`),
  entries: (gameId: string, threadId: string, sceneId: string) =>
    request<Entry[]>("GET", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}/entries`),
  // With afterId the entry goes right after that one; otherwise at the end of the scene.
  appendEntry: (gameId: string, threadId: string, sceneId: string, actorId: string, segments: Segment[], afterId?: string) =>
    request<Entry>("POST", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}/entries`, { actorId, segments, afterId }),
  updateEntry: (gameId: string, threadId: string, sceneId: string, id: string, actorId: string, segments: Segment[]) =>
    request<void>("PUT", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}/entries/${id}`, { actorId, segments }),
  deleteEntry: (gameId: string, threadId: string, sceneId: string, id: string) =>
    request<void>("DELETE", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}/entries/${id}`),
  prompt: (gameId: string, threadId: string, sceneId: string, actorId: string) =>
    request<Preview>("GET", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}/prompt?actor=${actorId}`),
  turn: async (gameId: string, threadId: string) =>
    (await request<{ turn: Turn | null }>("GET", `/api/games/${gameId}/threads/${threadId}/turn`)).turn,
  // Streams the turn as it is produced. Aborting the signal drops the
  // connection, which cancels the model call on the server.
  advance: async (gameId: string, threadId: string, onEvent: (ev: PlayEvent) => void, signal?: AbortSignal): Promise<Outcome> => {
    const init: RequestInit = { method: "POST" };
    if (signal) init.signal = signal;
    const res = await fetch(`/api/games/${gameId}/threads/${threadId}/advance`, init);
    if (!res.ok || !res.body) {
      throw new Error(await failure(res, `POST advance of thread ${threadId}`));
    }
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let outcome: Outcome | null = null;
    for (;;) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";
      for (const line of lines) {
        if (!line.trim()) continue;
        const msg = JSON.parse(line) as PlayEvent | { outcome: Outcome } | { error: string };
        if ("error" in msg) throw new Error(msg.error);
        if ("outcome" in msg) outcome = msg.outcome;
        else onEvent(msg);
      }
      if (done) break;
    }
    if (!outcome) throw new Error("advance ended without an outcome");
    return outcome;
  },
  pass: (gameId: string, threadId: string) => request<void>("DELETE", `/api/games/${gameId}/threads/${threadId}/turn`),
  roll: (gameId: string, threadId: string, sceneId: string, actorId: string) =>
    request<Entry>("POST", `/api/games/${gameId}/threads/${threadId}/scenes/${sceneId}/roll`, { actorId }),
};
