# Align Live and Backtest Fidelity

## Goal

Make paper/live daemon behavior and live-mode backtest assumptions match closely enough that reported returns are useful for real trading decisions, not inflated by missing execution mechanics.

## What I Already Know

* User wants a complete fix, not another audit-only pass.
* Current daemon accepts `--stop-loss`, but live/paper execution does not implement stop-loss or exit management.
* Current live-mode backtest includes maker fill modeling and stop-loss assumptions that daemon paper trading does not share.
* Current paper trading immediately records every signal as an open position, which overstates maker fill probability on thin tail markets.
* Current daemon city universe comes from `DEFAULT_CITIES`, which diverges from prior active/liquid backtest city sets.
* Current paper settlement infers winners from prices close to 1.0, which is usable but less direct than resolution/winning fields.

## Requirements

* Implement stop-loss behavior in paper/live flow or explicitly align backtest to no-stop-loss. Prefer implementing daemon-side stop-loss because the CLI already exposes it.
* Reuse or mirror backtest fill assumptions in paper mode so dry-run results do not count every signal as filled.
* Keep backtest and daemon configuration aligned for strategy set, Kelly, city universe, bankroll, max position, and risk limits.
* Add order/position lifecycle handling: stale/unfilled orders, stop-loss exits, settlement, and duplicate protection must have a coherent state transition.
* Update tests to cover the new flow and prevent silent drift.

## Acceptance Criteria

* [ ] Daemon/paper honors `--stop-loss` in a testable way.
* [ ] Paper trading uses a fill model comparable to live-mode backtest.
* [ ] Live-mode backtest and daemon share the same fill/stop-loss configuration model where practical.
* [ ] City whitelist can be supplied explicitly and does not silently depend on stale `DEFAULT_CITIES` for live/paper operation.
* [ ] Paper settlement and P&L remain internally consistent for YES and NO positions.
* [ ] Unit tests cover stop-loss, fill model integration, and config alignment.
* [ ] `ruff`, `mypy`, and relevant tests pass.

## Definition of Done

* Tests added/updated.
* Lint and typecheck green.
* Behavior changes documented in task notes or README if user-facing CLI semantics change.
* Server rollout path clear after local verification.

## Out of Scope

* Real-money deployment without explicit user approval.
* Replacing CLOB client or changing wallet/credential handling.
* Full L2 order book simulator unless research shows a minimal version is necessary.

## Technical Notes

* Key files expected: `pm_bot/cli/daemon.py`, `pm_bot/core/paper_trade.py`, `pm_bot/backtest/engine.py`, `pm_bot/backtest/fill_model.py`, `pm_bot/core/risk.py`, CLI command definitions, tests.
* Research needed: practical maker fill modeling, prediction-market stop-loss/exit modeling, and Polymarket order lifecycle constraints.
