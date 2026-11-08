import type { Attribute, AttributeValue } from "./api";
import styles from "./AttributeFields.module.css";
import deletable from "./shared/deletable.module.css";

type Props = {
  value: AttributeValue[];
  // The game's declared attributes; what is not declared is written freeform.
  attributes: Attribute[];
  onChange: (value: AttributeValue[]) => void;
};

// What a value starts as when added: a flag on, numbers at zero, an enum at its first option.
function initial(a: Attribute): string {
  switch (a.kind) {
    case "flag":
      return "true";
    case "enum":
      return a.options[0] ?? "";
    default:
      return "0";
  }
}

const freeform = "freeform";

// The attributes an item carries, one row each: a declared attribute shows
// its name and a control for its kind; a freeform one is a name and a text.
// The picker below adds a declared attribute not yet carried, or a freeform one.
export function AttributeFields({ value, attributes, onChange }: Props) {
  const declared = (id: string | undefined) => attributes.find((a) => a.id === id);
  const unused = attributes.filter((a) => !value.some((v) => v.attributeId === a.id));

  function update(at: number, patch: Partial<AttributeValue>) {
    onChange(value.map((v, i) => (i === at ? { ...v, ...patch } : v)));
  }

  function add(choice: string) {
    if (!choice) return;
    const a = declared(choice);
    onChange([...value, a ? { attributeId: a.id, value: initial(a) } : { name: "", value: "" }]);
  }

  return (
    <div className={styles.section}>
      <h4>Attributes</h4>
      <ul className={styles.rows}>
        {value.map((v, i) => {
          const a = declared(v.attributeId);
          return (
            <li key={i} className={`${styles.row} ${deletable.deletable}`}>
              {a ? (
                <span className={styles.name} title={a.description || undefined}>
                  {a.name}
                </span>
              ) : (
                <input
                  value={v.name ?? ""}
                  onChange={(e) => update(i, { name: e.target.value })}
                  placeholder="Name"
                  aria-label="Attribute name"
                />
              )}
              <Control attribute={a} value={v.value} onChange={(text) => update(i, { value: text })} />
              <button
                type="button"
                className={deletable.delete}
                title="Remove attribute"
                onClick={() => onChange(value.filter((_, j) => j !== i))}
              >
                ×
              </button>
            </li>
          );
        })}
      </ul>
      <select className={styles.add} value="" onChange={(e) => add(e.target.value)} aria-label="Add attribute">
        <option value="">+ attribute…</option>
        {unused.map((a) => (
          <option key={a.id} value={a.id}>
            {a.name}
          </option>
        ))}
        <option value={freeform}>freeform</option>
      </select>
    </div>
  );
}

type ControlProps = { attribute: Attribute | undefined; value: string; onChange: (value: string) => void };

// The control a value is edited with, by the kind of its attribute; plain text when freeform.
function Control({ attribute, value, onChange }: ControlProps) {
  switch (attribute?.kind) {
    case "flag":
      return (
        <input type="checkbox" checked={value === "true"} onChange={(e) => onChange(e.target.checked ? "true" : "false")} aria-label={attribute.name} />
      );
    case "integer":
      return <input type="number" step="1" value={value} onChange={(e) => onChange(e.target.value)} aria-label={attribute.name} />;
    case "float":
      return <input type="number" step="any" value={value} onChange={(e) => onChange(e.target.value)} aria-label={attribute.name} />;
    case "enum":
      return (
        <select value={value} onChange={(e) => onChange(e.target.value)} aria-label={attribute.name}>
          {attribute.options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      );
    default:
      return <input value={value} onChange={(e) => onChange(e.target.value)} placeholder="Value" aria-label="Attribute value" />;
  }
}
