# Browser regression fixtures

These fixtures are committed here so the CLI's browser tests run from a standalone
checkout, without sibling repositories or locally built packages.

Copied from `codevideo/codevideo-ide-react` at commit
`757d07d6a66038aec6a3fa6ef4f0850a7d287190`, under `scripts/golden/`:

- `fixture-selection.json`: initial selection, editor selection actions and portrait layout.
- `fixture-checkpoints.json`: snapshot restoration across resumable boundaries.
- `fixture-full.json`: a complete lesson with narration and file/terminal interactions.

Treat these as versioned regression inputs. When intentionally updating them from
the IDE repo, review the changes against the CLI's embedded frontend and run the
browser tests. Expected selection snapshots are computed using the published
VirtualIDE dev dependency pinned in `package-lock.json`.
