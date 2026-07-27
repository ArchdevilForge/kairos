# PRD: Coverage + full static checks (attention-lift)

## Goal
Close test gaps for snapshot store / lift helpers / pipeline snapshot write path; run full project static + race + coverage gates.

## Done when
- New unit tests for zero-ts skip, snapshot path, pipeline record helper, calibrate load/lift edges
- `make check` + `make cover-check` (+ cross-check if practical) pass
- Commit pushed to PR branch if code changed
