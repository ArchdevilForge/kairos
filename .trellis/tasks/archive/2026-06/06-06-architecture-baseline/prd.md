# Document architecture baseline

## Goal

Create `docs/architecture.md` as the authoritative architecture baseline for kairos after the design review, codebase inspection, and external best-practice research. The document should resolve current drift between README, `progress.md`, Hermes skills, config examples, and actual MCP/WebSocket implementation.

## What I Already Know

- kairos is now scoped as a signal discovery, structured analysis, chart/explanation, and risk-advice system.
- kairos does not place orders, manage live positions, or execute trades.
- The scanner is the primary workflow; WebSocket anomalies are secondary candidate hints.
- Scoring and scanner orchestration belong in Kairos code and should be exposed through MCP tools.
- Hermes is responsible for scheduling, LLM veto, Telegram delivery, and LLM decision records.
- `docs/architecture.md` is the new source of truth; Hermes skills become operation manuals only.
- Existing docs contain drift:
  - README still describes a broad MCP + WebSocket system.
  - AGENTS mentions deprecated CLI commands.
  - `skills/kairos-scanner-orchestrator` still carries pipeline and scoring details that should move into Kairos code.

## Requirements

- Document product boundary and non-goals.
- Document scanner-first architecture and WebSocket secondary role.
- Document planned MCP tools: `scan_market` and `analyze_symbol_setup`.
- Document candidate scoring vs setup scoring.
- Document long/short support and stricter short constraints.
- Document cycle as weighted scoring input with phase-specific thresholds.
- Document risk output semantics: upper bounds only, no order-size instructions.
- Document persistence plan: SQLite first, JSONL export optional, 90-day detailed retention.
- Document standardized event and MCP response schemas.
- Document chart generation policy.
- Document configuration structure.
- Document deprecated CLI status and skills authority.
- Include external best-practice evidence summary.

## Acceptance Criteria

- [x] `docs/architecture.md` exists and describes the confirmed architecture decisions.
- [x] The document explicitly states that Kairos does not execute trades.
- [x] The document states scanner is primary and WebSocket anomalies are secondary.
- [x] The document defines score thresholds and RR requirements.
- [x] The document defines storage path, retention, and fact-vs-LLM responsibility split.
- [x] The document lists follow-up implementation implications without changing code.

## Definition of Done

- Document is internally consistent and matches the decisions confirmed in the grill session.
- No source code behavior is changed.
- Existing user changes are left untouched.
- Markdown is readable and suitable as a future implementation reference.

## Out of Scope

- Implementing `scan_market`.
- Updating README or Hermes skills.
- Adding SQLite tables or storage code.
- Changing MCP tool schemas in code.
- Refactoring WebSocket daemon lifecycle.

## Research References

- `research/best-practices.md` - External reliability, WebSocket, LLM, and risk-control takeaways.

## Technical Notes

- Applicable Trellis specs: backend directory structure and quality guidelines.
- Existing dirty worktree includes unrelated changes in analysis modules and generated/report files. This task must not modify or revert them.
