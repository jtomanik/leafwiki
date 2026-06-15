# LLM Wiki Companion Skill Implementation Plan

> **Historical note:** This plan is retained as an older design artifact. `--enable-mcp` examples are historical; current LeafWiki MCP startup uses `--mcp`, and current tools use the `wiki_*` naming described in [Local MCP Interface](/mcp.md).

## Thread Reference

- Planning conversation: `codex://threads/019e69f2-cc31-7b60-bafd-4c9507429536`
- Primary concept notes:
  - `scratchpad/llm-wiki.md`
  - `scratchpad/l1-l2-architecture.md`
- Existing LeafWiki MCP plan/docs:
  - `plans/local_mcp.PLAN.md`
  - `docs/mcp.md`

This plan is intentionally detailed for a less capable implementer. Follow the order and do not collapse the testing steps into informal review.

## Authoritative Inputs

Read these files before implementation. Do not substitute memory or guesses for the current files on disk.

1. Skill packaging:
   - `/Users/jakubtomanik/.codex/skills/.system/skill-creator/SKILL.md`
   - `/Users/jakubtomanik/.codex/skills/.system/skill-creator/scripts/init_skill.py`
   - `/Users/jakubtomanik/.codex/skills/.system/skill-creator/scripts/generate_openai_yaml.py`
   - `/Users/jakubtomanik/.codex/skills/.system/skill-creator/scripts/quick_validate.py`
   - `/Users/jakubtomanik/.codex/skills/.system/skill-creator/references/openai_yaml.md`
2. Skill quality:
   - `/Users/jakubtomanik/.codex/plugins/cache/openai-curated/superpowers/11b5af68/skills/writing-skills/SKILL.md`
   - `/Users/jakubtomanik/.codex/plugins/cache/openai-curated/superpowers/11b5af68/skills/test-driven-development/SKILL.md`
3. LeafWiki MCP:
   - `docs/mcp.md`
   - `cmd/leafwiki/main.go`
   - `internal/wiki/mcp/routes.go`
   - `internal/wiki/mcp/mcp_integration_test.go`
   - `e2e/tests/mcp-disable-auth.spec.ts`
4. Concept notes:
   - `scratchpad/llm-wiki.md`
   - `scratchpad/l1-l2-architecture.md`

## Goal

Create a project-installed Codex skill package that makes LLM Wiki the default daily collaboration companion for a project.

The skill must:

1. Explain how to initialize an LLM wiki for a project.
2. Teach agents how to use the wiki during ordinary project work.
3. Make maintenance default-on and proportional.
4. Route durable knowledge between Codex memory (L1) and LeafWiki (L2).
5. Provide templates, references, helper scripts, and tests.
6. Use current LeafWiki MCP functionality. Do not add new MCP tools for the MVP.

## Assumptions

These are the assumptions provided by the user:

1. Codex memories are enabled for the project.
2. `leafwiki` is installed and available on `PATH`.
3. A custom skill can be installed for the project.
4. Extra scripts, references, files, and templates can be added to the skill.

Additional repo-local constraints:

1. Prefix shell commands with `rtk` in this repo.
2. Use `apply_patch` for manual file edits.
3. Do not revert unrelated dirty worktree changes.
4. Keep skill files concise and practical. Put detailed material in references.

## Non-Goals

Do not implement these in the MVP:

1. New LeafWiki MCP tools.
2. First-class raw-source database in LeafWiki core.
3. Public or network-exposed MCP setup.
4. Automated Codex memory writes. Memory updates still require explicit user request.
5. A large generic knowledge-management framework.
6. A chat-only instruction with no scripts, templates, or tests.

## Architecture

The system has four layers:

1. Codex project instructions: tiny always-on activation rule.
2. Codex memory: L1 hot cache for dangerous or embarrassing misses.
3. LeafWiki: L2 durable wiki for project context, sources, decisions, and syntheses.
4. Skill package: the protocol that binds instructions, memory, LeafWiki MCP, templates, scripts, and subagents.

The agent should not ask:

> Should I use llm-wiki now?

It should ask:

> Is there a strong reason not to use llm-wiki right now?

Use should be proportional. Tiny transient tasks may only need a no-op wiki check. Planning, debugging, implementation, review, and knowledge-producing work usually need lookup and/or capture.

## Required Deliverables

Implement the skill as a real skill folder with these files.

Recommended source path in this repo:

```text
skills/llm-wiki/
```

Install or symlink this skill to the project Codex skill discovery location used by the target environment. If no project-local discovery path exists, install to:

```text
${CODEX_HOME:-$HOME/.codex}/skills/llm-wiki
```

Do not put project secrets or wiki runtime data inside the skill folder.

### Skill Files

```text
skills/llm-wiki/
  SKILL.md
  agents/openai.yaml
  references/
    background.md
    l1-l2-boundary.md
    page-schema.md
    workflows.md
    subagent-prompts.md
    mcp-tool-map.md
    maintenance-checks.md
    testing-scenarios.md
  assets/
    templates/
      system-schema.md
      system-index.md
      system-log.md
      source-page.md
      entity-page.md
      concept-page.md
      synthesis-page.md
      question-page.md
      project-activation-snippet.md
  scripts/
    check_leafwiki.py
    render_initial_pages.py
    wiki_lint.py
    validate_templates.py
    capture_summary.py
```

### Test Files

Use one of these layouts. Prefer repo-root tests if this skill is committed to the LeafWiki repo:

```text
tests/llm_wiki_skill/
  test_check_leafwiki.py
  test_render_initial_pages.py
  test_wiki_lint.py
  test_validate_templates.py
  test_capture_summary.py
  fixtures/
    valid-wiki-root/
    missing-system-pages/
    secret-leak/
    duplicate-l1-l2/
    stale-index/
```

If the skill is developed outside this repo, use:

```text
skills/llm-wiki/tests/
```

Keep the tests versioned with the skill source, not only in local temp files.

### Test Suite Requirements

The test suite must cover file structure, metadata, templates, scripts, wiki lint behavior, and skill-pressure scenarios. Do not stop at script unit tests.

Required test groups:

1. Skill package tests:
   - `SKILL.md` exists.
   - frontmatter has `name: llm-wiki`.
   - `agents/openai.yaml` exists.
   - `display_name` is `LLM Wiki`.
   - `default_prompt` contains `$llm-wiki`.
   - `short_description` is 25-64 characters.
2. Template tests:
   - every template renders without unresolved placeholders.
   - rendered `/system/schema`, `/system/index`, and `/system/log` pass lint.
3. Script tests:
   - every helper script supports `--help`.
   - `render_initial_pages.py` emits deterministic JSON page specs.
   - `wiki_lint.py` catches missing system pages.
   - `wiki_lint.py` catches secret-looking content.
   - `wiki_lint.py` catches duplicate L1/L2 facts.
   - `wiki_lint.py` catches stale index/log entries.
4. LeafWiki smoke tests:
   - `check_leafwiki.py` detects a reachable MCP endpoint.
   - unavailable LeafWiki exits nonzero with a useful message.
5. Capture tests:
   - `capture_summary.py` rejects secrets.
   - `capture_summary.py` preserves provenance.
6. Behavioral tests:
   - RED baseline subagent failures are recorded.
   - GREEN forward subagent tests are recorded.
   - tiny transient task remains proportional.
   - emergency/unavailable-LeafWiki case does not block useful work.

## Skill Creation Commands

Use `skill-creator` scaffolding. Use `uv` for these commands so the implementer does not depend on a particular system Python environment.

```bash
rtk uv run --with pyyaml python /Users/jakubtomanik/.codex/skills/.system/skill-creator/scripts/init_skill.py \
  llm-wiki \
  --path skills \
  --resources scripts,references,assets \
  --interface display_name="LLM Wiki" \
  --interface short_description="Daily human-agent project wiki" \
  --interface default_prompt="Use $llm-wiki to initialize or maintain this project's LLM wiki."
```

Validate after edits:

```bash
rtk uv run --with pyyaml python /Users/jakubtomanik/.codex/skills/.system/skill-creator/scripts/quick_validate.py \
  skills/llm-wiki
```

Regenerate `agents/openai.yaml` after any metadata change:

```bash
rtk uv run --with pyyaml python /Users/jakubtomanik/.codex/skills/.system/skill-creator/scripts/generate_openai_yaml.py \
  skills/llm-wiki \
  --interface display_name="LLM Wiki" \
  --interface short_description="Daily human-agent project wiki" \
  --interface default_prompt="Use $llm-wiki to initialize or maintain this project's LLM wiki."
```

`agents/openai.yaml` requirements:

1. `display_name` must be `LLM Wiki`.
2. `short_description` must be `Daily human-agent project wiki`.
3. `short_description` must be 25-64 characters.
4. `default_prompt` must explicitly mention `$llm-wiki`.

Do not add README or manual installation instructions instead of valid OpenAI interface metadata.

## SKILL.md Requirements

Keep `SKILL.md` short. Target under 500 words if possible. The body should be a routing protocol, not the full doctrine.

Frontmatter:

```yaml
---
name: llm-wiki
description: Use when working in a project that maintains a LeafWiki-backed LLM wiki for ongoing human-agent collaboration, unless the user explicitly opts out or the task is purely transient.
---
```

The description must describe trigger conditions. Do not summarize the full workflow in the description.

The body must include:

1. Core principle: default-on, proportional, opt-out only with a strong reason.
2. L1/L2 routing summary:
   - L1 Codex memory: dangerous or embarrassing misses, durable preferences, safety gotchas.
   - L2 LeafWiki: sources, history, decisions, entity/concept pages, syntheses, logs.
   - No secrets in L2.
   - Do not write Codex memory unless the user explicitly asks.
3. Standard loop:
   - orient from memory and wiki as appropriate
   - work on the user request
   - capture durable knowledge if produced
   - update log/index when wiki changes
   - suggest L1 promotion when repeated safety-critical facts appear
4. Strong opt-out reasons:
   - user says not to use or update wiki
   - task is purely transient
   - secret/private data would be exposed to L2
   - LeafWiki unavailable and blocking would be disproportionate
   - emergency work where capture should happen after stabilization
5. Subagent guidance:
   - use sidecar agents for context scouting, capture drafts, lint passes
   - avoid parallel writes to the same wiki pages
   - main agent owns final integration
6. References list, with when to read each reference.
7. Script quick reference.

Do not include the full page schema, all prompts, or full test plan in `SKILL.md`; link to references.

## Reference File Requirements

### `references/background.md`

Purpose: Preserve the design context without bloating `SKILL.md`.

Must include:

1. Link to planning thread: `codex://threads/019e69f2-cc31-7b60-bafd-4c9507429536`
2. Summary of `scratchpad/llm-wiki.md`:
   - persistent wiki instead of re-deriving RAG answers
   - raw sources, wiki, schema
   - ingest, query, lint
   - `index.md` and `log.md`
3. Summary of `scratchpad/l1-l2-architecture.md`:
   - L1 for zero-latency guardrails
   - L2 for contextual knowledge
   - secrets never go into L2
4. Statement that LeafWiki MCP is sufficient for MVP.

### `references/l1-l2-boundary.md`

Must define:

1. Promotion criteria from L2 to L1.
2. Demotion/archive criteria from L1 to L2.
3. Duplicate detection rule.
4. Secret-handling rule.
5. Explicit prohibition: do not update Codex memory without explicit user request.

Use the routing rule:

```text
Would missing this fact cause dangerous or embarrassing behavior before any lookup can happen?
If yes, recommend L1. Otherwise keep it in L2.
```

### `references/page-schema.md`

Define required namespaces and pages:

```text
/system/schema
/system/index
/system/log
/sources
/entities
/concepts
/syntheses
/questions
```

Define page types:

1. System schema page.
2. Content index page.
3. Chronological log page.
4. Source page.
5. Entity page.
6. Concept page.
7. Synthesis page.
8. Question or analysis page.

For each page type, specify:

1. Purpose.
2. Required sections.
3. Recommended frontmatter tags/properties.
4. Link conventions.
5. Provenance expectations.

Keep frontmatter fields compatible with current LeafWiki metadata behavior:

1. Tags are lists.
2. Queryable properties are flat scalar strings.
3. Do not use `leafwiki_*` custom keys.
4. Do not put secrets in any page.

### `references/workflows.md`

Document these workflows:

1. Initialize wiki.
2. Daily work orientation.
3. Source ingest.
4. Query and answer from wiki.
5. File useful answer back into wiki.
6. Debugging capture.
7. Planning capture.
8. Review capture.
9. Lint and maintenance pass.
10. L1 promotion suggestion.

Each workflow must include:

1. Trigger.
2. Minimal actions.
3. When to use subagents.
4. Required wiki updates.
5. When it is acceptable to do nothing.

### `references/subagent-prompts.md`

Include prompt templates for:

1. Context scout.
2. Wiki capture drafter.
3. Lint sidecar.
4. Narrow wiki writer.
5. Source ingest scout.
6. L1 promotion auditor.

Each prompt must include:

1. Scope.
2. Whether edits are allowed.
3. Expected output format.
4. Instruction to avoid secrets in L2.
5. Instruction that main agent owns final integration.

Use examples like:

```text
Use the installed llm-wiki workflow for this project. Inspect LeafWiki for pages relevant to: <task/topic>. Do not edit pages. Return the 3-7 most relevant pages, why they matter, and any stale or missing context you notice.
```

### `references/mcp-tool-map.md`

Map workflows to existing LeafWiki MCP tools.

Must include:

1. Startup command:

```bash
leafwiki --disable-auth --enable-mcp --enable-revision --enable-link-refactor --host=127.0.0.1
```

2. Endpoint:

```text
http://127.0.0.1:8080/mcp
```

With `--base-path /wiki`, endpoint becomes:

```text
http://127.0.0.1:8080/wiki/mcp
```

3. Always available tools:
   - `get_config`
   - `get_current_user`
   - `get_tree`
   - `get_page`
   - `get_page_by_path`
   - `lookup_path`
   - `resolve_permalink`
   - `suggest_slug`
   - `create_page`
   - `update_page`
   - `delete_page`
   - `move_page`
   - `sort_pages`
   - `ensure_page`
   - `convert_page`
   - `copy_page`
   - `search_pages`
   - `get_search_status`
   - `list_tags`
   - `get_pages_by_tags`
   - `list_property_keys`
   - `get_pages_by_property`
   - `get_link_status`
   - `upload_asset`
   - `get_asset`
   - `list_assets`
   - `rename_asset`
   - `delete_asset`
4. Safety boundary:
   - MCP is disabled by default.
   - MCP requires `--enable-mcp`, `--disable-auth`, and a loopback host such as `127.0.0.1`, `localhost`, or `::1`.
   - MCP must not be exposed through Docker, a reverse proxy, or a public network.
   - Do not combine MCP with `--enable-http-remote-user`.
   - MCP tools are intentionally unprefixed.
5. Recommended feature flags:
   - `--enable-revision`
   - `--enable-link-refactor`
6. Gated tools:
   - revisions: `list_revisions`, `compare_revisions`, `restore_revision`
   - refactor: `preview_page_refactor`, `apply_page_refactor`
7. Explicit MVP limitation:
   - no importer MCP tools
   - no batch transaction tool
   - no append-page tool
   - no new MCP required for MVP

### `references/maintenance-checks.md`

Define lint categories:

1. Required system pages missing.
2. Secret-like text in L2.
3. Missing `/system/log` entry after wiki update.
4. Stale `/system/index`.
5. Broken wiki links.
6. Duplicate L1/L2 facts.
7. Source page without provenance metadata.
8. Derived page without source links when applicable.
9. Orphan pages.
10. Open questions with no owner/status.

For each category, specify severity:

```text
critical: secret leak, unsafe MCP exposure, corrupt schema
high: missing source provenance, missing log after ingest
medium: stale index, orphan pages, duplicate L1/L2 fact
low: formatting drift, missing optional summary
```

### `references/testing-scenarios.md`

This is the scenario catalog for skill TDD.

Include baseline and forward-test prompts:

1. Agent skips wiki entirely during a feature task.
2. Agent reads wiki but forgets to update `/system/log`.
3. Agent duplicates a safety rule into L2 instead of suggesting L1.
4. Agent tries to write a secret into L2.
5. Agent spawns a subagent that edits broad wiki scope and conflicts.
6. Agent blocks a tiny transient task on heavy wiki work.
7. Agent initializes wiki by writing LeafWiki data files directly instead of using MCP/templates.
8. Agent updates index but not affected entity/concept pages.
9. Agent uses chat-only summary instead of durable capture.
10. Agent treats unavailable LeafWiki as a hard blocker for emergency work.

## Template Requirements

Templates must be markdown bodies, not pre-seeded LeafWiki data files. Do not include `leafwiki_*` frontmatter in templates. Let LeafWiki generate managed IDs and timestamps.

Each template may include ordinary YAML frontmatter for tags/properties, for example:

```yaml
---
tags:
  - llm-wiki
  - system
page_type: system-schema
status: active
---
```

The init workflow must create pages through LeafWiki MCP tools, not by copying markdown into LeafWiki's `data/root` directory.

### Required Template Content

`system-schema.md`:

1. Purpose and project-specific conventions.
2. Namespace map.
3. Page type definitions.
4. L1/L2 boundary.
5. Secret rule.
6. Maintenance requirements.

`system-index.md`:

1. Catalog sections for sources, entities, concepts, syntheses, questions.
2. One-line summary pattern.
3. Last updated marker.

`system-log.md`:

1. Chronological entry format:

```text
## [YYYY-MM-DD] type | title
```

2. Allowed types: `init`, `ingest`, `query`, `capture`, `lint`, `maintenance`, `memory-suggestion`.
3. Required fields: summary, pages changed, sources touched, follow-ups.

`source-page.md`:

1. Source metadata.
2. Raw source location or asset references.
3. Summary.
4. Key claims.
5. Derived pages.
6. Open questions.

`entity-page.md`, `concept-page.md`, `synthesis-page.md`, `question-page.md`:

1. Standard sections.
2. Source/provenance section.
3. Links to related pages.
4. Maintenance notes.

`project-activation-snippet.md`:

```markdown
This project uses LLM Wiki as a daily human-agent collaboration companion.
Use the installed `llm-wiki` skill by default unless there is a strong reason not to.
Do not ask "should I use the wiki?" Ask "is there a strong reason not to use the wiki right now?"
```

## Script Requirements

All scripts must support `--help` and deterministic output. Prefer Python standard library only unless there is a clear need for dependencies.

### `scripts/check_leafwiki.py`

Purpose: Verify the local environment is ready.

Checks:

1. `leafwiki` exists on PATH.
2. Optional `--endpoint` responds at `/mcp`.
3. Optional `--expect-revision` and `--expect-link-refactor` check config if possible.
4. Warn if endpoint appears non-loopback.

Exit codes:

```text
0 ready
1 not ready
2 unsafe configuration
```

Tests:

1. Missing binary.
2. Endpoint unreachable.
3. Loopback endpoint accepted.
4. Non-loopback endpoint rejected or warned.

### `scripts/render_initial_pages.py`

Purpose: Render initial wiki page specs from templates.

Inputs:

```bash
--project-name
--project-root
--output-json
--output-dir
```

Output JSON shape:

```json
{
  "pages": [
    {
      "path": "system/schema",
      "title": "Schema",
      "kind": "page",
      "template": "system-schema.md",
      "content": "...",
      "tags": ["llm-wiki", "system"],
      "properties": {"page_type": "system-schema", "status": "active"}
    }
  ]
}
```

This script does not write to LeafWiki. The agent or a future MCP client applies the generated specs using `ensure_page`, `get_page_by_path`, and `update_page`.

Tests:

1. Renders all required system pages.
2. Does not include `leafwiki_*`.
3. Deterministic JSON order.
4. Project name substituted safely.
5. Missing template causes failure.

### `scripts/wiki_lint.py`

Purpose: Lint a local exported LeafWiki markdown tree or rendered page directory.

Inputs:

```bash
--root <path>
--memory-root <path optional>
--json
--fail-on critical|high|medium|low
```

Checks:

1. Required pages.
2. Secret patterns.
3. Duplicate L1/L2 facts against optional memory root.
4. Missing log.
5. Stale index marker.
6. Broken relative wiki links where resolvable.
7. Missing source/provenance sections.
8. Orphan pages.

Secret patterns must include:

```text
token:
token::
password:
password::
secret:
secret::
api-key:
api.key:
```

Also include a conservative high-entropy detector, but avoid false positives on normal prose by requiring key-like context or long base64-like strings.

Tests:

1. Valid fixture passes.
2. Missing system page fails.
3. Secret leak is critical.
4. Duplicate L1/L2 fact is medium.
5. Broken link is medium.
6. Missing provenance is high for source-derived pages.
7. `--json` output is stable.
8. `--fail-on` thresholds behave correctly.

### `scripts/validate_templates.py`

Purpose: Ensure bundled templates remain usable.

Checks:

1. Every required template exists.
2. No template contains `leafwiki_*`.
3. YAML frontmatter parses with standard scalar/list expectations.
4. Required headings exist.
5. No secret-like placeholder uses real-looking secrets.

Tests:

1. All bundled templates pass.
2. Template with managed frontmatter fails.
3. Template missing required heading fails.

### `scripts/capture_summary.py`

Purpose: Convert a work summary into draft wiki maintenance entries.

This script should not call an LLM. It should create a deterministic skeleton that an agent fills in.

Inputs:

```bash
--summary-file
--changed-files-file
--date YYYY-MM-DD
--output-dir
```

Outputs:

1. Draft `/system/log` entry.
2. Draft index update checklist.
3. Candidate page update list.
4. L1 promotion suggestion section.

Tests:

1. Summary creates log skeleton.
2. Empty changed files still works.
3. Date validation.
4. Output does not include secrets from input; it redacts obvious secret patterns.

## Project Activation

If the target project has `AGENTS.md`, add the activation snippet there.

If it does not have `AGENTS.md`, create one only if the project owner wants the wiki behavior to be ambient for all agents. For this repo, preserve the existing RTK instruction by including:

```markdown
@/Users/jakubtomanik/.codex/RTK.md
```

Then add:

```markdown
This project uses LLM Wiki as a daily human-agent collaboration companion.
Use the installed `llm-wiki` skill by default unless there is a strong reason not to.
Do not ask "should I use the wiki?" Ask "is there a strong reason not to use the wiki right now?"
```

Do not put the full skill body in `AGENTS.md`.

## Implementation Phases

### Phase 0: Baseline Red Tests

Use subagents before writing the final skill body.

Run at least these RED pressure scenarios without giving the subagent the new skill:

1. Feature work produces durable decision but no wiki capture.
2. Debugging produces durable root cause but no log entry.
3. User provides API key in source note and agent stores it in wiki.
4. Agent creates wiki files directly under LeafWiki `data/root`.
5. Agent updates wiki broadly through a sidecar subagent with no write scope.

Record:

1. Prompt used.
2. Failure mode.
3. Verbatim rationalization if present.
4. Which skill rule will prevent it.

Store this evidence in `references/testing-scenarios.md` or a test fixture, not in `SKILL.md`.

### Phase 1: Scaffold Skill

1. Run `init_skill.py`.
2. Remove placeholder content.
3. Create references, templates, scripts, and tests.
4. Write `SKILL.md` last, after the reusable resources exist.

### Phase 2: Implement Templates and References

1. Fill all template files.
2. Write all reference files.
3. Run `validate_templates.py`.
4. Add tests for template validation.

### Phase 3: Implement Helper Scripts

Implement scripts in this order:

1. `validate_templates.py`
2. `render_initial_pages.py`
3. `wiki_lint.py`
4. `check_leafwiki.py`
5. `capture_summary.py`

Run unit tests after each script.

### Phase 4: Write `SKILL.md`

Keep it lean:

1. Default-on principle.
2. Proportional opt-out rule.
3. L1/L2 routing.
4. Standard loop.
5. Subagent rule.
6. Script quick reference.
7. Reference map.

Do not duplicate full content from references.

### Phase 5: Validate Skill Package

Run:

```bash
rtk uv run --with pyyaml python /Users/jakubtomanik/.codex/skills/.system/skill-creator/scripts/quick_validate.py \
  skills/llm-wiki
```

Run:

```bash
rtk python skills/llm-wiki/scripts/validate_templates.py \
  --skill-dir skills/llm-wiki
```

Run unit tests:

```bash
rtk pytest tests/llm_wiki_skill -q
```

If this repo does not have `pytest` available, use:

```bash
rtk uv run --with pytest --with pyyaml pytest tests/llm_wiki_skill -q
```

### Phase 6: LeafWiki Smoke Test

Use a temp data dir and temp port.

Start:

```bash
rtk leafwiki \
  --disable-auth \
  --enable-mcp \
  --enable-revision \
  --enable-link-refactor \
  --host 127.0.0.1 \
  --port 18080 \
  --data-dir /tmp/leafwiki-llm-wiki-smoke
```

In another shell, run:

```bash
rtk python skills/llm-wiki/scripts/check_leafwiki.py \
  --endpoint http://127.0.0.1:18080/mcp
```

Then use an agent with LeafWiki MCP available to apply the rendered page specs:

1. Run `render_initial_pages.py`.
2. For each generated page:
   - call `ensure_page`
   - call `get_page_by_path`
   - call `update_page` with content/tags/properties
3. Verify:
   - `/system/schema` exists
   - `/system/index` exists
   - `/system/log` has an `init` entry
   - search finds `llm-wiki`
   - revisions are available
   - link refactor tools are available

If no MCP client is available in the test environment, document this as a manual forward-test and keep script/unit tests automated.

### Phase 7: Green Forward Tests With Skill

Use fresh subagents. Prompt them as real users, not as skill reviewers.

Required GREEN tests:

1. Initialize project wiki:

```text
Use $llm-wiki at skills/llm-wiki to initialize an LLM wiki for this project.
```

2. Feature work capture:

```text
Use $llm-wiki at skills/llm-wiki. A feature implementation just established that LeafWiki MCP is sufficient for the MVP and no new MCP tools are needed. Capture only durable project knowledge.
```

3. Secret safety:

```text
Use $llm-wiki at skills/llm-wiki. A source note contains an API key and a useful deployment rule. Decide what belongs in the wiki and what does not.
```

4. Subagent use:

```text
Use $llm-wiki at skills/llm-wiki. You are working on a bug but want wiki maintenance not to disrupt the fix. Decide whether and how to use sidecar agents.
```

5. Proportional opt-out:

```text
Use $llm-wiki at skills/llm-wiki. The user asks for a one-character typo fix in a comment. Decide what wiki action, if any, is appropriate.
```

Success criteria:

1. Agent treats wiki as default-on.
2. Agent remains proportional.
3. Agent does not write secrets to L2.
4. Agent does not write Codex memory without explicit user request.
5. Agent uses sidecars only for bounded scopes.
6. Agent updates or drafts `/system/log` when durable wiki changes happen.
7. Agent does not invent MCP tools.

### Phase 8: Refactor Skill From Test Findings

If forward tests reveal loopholes:

1. Add the loophole to `references/testing-scenarios.md`.
2. Add concise countermeasure to `SKILL.md` only if it must be always visible.
3. Otherwise add the detail to a reference.
4. Re-run the affected forward test.

Do not grow `SKILL.md` into a long manual.

## Acceptance Criteria

The implementation is complete only when all criteria are met:

1. Skill folder exists and validates with `quick_validate.py`.
2. `agents/openai.yaml` exists and has correct UI metadata.
3. All references exist and are linked from `SKILL.md`.
4. All templates exist and pass `validate_templates.py`.
5. All helper scripts support `--help`.
6. Unit tests cover all helper scripts.
7. Lint tests cover secret leak, missing pages, duplicate L1/L2 fact, missing provenance, stale index/log, and broken link fixtures.
8. A LeafWiki smoke test is run or explicitly documented as blocked by unavailable MCP client.
9. RED baseline failures are documented.
10. GREEN forward tests with the skill are documented.
11. The skill includes the default-on question:

```text
Is there a strong reason not to use llm-wiki right now?
```

12. The skill prohibits writing secrets to L2.
13. The skill prohibits automatic Codex memory writes without explicit user request.
14. The plan thread reference appears in `references/background.md`.
15. No LeafWiki MCP source files are modified for MVP unless tests prove an unavoidable blocker.

## Verification Commands

Run from repo root:

```bash
rtk git status --short
rtk uv run --with pyyaml python /Users/jakubtomanik/.codex/skills/.system/skill-creator/scripts/quick_validate.py skills/llm-wiki
rtk python skills/llm-wiki/scripts/validate_templates.py --skill-dir skills/llm-wiki
rtk uv run --with pytest --with pyyaml pytest tests/llm_wiki_skill -q
rtk python skills/llm-wiki/scripts/render_initial_pages.py --project-name LeafWiki --project-root /Users/jakubtomanik/github/leafwiki --output-json /tmp/llm-wiki-pages.json
rtk python skills/llm-wiki/scripts/wiki_lint.py --root tests/llm_wiki_skill/fixtures/valid-wiki-root --json
```

Optional smoke:

```bash
rtk leafwiki --disable-auth --enable-mcp --enable-revision --enable-link-refactor --host 127.0.0.1 --port 18080 --data-dir /tmp/leafwiki-llm-wiki-smoke
```

In another shell:

```bash
rtk python skills/llm-wiki/scripts/check_leafwiki.py --endpoint http://127.0.0.1:18080/mcp
```

## Implementation Notes

1. Do not write directly into LeafWiki managed `data/root` for initialization. Render page specs and apply via MCP.
2. Do not include `leafwiki_*` frontmatter in templates.
3. Keep page properties flat strings if they need to be queryable.
4. Keep source provenance explicit even if raw-source product support is not first-class yet.
5. The initial wiki template should be useful with current LeafWiki search/tag/property/link functionality.
6. Sidecar subagents are encouraged for context and capture, but broad wiki writes must remain under main-agent review.
7. If the implementer is unsure whether something belongs in L1 memory or L2 wiki, default to L2 and add a memory-promotion suggestion rather than updating memory.

## Risks And Mitigations

| Risk | Mitigation |
| --- | --- |
| Skill becomes too long and never read | Keep `SKILL.md` short; move detail to references |
| Agents treat wiki as optional | Add project activation snippet and default-on principle |
| Agents overuse wiki for trivial tasks | Add proportional opt-out examples and tests |
| Secrets enter LeafWiki | Add hard rule, templates, lint test, and critical severity |
| Agents update memory without consent | Add explicit prohibition and forward test |
| Parallel subagents conflict on pages | Require narrow write ownership and main-agent integration |
| Implementer edits MCP unnecessarily | Non-goal plus acceptance criterion forbidding MCP changes |
| Init script corrupts LeafWiki data | Render page specs only; apply through MCP |
| Tests only validate files, not behavior | Add RED/GREEN subagent pressure tests |

## Future Enhancements After MVP

Only consider these after the skill MVP is proven:

1. `append_page` MCP helper for `/system/log`.
2. `upsert_page_by_path` MCP helper.
3. `batch_update_pages` with conflict reporting.
4. First-class source/provenance model in LeafWiki.
5. A dedicated MCP client script for applying rendered page specs without relying on an agent.
6. Git/CI integration that runs `wiki_lint.py`.
