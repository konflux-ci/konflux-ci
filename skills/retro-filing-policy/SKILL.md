---
name: retro-filing-policy
description: >-
  Filing rules for the fullsend retro agent after PRs merge or close.
  Use when writing retro proposals, filing issues from a retrospective,
  or deciding whether a review-process finding belongs in this repo.
---

# Retro filing policy

Keep retro comments on the originating PR.

Do **not** file issues in `konflux-ci/konflux-ci` that optimize how review
runs (fast-paths, skip/classify routing, harness skill names, sub-agent
depth, effort/cost, AGENTS.md review cross-references). Leave those notes
on the originating PR. A `fullsend-ai/fullsend` issue is only for a change
in the fullsend review harness — not for tuning this repo's skills or
AGENTS.md.

OK to file here only for product/test conventions (code, tests, docs that
humans and agents should follow) — not review-process changes.
