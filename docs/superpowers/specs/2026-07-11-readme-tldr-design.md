# README TL;DR rewrite design

## Goal

Rewrite `README.md` for an engineer or team evaluating whether to adopt
`aidev-clis`. The reader should understand the project's purpose, operating
model, and security boundary within the first two screens, then reach a first
read-only call without reading a manual.

The README remains Chinese-first and uses short, direct prose. It is an
adoption page, not a second architecture reference or CLI manual.

## Narrative

The README follows one causal thread:

1. Local tests cannot establish every fact about a running system.
2. An agent therefore needs constrained capabilities to discover, observe,
   operate, and verify that system.
3. `aidev-clis` exposes those capabilities as independent CLIs with structured
   output, local credential injection, and audit records.
4. These controls reduce accidental exposure and make actions legible; they do
   not sandbox a hostile same-user process. Backend authorization and host-side
   approval remain the real enforcement boundaries.
5. A minimal install/discover/query sequence lets the reader validate the model.

## README structure

1. **Hero / TL;DR** — one-sentence definition, the running-system feedback-loop
   problem, and a compact example showing command plus JSON response.
2. **System view** — move the Mermaid diagram from `docs/ARCHITECTURE.md` here,
   immediately after the problem statement. Its job is orientation, not
   implementation detail.
3. **What the tools let an agent do** — group the six CLIs by intent:
   orient (`aidev`), observe (`dbcli`, `logcli`), act (`apicli`, `jcli`), and
   verify (`tcli`). Use a compact table.
4. **Why use this instead of credentials plus shell** — four concise contracts:
   structured AI-first output, locally injected credentials, bounded actions,
   and audit records that omit response bodies.
5. **Boundary** — a visually prominent, short statement of what the project is
   not and where authorization actually lives. Avoid absolute claims such as
   “safe” or “the agent never sees credentials.”
6. **Quick start** — install, run `aidev`, list targets, make one read-only DB
   query. Configuration prerequisites are explicit and linked.
7. **Install and configure** — retain macOS/Linux, Windows, and source paths,
   but remove explanations better owned by install scripts, `-h`, or CLI docs.
8. **Choose a CLI / go deeper** — task-oriented links to the six manuals and
   stable contracts, followed by the minimal contributor commands.

The current engineering-loop Mermaid is removed. Its useful idea is absorbed
into the opening problem statement; keeping two workflow diagrams would dilute
the TL;DR and repeat the same causal argument.

## Architecture document boundary

Remove the `System view` heading and Mermaid block from
`docs/ARCHITECTURE.md`. Rewrite the adjacent prose so the document independently
explains the runtime call path in text: caller → standalone CLI → local config,
credential/session, and audit primitives → backend. Preserve the subprocess
composition rule for `tcli` and the exceptional read-only config import used by
`aidev`. Do not link back to the README.

## Content rules

- Prefer a sentence or table row over a paragraph when both carry the same fact.
- Do not enumerate flags already discoverable from `-h`.
- Do not duplicate detailed adapter lists unless they help adoption decisions.
- State explicitly that audit payloads are not redacted and may contain
  sensitive command or request data; credential bytes are absent because they
  are injected separately, and response bodies are discarded.
- Keep all durable claims consistent with `OUTPUT-CONTRACT.md`,
  `SECURITY-MODEL.md`, and `ADAPTER-ISOLATION.md`.
- Preserve cross-platform installation parity.
- Use relative repository links and verify every local link resolves.

## Verification

The rewrite is complete when:

- the first two rendered screens answer what the project is, why it exists, how
  it connects to running systems, and where its trust boundary ends;
- the shortest path from installation to a read-only call is contiguous;
- `System view` exists only in the README, while the architecture document is
  self-contained in prose;
- no README prose re-enumerates command flags;
- all relative Markdown links and fenced Mermaid syntax validate;
- `make check` passes from the isolated worktree.
