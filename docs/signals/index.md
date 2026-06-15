<!-- leafwiki
version: 1
page:
  id: VTmJGN-Dg
  title: Signals
  created_at: "2026-06-15T10:12:09.318903Z"
  updated_at: "2026-06-15T10:14:35.409552Z"
  creator_id: public-editor
  last_author_id: public-editor
tags:
  - signals
  - category
fields:
  phase: before-discovery
  purpose: raw-observations
-->

# Signals

Signals are the first trace that something may be worth improving.

They are raw observations, friction points, annoyances, surprising moments, repeated questions, undeveloped ideas, or glimpses of a better shape. A signal does not need to contain a solution. It only needs to capture that reality is telling us something.

A good signal page preserves the original spark as plainly as possible: what happened, what felt messy, what was harder than expected, what someone wished existed, or what pattern keeps showing up.

## Workflow

Signals are the raw input for [Discovery](/discovery). Discovery can later turn a signal into a precise problem, and [Plans](/plans) can turn that precise problem into an implementation path.

```text
Signals -> Discovery -> Plans
```

## Signals Capture

- Things that are not working as well as we want.
- Workflows that feel messy, slow, fragile, or confusing.
- Repeated user questions or support/debugging loops.
- Product gaps noticed while doing real work.
- Early ideas that are not yet ready for discovery.
- Concrete examples, quotes, screenshots, logs, or context that explain the friction.

## Relationship To Discovery

Signals are upstream of discovery.

```text
signals/
  What did we notice?
  What felt wrong or unnecessarily hard?
  What opportunity appeared?
  What raw example started the thread?

discovery/
  What problem are we actually solving?
  What constraints and context matter?
  What models are emerging?
  What have we agreed?
  What is still uncertain?

plans/
  What exactly will we build?
  In what order?
  How will we verify it?
```

Signals should stay close to the source. Discovery can later interpret them, connect them, reject them, or turn them into a precise problem worth planning.

## Guideline

Do not polish a signal too early. Capture the friction while it is still fresh. The point is not to be right yet; the point is to not lose the evidence that something deserves attention.
