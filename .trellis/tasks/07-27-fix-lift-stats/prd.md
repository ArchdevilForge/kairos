# PRD: Fix lift_5m statistical validity

## Must fix (PR review)
1. `--include-shadow=false` filters outcomes too
2. Baseline excludes alert windows [ts-300, ts+900]
3. Same-hour lift = alertRate / expectedRandom (hour-stratified), Laplace on zero cells
4. Min-n status + CI; no "target >1.5×" on n=1
5. Snapshot minute dedupe on load
6. Strength-matched baseline (med60 buckets) + honest naming

## Done when
Tests cover the above; `make check` green; pushed to PR branch.
