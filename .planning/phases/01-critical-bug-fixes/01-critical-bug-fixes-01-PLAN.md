---
phase: 01-critical-bug-fixes
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - src/pwatch/app/cli.py
autonomous: true
requirements:
  - R1
user_setup: []

must_haves:
  truths:
    - "`pwatch run` exits cleanly on Ctrl+C without restarting a second monitoring session"
    - "Failed symbol sync raises error visible to user (not silent exit)"
    - "Config listener errors are logged, not swallowed"
    - "All existing tests pass"
  artifacts:
    - path: "src/pwatch/app/cli.py"
      provides: "Single asyncio.run() invocation in cmd_run"
      contains: "asyncio.run(run_monitoring()) exactly once"
  key_links:
    - from: "src/pwatch/app/cli.py"
      to: "run_monitoring()"
      via: "asyncio.run() single call"
      pattern: "asyncio\\.run\\(run_monitoring\\(\\)\\)"
---

<objective>
Fix the duplicate `asyncio.run()` call in `cmd_run` that causes monitoring to restart after Ctrl+C.

Purpose: R1 — Prevents double invocation that restarts monitoring after user presses Ctrl+C, causing confusing behavior and resource leaks.
Output: Clean single-invocation entry point in cli.py.
</objective>

<execution_context>
@$HOME/.config/opencode/get-shit-done/workflows/execute-plan.md
@$HOME/.config/opencode/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@.planning/ROADMAP.md
@.planning/REQUIREMENTS.md
@src/pwatch/app/cli.py
</context>

<tasks>

<task type="auto">
  <name>Task 1: Remove duplicate asyncio.run() in cmd_run</name>
  <files>src/pwatch/app/cli.py</files>
  <read_first>
    - src/pwatch/app/cli.py (lines 585-604) — current cmd_run implementation
  </read_first>
  <action>
    In `src/pwatch/app/cli.py`, function `cmd_run` (lines 585-604):
    - DELETE line 597: `asyncio.run(run_monitoring())` — this is the duplicate call
    - KEEP line 595: `asyncio.run(run_monitoring())` — this is the correct single call
    - The function should have exactly ONE `asyncio.run(run_monitoring())` call
    
    Current code (lines 593-597):
    ```python
    try:
        _run_start_preflight()
        asyncio.run(run_monitoring())

        asyncio.run(run_monitoring())
    ```
    
    Target code after fix:
    ```python
    try:
        _run_start_preflight()
        asyncio.run(run_monitoring())
    ```
  </action>
  <verify>
    <automated>uv run pytest -v -x 2>&1 | tail -20</automated>
    <manual>grep -c "asyncio.run(run_monitoring())" src/pwatch/app/cli.py → must return 1</manual>
  </verify>
  <acceptance_criteria>
    - `grep -c "asyncio.run(run_monitoring())" src/pwatch/app/cli.py` returns exactly 1
    - `uv run pytest` passes with no failures
    - cmd_run function contains exactly one asyncio.run() call
  </acceptance_criteria>
  <done>Duplicate asyncio.run() removed, single call remains, all tests pass</done>
</task>

</tasks>

<verification>
- File exists: src/pwatch/app/cli.py with exactly one asyncio.run(run_monitoring()) call
- All existing tests pass: uv run pytest exits 0
- No new lint issues introduced: uv run ruff check src/pwatch/app/cli.py returns 0
</verification>

<success_criteria>
- cmd_run contains exactly one asyncio.run(run_monitoring()) call
- Ctrl+C during pwatch run exits cleanly without restarting
- All existing tests pass
</success_criteria>

<output>
After completion, create `.planning/phases/01-critical-bug-fixes/01-critical-bug-fixes-01-SUMMARY.md`
</output>
