# ADR-080: `DecimalInput` — Locale-Safe Decimal Entry as a Shared UI Primitive

**Date:** 2026-08-27
**Status:** Accepted

## Context

Every price/amount/unit entry field across all three apps (root `investing-app`,
`apps/portfolio-b`, `apps/portfolio-c`) uses a native
`<Input type="number" step="…" />`. Native `type="number"` inputs only accept the decimal
separator matching the browser/OS locale. On a Polish-locale system, the numpad decimal key
sends `,`, but the input (rendered `en`) expects `.` — the keystroke is silently dropped and
the user cannot type a decimal at all. This is not one dialog's bug; it's the failure mode of
every `type="number"` field app-wide (task `.claude/tasks/126-decimal-input-comma-dot-support.task.md`
enumerates ~25 affected files across the three trees).

`packages/shared/ui` (ADR-076/078, `@shared/ui-primitives`) already holds the one canonical
copy of `Input` and other shadcn-style primitives consumed by all three apps. This is exactly
the shape of fix that belongs there: one component, one behavior, fixed once for every
consumer.

### Pattern evaluation

| Candidate | Verdict | Why |
|---|---|---|
| Adapter | **Adopted** | `DecimalInput` adapts a plain `<input type="text">` to present the same interaction contract a numeric-entry field needs (typed decimal in, normalized decimal string out) — it wraps `Input`, translating locale-variant keystrokes into one canonical `.`-separated string, without changing `Input`'s own rendering/styling. |
| Decorator (generic, wraps arbitrary children/behaviors) | Rejected | Only one sanitization behavior exists today (decimal normalization). A generic decorator abstraction over arbitrary input behaviors has no second use case yet — YAGNI. |
| Strategy (pluggable sanitization rules, e.g. per-locale formats) | Rejected | One normalization rule set (`,`/`.` → `.`, single separator, digits + optional leading `-`) covers every known call site. No evidence of a second variant needed — YAGNI. |
| Template Method (shared `SanitizedTextInput` base for future `IntegerInput`, etc.) | Rejected, for now | Extracting a base class/hook for "text input with a sanitize-on-change step" is reasonable the moment a *second* sanitized-input variant appears. With exactly one variant, that extraction is speculative — KISS. Flagged below as the natural next step if it happens. |
| Facade | Rejected | Surface is one component and one function; a facade adds a layer with nothing to simplify. |

**Principles applied:** Single Responsibility — `DecimalInput` only sanitizes/normalizes typed
text; it does not validate (min/max/required stay in the zod schema, per existing convention).
DRY — delegates to `Input` for all rendering/styling rather than re-implementing shadcn markup.
Open/Closed — every native `<input>` prop except `type` still passes through untouched, so
call sites keep `placeholder`, `className`, `disabled`, `min` (as an HTML-level hint only), etc.
without needing changes to `DecimalInput` itself.

**No diagram:** this is a single new leaf component with no new relationships to existing
entities/services/flows beyond "wraps `Input`" — a class/sequence diagram would add no
information a one-line description doesn't already carry. The existing
`docs/architecture/12-portfolio-bpps-and-shared-packages.md` §2.1 table (which documents every
`@shared/ui-primitives` member) is the right place for this addition, and is updated below.

## Decision

Add `DecimalInput` to `packages/shared/ui/src/decimal-input.tsx`, beside the existing `Input`
in `packages/shared/ui/src/input.tsx`.

### API

```ts
export function normalizeDecimalInput(raw: string): string;

export interface DecimalInputProps
  extends Omit<React.ComponentProps<"input">, "type"> {}

export function DecimalInput(props: DecimalInputProps): JSX.Element;
```

- **`type` is fixed** — `DecimalInput` always renders `<Input type="text" inputMode="decimal" .../>`
  internally. `type` is omitted from the props type (a TS compile error if a call site tries to
  pass it), since text-mode is what makes locale-independent typing possible in the first place —
  `type="number"` is exactly the thing being replaced.
- **`inputMode` defaults to `"decimal"`** but is not hardcoded away — a call site may still pass
  an explicit `inputMode` override (e.g. `"numeric"` for an integer-only field that reuses this
  component); an explicit prop wins over the default.
- **Every other native `<input>` prop passes through unchanged** — `value`, `defaultValue`,
  `placeholder`, `className`, `disabled`, `min`/`max` (HTML-level hints only — real bounds
  validation belongs in the zod schema, unchanged from today), `name`, `ref`, etc. `DecimalInput`
  does not reimplement `Input`'s styling; it renders `<Input {...}/>` and inherits it.

### Sanitization / normalization behavior

`onChange` is intercepted, not replaced: `DecimalInput` mutates `event.target.value` in place
(via `normalizeDecimalInput`) before invoking the caller's own `onChange(event)`, so existing
call sites — whether they read `e.target.value` directly, or are RHF-registered — see the
already-normalized string with no signature change on their end.

`normalizeDecimalInput(raw: string): string` — the standalone export the task file proposed,
**confirmed as warranted**: call sites that parse a value manually outside of an `onChange`
handler (e.g. sanitizing a paste, or a value arriving from elsewhere) need the same rule applied
without going through a synthetic event. Exact rules, applied in order:

1. **Strip disallowed characters** — anything outside `[0-9.,-]` is removed.
2. **At most one leading `-`** — a `-` is kept only if it is the very first character of the
   result; every other `-` anywhere else in the string is stripped (so interior `-` characters
   from a bad paste don't survive, but a genuine negative-number field still works end to end).
3. **Collapse to a single decimal separator, normalized to `.`** — scanning left to right, the
   *first* `.` or `,` encountered is kept and converted to `.`; every subsequent `.` or `,` is
   removed (its surrounding digits are kept — a wayward second separator doesn't truncate what
   was already typed, e.g. `"12.34.56"` → `"12.3456"`, not `"12.34"`).

Reference implementation (normative — implementers should match this exactly, not just the
prose above):

```ts
export function normalizeDecimalInput(raw: string): string {
  let value = raw.replace(/[^0-9.,-]/g, "");

  const isNegative = value.startsWith("-");
  value = value.replace(/-/g, "");

  let seenSeparator = false;
  value = value.replace(/[.,]/g, () => {
    if (seenSeparator) return "";
    seenSeparator = true;
    return ".";
  });

  return (isNegative ? "-" : "") + value;
}
```

Examples: `"12,5"` → `"12.5"`; `"-12,5"` → `"-12.5"`; `"12.34.56"` → `"12.3456"`;
`"abc12,5xyz"` → `"12.5"`; `"1-2"` → `"12"`; `","` → `"."`; `".5"` → `".5"` (a bare leading
separator is left as-is — a valid intermediate typing state; `Number(".5")` still parses
correctly at submit time).

`DecimalInput` never enforces positivity, a minimum, or "required" — that is deliberately left
to the zod schema at each call site, unchanged from the current validation split.

### Call-site migration patterns (for the frontend-web implementation agents)

**Mechanical component swap**, per field:

```diff
-<Input type="number" step="0.01" min="0" {...} />
+<DecimalInput {...} />
```

Drop `type`/`step`. Keep `min` only where it already exists purely as an HTML hint; do not add
new zod validation as part of this fix unless a field genuinely has none today (out of scope —
this is a mechanical fix, not a validation audit).

**react-hook-form fields** — `valueAsNumber: true` relies on `input.valueAsNumber`, a browser
API that only exists for `type="number"` inputs (always `NaN` on `type="text"`). Replace with
`setValueAs`:

```diff
-register("price", { valueAsNumber: true })
+register("price", { setValueAs: (v) => (v === "" ? undefined : Number(v)) })
```

No other RHF wiring changes. `DecimalInput` is uncontrolled exactly like `Input` was (it forwards
`ref` straight through to the underlying native `<input>` via `Input`, and does not hold any
internal display-state of its own) — `register(...)`'s spread (`onChange`, `onBlur`, `name`,
`ref`) works unchanged; RHF still reads the live DOM value through the `ref` on submit, same as
it did through `type="number"`. This uncontrolled-by-design choice is exactly why the Template
Method / internal-state option (a `DecimalInput` that buffers its own display string to
reconcile against a numeric `value` prop) was not built — see the Pattern evaluation table
above; it would only be needed to paper over the `useState`-controlled pitfall below, which has
a simpler fix at the call site instead.

**Plain `useState`-controlled fields** (inline table edits, `TradeEntryForm`, and any other
field that is *not* RHF-registered): the pitfall is that a controlled field which round-trips
its value through `Number()` on every keystroke drops a trailing `.` the instant it's typed —
`Number("12.")` is `12`, so the next render shows `"12"` back into the input and the user can
never finish typing a decimal. Fix at the call site, not in `DecimalInput` (which has no way to
know how the caller stores its value):

```diff
-const [price, setPrice] = useState(0);
-<DecimalInput value={price} onChange={(e) => setPrice(Number(e.target.value))} />
+const [price, setPrice] = useState("");
+<DecimalInput value={price} onChange={(e) => setPrice(e.target.value)} />
// coerce once, at submit/blur: const parsed = price === "" ? undefined : Number(price);
```

Keep the *string* in local state while typing; only coerce to `Number` at submit/blur. This is
not automatic from the component swap alone — each of the task file's listed `useState`-backed
fields needs this audited individually by the implementing agent.

## Consequences

- One normalization rule, defined once, fixes every locale-dependent decimal-entry field in all
  three apps — consistent with the existing `@shared/ui-primitives` extraction pattern (ADR-076
  §2.1), no new per-app divergence introduced.
- `DecimalInput` intentionally does zero validation — min/max/required stay exactly where they
  already are (zod schemas). This keeps the fix mechanical and small, matching the task's own
  framing; a future validation audit, if ever needed, is a fully separate piece of work.
- `IntegerInput` (or any other sanitized-text-input variant) is not built now. If a second such
  variant is ever needed, that is the trigger to extract the Template Method base considered and
  rejected above — not before.
- Mobile is out of scope — there is no mobile app in this repo.
