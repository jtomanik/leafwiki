<!-- leafwiki
version: 1
page:
  id: Wsjd2V-vg
  title: Plans
  created_at: "2026-06-13T21:57:27.477434116Z"
  updated_at: "2026-06-15T10:14:36.193208Z"
  creator_id: system
  last_author_id: public-editor
tags:
  - plans
  - category
fields:
  phase: after-discovery
  purpose: implementation-planning
-->

# Plans

Plans are for deciding exactly what will be built, in what order, and how completion will be verified.

A plan should start only after the problem has been shaped enough to support implementation. It should turn discovery into scope, contracts, tests, sequencing, non-happy paths, and a Definition of Done.

## Workflow

Plans are downstream of [Signals](/signals) and [Discovery](/discovery).

Signals preserve the raw observation. Discovery turns that observation into a precise problem and evaluates possible shapes. Plans commit to an implementation path.

```text
Signals -> Discovery -> Plans
```

## Plans Capture

- The objective and non-goals.
- The behavioral contract to implement.
- The files, modules, or boundaries likely affected.
- The sequence of implementation tasks.
- The expected tests and verification commands.
- Non-happy paths and failure modes.
- The Definition of Done.

## Relationship To Earlier Stages

A plan should link back to the discovery page or signal that justified it when that context exists. That keeps implementation grounded in the original friction and the problem-shaping work, instead of becoming detached task execution.
