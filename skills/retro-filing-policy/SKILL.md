---
name: retro-filing-policy
description: >-
  Filing rules for the fullsend retro agent. Use when writing retro
  output after a PR closes or on /fs-retro.
---

# Retro Filing Policy

Keep the retrospective on the originating PR.

Required `agent-result.json` for an automatic PR-closed retro, and for
`/fs-retro` unless the human comment explicitly says to file issues:

- `proposals` must be `[]`
- Do not add proposal objects for this repository or any `target_repo`
- Put findings only in `summary`

When a `/fs-retro` comment explicitly says to file issues, add proposal
objects only for what that comment asks to file.

An empty `proposals` array is a valid retro result.
