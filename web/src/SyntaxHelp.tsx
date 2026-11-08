import type { RefObject } from "react";

import { Modal } from "./Modal";
import styles from "./SyntaxHelp.module.css";

type Props = { ref: RefObject<HTMLDialogElement | null> };

// What the composer reads: the lines of an entry, by example. Opened from
// the Help button beside the composer; the analysis in script.ts is the
// authority, this only shows it.
export function SyntaxHelp({ ref }: Props) {
  return (
    <Modal ref={ref} title="How an entry is written">
      <section className={styles.topic}>
        <h4>An entry</h4>
        <p>The first line names who acts. Then one line per beat, its kind by its first character.</p>
        <pre className={styles.example}>
          {`Mara:
(sets the lantern down)
"You said three days."
\`I cannot lie for her.\`
Rain on the shutters.`}
        </pre>
        <dl className={styles.kinds}>
          <dt>( )</dt>
          <dd>action, what the actor does</dd>
          <dt>" "</dt>
          <dd>speech, what the actor says aloud</dd>
          <dt>` `</dt>
          <dd>thought, unheard by anyone</dd>
          <dt>plain</dt>
          <dd>narration, what is seen from the actor's point of view</dd>
        </dl>
      </section>
      <section className={styles.topic}>
        <h4>Who comes and goes</h4>
        <p>A staging line stands alone, with no beats beside it. It can be inserted anywhere in the script.</p>
        <pre className={styles.example}>
          {`Enter: Fen, Innkeeper.
Exit: Mara.`}
        </pre>
      </section>
      <section className={styles.topic}>
        <h4>Where the play goes</h4>
        <p>A move comes last, under the beats of the entry that arrives. Only the composer at the end takes one.</p>
        <pre className={styles.example}>
          {`Cut -> Tavern
With: Mara, Fen`}
        </pre>
        <dl className={styles.kinds}>
          <dt>Cut -&gt; Place</dt>
          <dd>the play goes on at a place of the game; With: says who is present there, everyone present now by default</dd>
          <dt>Cut =&gt; Name: what it is | how it stands to the place left</dt>
          <dd>the same, to a place made now; the part after | is required while a scene is being left</dd>
          <dt>Split -&gt; Place, Split =&gt; ...</dt>
          <dd>those under With: leave for the place and go on in a new thread; the rest stay here</dd>
          <dt>Converge -&gt; Thread</dt>
          <dd>everyone present goes to where another open thread is, and the threads become one</dd>
        </dl>
      </section>
      <section className={styles.topic}>
        <h4>Keys</h4>
        <p>Ctrl+Enter sends the entry. With nothing written, it lets the director decide what happens next.</p>
        <p>Where a name goes, the rest of the first name that fits is shown after the caret. Tab takes it; Tab again takes the next one.</p>
      </section>
    </Modal>
  );
}
