<!-- leafwiki
version: 1
page:
  id: gJhsrI-vR
  title: Discovery
  created_at: "2026-06-15T07:55:49.66408Z"
  updated_at: "2026-06-15T10:14:35.823263Z"
  creator_id: public-editor
  last_author_id: public-editor
tags:
  - discovery
  - category
fields:
  phase: before-plans
  purpose: problem-shaping
-->

# Discovery

Discovery is for deciding what is worth building before we decide how to build it.

> The hardest single part of building a software system is deciding precisely what to build.
>
> -- Fred Brooks, The Mythical Man-Month

This section is for shaping the problem, not pitching solutions. It collects observations, constraints, analogies, user needs, risks, rejected paths, and emerging models until the thing worth building becomes precise enough to plan.

A discovery page should preserve uncertainty without becoming vague. It should make the current state of thought inspectable: what might be true, what seems agreed, what remains unresolved, and what would need to be proven before implementation.

## Workflow

Discovery sits between [Signals](/signals) and [Plans](/plans).

Signals capture what was noticed. Discovery decides what the observation means and what problem is worth solving. Plans define what will be built, in what order, and how it will be verified.

```text
Signals -> Discovery -> Plans
```

## Discovery Pages Capture

- What problem are we actually solving?
- What constraints and context matter?
- What models or analogies are emerging?
- What options have been considered or rejected?
- What do we think is agreed so far?
- What questions remain open?

## Relationship To Plans

Discovery is upstream of plans.

```text
discovery/
  What might be true?
  What problem are we actually solving?
  What models are emerging?
  What have we agreed?
  What is still uncertain?

plans/
  What exactly will we build?
  In what order?
  How will we verify it?
```

Discovery is not pre-work. It is the work of reducing ambiguity until a plan can be precise.
