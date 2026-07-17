# tcli expect operators: negation, regex, and type assertion design

## Goal

Extend tcli's `expect` expression language with four operators — `not exists`,
`not contains`, `matches`, and `is <type>` — while keeping the language a flat
`<lhs> <op> <rhs>` list with no assertion keys, no boolean combinators, and no
new configuration surface.

## Motivation

Three concrete assertion gaps drove this:

1. **Desensitization checks need negative assertions.** "This field must be
   masked or absent" cannot be written with `exists`/`contains` alone.
   `not exists` asserts a field is gone; `not contains` asserts a plaintext
   value appears nowhere. Because gjson's `Result.String()` on an object or
   array path returns the raw JSON serialization, `body not contains
   <plaintext>` scans the entire serialized body — the check survives field
   renames and moves.
2. **Response-format checks need a regex.** "The masked phone looks like
   `133****3333`" is a shape assertion, not an equality; `matches` brings Go
   RE2 matching to any result path.
3. **Type regressions are invisible to `==`.** A snowflake ID serialized as the
   JSON string `"123456789012345678"` and the JSON number `123456789012345678`
   both satisfy `orderNo == 123456789012345678` (comparison is on the string
   value, numerically when both sides parse). When a service stops quoting the
   ID — breaking JavaScript clients — no equality expression can catch it.
   `is string` asserts the JSON type itself.

## Syntax and semantics

All four are ordinary expressions in the existing `expect:` list. `{{vars}}`
render first; parse errors are `EXPECT_INVALID`; assertion failures are
`EXPECT_FAILED`.

- **`path not exists`** (suffix form, no rhs) — exact negation of `exists`:
  passes when the path is missing OR its trimmed string value is empty (JSON
  `null` counts as empty). Parsed before the `exists` suffix so `x not exists`
  is not misread as lhs `x not`. In a standalone assertion the rendered
  literal passes iff it is empty/whitespace.
- **`path not contains v`** — exact negation of `contains`: fails when the
  actual string contains `v` (unquoted), passes otherwise. A missing path
  passes vacuously — "must not appear" holds. Listed ahead of ` contains ` in
  the operator table so `x not contains y` does not split as lhs `x not`.
- **`path matches re`** — Go regexp (RE2), `MatchString` against the actual
  string value, **unanchored** partial match; the rhs is unquoted before
  compiling. The regex is compiled inside `ParseExpr`, so an invalid pattern
  is `EXPECT_INVALID` at validate time and at eval time alike. A missing path
  yields actual `""` and fails through the normal comparison path.
- **`path is t`** where `t` ∈ `string | number | bool | array | object |
  null` — asserts the gjson type of the result: `gjson.String`,
  `gjson.Number`, `True|False`, `IsArray()`, `IsObject()`, `gjson.Null`. The
  type set is validated in `ParseExpr` (typos fail `validate`). A missing path
  fails with a "(missing)" message. In a standalone assertion (`payload ==
  nil`) it is `EXPECT_INVALID` — captured vars are always strings, so a type
  assertion is meaningless there.

Operator-table placement: ` not contains ` precedes ` contains `; ` matches `
sits after the contains pair; ` is ` is last so the short token cannot steal a
split from a longer operator appearing earlier in the expression.

## Deliberate non-choices

- **No `not matches`** — invert the pattern inside the regex; a second
  negation form is not worth the surface.
- **No `is` in standalone assertions** — captured vars are strings by
  construction; allowing it would assert a constant.
- **Regex is unanchored** — matches Go's `MatchString` default; users write
  `^...$` when they mean whole-value.
- **No boolean combinators (and/or/not-expressions)** — the `expect:` list is
  already a conjunction; anything richer belongs in multiple steps.

## Verification

- `internal/tcli/expr_test.go` pins: parse forms for all four operators; the
  suffix-ordering trap (`not exists` before `exists`); the raw-JSON whole-body
  `not contains` behavior (nested plaintext fails, masked passes, positive
  `contains` on an object path); regex pass/fail/invalid; `is` across all six
  types plus the `==`-passes-for-both regression gap; standalone behaviors.
- Docs updated in `docs/cli-tcli.md` (operator table) and
  `skills/aidev-tcli/SKILL.md` (operator list + desensitization pattern).
- `make check` passes from the isolated worktree.
