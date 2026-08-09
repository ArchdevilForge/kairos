from __future__ import annotations

import math


def calculate_metrics(trades: list, bankroll_series: list[float]) -> dict[str, float]:
    resolved = [t for t in trades if t.resolved]
    if not resolved:
        return {
            k: 0.0
            for k in (
                "sharpe",
                "sortino",
                "max_drawdown",
                "win_rate",
                "avg_win",
                "avg_loss",
                "brier_score",
            )
        }

    pnls = [t.pnl for t in resolved]
    wins = [p for p in pnls if p > 0]
    losses = [p for p in pnls if p < 0]

    win_rate = len(wins) / len(pnls) if pnls else 0.0
    avg_win = sum(wins) / len(wins) if wins else 0.0
    avg_loss = sum(losses) / len(losses) if losses else 0.0

    daily_returns = _compute_daily_returns(bankroll_series)
    sharpe = _sharpe(daily_returns)
    sortino = _sortino(daily_returns)
    max_dd = _max_drawdown(bankroll_series)

    brier = _brier_score(resolved)

    return {
        "sharpe": sharpe,
        "sortino": sortino,
        "max_drawdown": max_dd,
        "win_rate": win_rate,
        "avg_win": avg_win,
        "avg_loss": avg_loss,
        "brier_score": brier,
    }


def _compute_daily_returns(series: list[float]) -> list[float]:
    if len(series) < 2:
        return []
    returns: list[float] = []
    for i in range(1, len(series)):
        prev = series[i - 1]
        if prev <= 0:
            returns.append(0.0)
        else:
            returns.append((series[i] - prev) / prev)
    return returns


def _sharpe(daily_returns: list[float]) -> float:
    if not daily_returns:
        return 0.0
    n = len(daily_returns)
    if n < 2:
        return 0.0
    mean_r = sum(daily_returns) / n
    var = sum((r - mean_r) ** 2 for r in daily_returns) / (n - 1)
    std_r = math.sqrt(var) if var > 0 else 0.0
    if std_r == 0:
        return 0.0
    return (mean_r / std_r) * math.sqrt(365)


def _sortino(daily_returns: list[float]) -> float:
    if not daily_returns:
        return 0.0
    n = len(daily_returns)
    mean_r = sum(daily_returns) / n
    downside = [r for r in daily_returns if r < 0]
    if not downside:
        return float("inf") if mean_r > 0 else 0.0
    ds_var = sum(r**2 for r in downside) / len(daily_returns)
    ds_std = math.sqrt(ds_var) if ds_var > 0 else 0.0
    if ds_std == 0:
        return 0.0
    return (mean_r / ds_std) * math.sqrt(365)


def _max_drawdown(series: list[float]) -> float:
    if len(series) < 2:
        return 0.0
    peak = series[0]
    max_dd = 0.0
    for v in series[1:]:
        if v > peak:
            peak = v
        dd = (peak - v) / peak if peak > 0 else 0.0
        max_dd = max(max_dd, dd)
    return max_dd


def _brier_score(trades: list) -> float:
    total = 0.0
    count = 0
    for t in trades:
        if not t.resolved:
            continue
        prob = t.price if t.direction == "YES" else 1.0 - t.price
        # Use bucket_hit if available; fall back to PnL sign as proxy.
        if hasattr(t, "bucket_hit") and t.bucket_hit is not None:
            outcome = 1.0 if t.bucket_hit else 0.0
        else:
            outcome = 1.0 if t.pnl > 0 else 0.0
        total += (prob - outcome) ** 2
        count += 1
    return total / count if count > 0 else 0.0
