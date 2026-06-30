"""Backtest engine for kairos trading strategies.

Runs box breakout detection and cycle analysis on historical OHLCV data,
simulates entries/exits with fees, slippage, position sizing, and produces
trade-level + summary statistics (Sharpe, Calmar, profit factor, holding time).
"""

from __future__ import annotations

import argparse
import json
import logging
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from enum import Enum
from typing import Any

import ccxt  # type: ignore[import-untyped]
import numpy as np

from kairos.analysis.box_pattern import BoxDetector, BoxPattern, BoxStatus
from kairos.analysis.cycle import CycleDetector, MarketPhase

logger = logging.getLogger(__name__)


class Direction(str, Enum):
    LONG = "long"
    SHORT = "short"


class ExitReason(str, Enum):
    STOP_LOSS = "stop_loss"
    TAKE_PROFIT = "take_profit"
    END_OF_PERIOD = "end_of_period"


@dataclass
class Trade:
    """A single completed trade."""

    symbol: str
    direction: str  # "long" | "short"
    entry_price: float
    entry_time: str  # ISO 8601 datetime
    exit_price: float
    exit_time: str  # ISO 8601 datetime
    pnl_pct: float
    exit_reason: str  # "stop_loss" | "take_profit" | "end_of_period"
    entry_time_ms: float = 0.0
    exit_time_ms: float = 0.0
    holding_hours: float = 0.0


def _ts_to_iso(ts_ms: float) -> str:
    """Convert millisecond Unix timestamp to ISO 8601 string."""
    return datetime.fromtimestamp(ts_ms / 1000, tz=timezone.utc).isoformat()


def _parse_date(date_str: str) -> int:
    """Parse 'YYYY-MM-DD' and return millisecond timestamp via ccxt."""
    return ccxt.Exchange.parse8601(date_str + "T00:00:00Z")  # type: ignore[no-any-return]


def _holding_hours(entry_iso: str, exit_iso: str) -> float:
    """Compute holding time in hours from ISO 8601 strings."""
    try:
        t1 = datetime.fromisoformat(entry_iso)
        t2 = datetime.fromisoformat(exit_iso)
        return round((t2 - t1).total_seconds() / 3600, 2)
    except Exception:
        return 0.0


class BacktestRunner:
    """Runs backtests for kairos box-breakout strategy on historical OHLCV data.

    Usage:
        runner = BacktestRunner("okx")
        result = runner.run("BTC/USDT", "2024-01-01", "2024-06-01", fee_pct=0.04, slippage_pct=0.02, position_pct=33.0)
        print(result["summary"])
    """

    def __init__(self, exchange_id: str = "okx", exchange: Any = None, box_config: dict | None = None) -> None:
        self.exchange: Any
        if exchange is not None:
            self.exchange = exchange
        else:
            exchange_class = getattr(ccxt, exchange_id, None)
            if exchange_class is None:
                raise ValueError(f"Unknown exchange: {exchange_id}")
            self.exchange = exchange_class({"enableRateLimit": True})  # type: ignore[abstract]
        self.box_detector = BoxDetector(box_config or {})
        self.cycle_detector = CycleDetector()
        self.logger = logging.getLogger("kairos.backtest")
        self._btc_data_cache: dict[str, np.ndarray] | None = None  # cached BTC 1d data

    def run(
        self,
        symbol: str,
        start: str,
        end: str,
        timeframe: str = "4h",
        fee_pct: float = 0.04,
        slippage_pct: float = 0.02,
        position_pct: float = 100.0,
        btc_data: dict[str, np.ndarray] | None = None,
    ) -> dict[str, Any]:
        """Run backtest and return {"summary": {...}, "trades": [...]}.

        Args:
            symbol: Trading pair, e.g. "BTC/USDT".
            start: Start date "YYYY-MM-DD".
            end: End date "YYYY-MM-DD".
            timeframe: OHLCV timeframe, default "4h".
            fee_pct: Taker fee per side (default 0.04%).
            slippage_pct: Slippage per side (default 0.02%).
            position_pct: Capital deployed per trade (default 100%).
            btc_data: Pre-fetched BTC/USDT 1d OHLCV for cycle detection.
        """
        data = self._fetch_historical(symbol, start, end, timeframe)
        if data is None or len(data["closes"]) < 50:
            raise ValueError(
                f"Insufficient data for {symbol} {start}→{end}: got {len(data['closes']) if data else 0} bars"
            )

        closes: np.ndarray = data["closes"]
        highs: np.ndarray = data["highs"]
        lows: np.ndarray = data["lows"]
        volumes: np.ndarray = data["volumes"]
        timestamps: np.ndarray = data["timestamps"]
        n = len(closes)

        # Round-trip cost: fee + slippage on both entry and exit
        round_trip_cost = (fee_pct + slippage_pct) * 2

        # BTC cycle data: use pre-fetched 1d data, fall back to symbol's own data
        btc_closes = btc_data["closes"] if btc_data else closes
        btc_volumes = btc_data["volumes"] if btc_data else volumes

        # Detect all boxes on full data; we filter by end_time in the bar loop
        all_boxes = self.box_detector.detect(symbol, timeframe, highs, lows, closes, volumes, timestamps)

        # ponytail: recompute cycle per-bar using BTC 1d (or symbol fallback) data.
        CYCLE_INTERVAL = 12
        cycle = self._compute_cycle(
            btc_closes[: max(30, 50)], btc_volumes[: max(30, 50)]
        )

        trades: list[Trade] = []
        position: dict[str, Any] | None = None

        warmup = max(50, self.box_detector.min_bars * 2)

        # Map timestamps to index for BTC data slicing (BTC 1d vs symbol 4h mismatch)
        btc_len = len(btc_closes)

        for i in range(warmup, n):
            current_ts_ms = float(timestamps[i])
            current_ts_str = _ts_to_iso(current_ts_ms)

            # Per-bar cycle recomputation: slice BTC data up to current timestamp
            if i >= 30 and i % CYCLE_INTERVAL == 0:
                # Find BTC bars up to current bar's timestamp
                if btc_data is not None:
                    btc_idx = int(np.searchsorted(btc_data["timestamps"], current_ts_ms, side="right"))
                    btc_idx = min(btc_idx, btc_len)
                    c = btc_closes[: max(30, btc_idx)]
                    v = btc_volumes[: max(30, btc_idx)]
                else:
                    c = closes[: i + 1]
                    v = volumes[: i + 1]
                cycle = self._compute_cycle(c, v)

            # --- Check exit on existing position ---
            if position is not None:
                t = self._check_exit(position, highs[i], lows[i], current_ts_str, current_ts_ms, round_trip_cost)
                if t is not None:
                    t.symbol = symbol
                    trades.append(t)
                    position = None
                    continue

            # --- Entry signals ---
            active_boxes = [
                b for b in all_boxes
                if b.end_time <= current_ts_ms and b.status in (BoxStatus.CONVERGING, BoxStatus.FORMING)
            ]

            for box in active_boxes:
                direction = self._detect_breakout(box, highs[i], lows[i], volumes, i)
                if direction is None:
                    continue
                if not self._cycle_supports(cycle.phase, direction):
                    continue

                if direction == Direction.LONG:
                    entry_price = box.high * 1.005
                    stop = box.low * 0.995
                    target = entry_price + max(box.height, entry_price * 0.01)
                else:
                    entry_price = box.low * 0.995
                    stop = box.high * 1.005
                    target = entry_price - max(box.height, entry_price * 0.01)

                position = {
                    "direction": direction,
                    "entry_price": entry_price,
                    "entry_time": current_ts_str,
                    "entry_time_ms": current_ts_ms,
                    "stop": stop,
                    "target": target,
                }
                box.status = BoxStatus.BREAKOUT_UP if direction == Direction.LONG else BoxStatus.BREAKOUT_DOWN
                break

        # Close any remaining position at end
        if position is not None:
            last_close = float(closes[-1])
            last_ts = _ts_to_iso(timestamps[-1])
            gross_pnl = self._calc_pnl(position["direction"], position["entry_price"], last_close)
            pnl = gross_pnl - round_trip_cost
            trades.append(
                Trade(
                    symbol=symbol,
                    direction=position["direction"].value,
                    entry_price=position["entry_price"],
                    entry_time=position["entry_time"],
                    entry_time_ms=position.get("entry_time_ms", 0.0),
                    exit_price=last_close,
                    exit_time=last_ts,
                    exit_time_ms=float(timestamps[-1]),
                    pnl_pct=pnl,
                    exit_reason=ExitReason.END_OF_PERIOD.value,
                    holding_hours=_holding_hours(position["entry_time"], last_ts),
                )
            )

        summary = self._compute_summary(trades, start, end, position_pct)
        return {
            "summary": summary,
            "trades": [self._trade_to_dict(t) for t in trades],
        }

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _fetch_historical(self, symbol: str, start: str, end: str, timeframe: str) -> dict[str, np.ndarray] | None:
        """Fetch OHLCV from CCXT with pagination across the date range."""
        since_ms = _parse_date(start)
        end_ms = _parse_date(end)

        all_rows: list[list[float]] = []
        rate_limit_sec = float(getattr(self.exchange, "rateLimit", 1000)) / 1000.0

        while since_ms < end_ms:
            try:
                raw = self.exchange.fetch_ohlcv(symbol, timeframe, since=since_ms, limit=1000)
            except Exception as exc:
                self.logger.warning("fetch_ohlcv error at %s: %s", _ts_to_iso(since_ms), exc)
                break

            if not raw or len(raw) == 0:
                break

            all_rows.extend(raw)
            last_ts = raw[-1][0]
            if last_ts <= since_ms:
                break
            since_ms = int(last_ts) + 60000
            time.sleep(rate_limit_sec)

        if not all_rows:
            return None

        data = np.array(all_rows, dtype=float)
        if data.ndim != 2 or data.shape[1] < 6:
            return None

        return {
            "timestamps": data[:, 0],
            "opens": data[:, 1],
            "highs": data[:, 2],
            "lows": data[:, 3],
            "closes": data[:, 4],
            "volumes": data[:, 5],
        }

    def fetch_btc_cycle_data(self, start: str, end: str) -> dict[str, np.ndarray] | None:
        """Fetch BTC/USDT 1d OHLCV for cycle detection. Cached per instance."""
        if self._btc_data_cache is not None:
            return self._btc_data_cache
        data = self._fetch_historical("BTC/USDT", start, end, "1d")
        if data is not None:
            self._btc_data_cache = data
        return data

    def _compute_cycle(self, closes: np.ndarray, volumes: np.ndarray) -> Any:
        """Detect market cycle phase from price/volume data."""
        if len(closes) < 30:
            return self.cycle_detector._default_cycle()
        return self.cycle_detector.detect_phase(
            btc_prices=closes,
            btc_volumes=volumes,
        )

    @staticmethod
    def _cycle_supports(phase: MarketPhase, direction: Direction) -> bool:
        """Check if the cycle supports the given trade direction."""
        if phase == MarketPhase.SPRING:
            return direction == Direction.LONG
        if phase == MarketPhase.SUMMER:
            return direction == Direction.LONG
        if phase == MarketPhase.WINTER:
            return direction == Direction.SHORT
        return True  # AUTUMN: both

    def _detect_breakout(
        self,
        box: BoxPattern,
        bar_high: float,
        bar_low: float,
        volumes: np.ndarray,
        bar_idx: int,
    ) -> Direction | None:
        """Check if the box broke out on this bar. Returns direction or None."""
        vol_start = max(0, bar_idx - 19)
        avg_vol = float(np.mean(volumes[vol_start : bar_idx + 1]))

        if bar_high > box.high * 1.005 and float(volumes[bar_idx]) > avg_vol * 1.5:
            return Direction.LONG
        if bar_low < box.low * 0.995 and float(volumes[bar_idx]) > avg_vol * 1.5:
            return Direction.SHORT
        return None

    def _check_exit(
        self,
        position: dict[str, Any],
        bar_high: float,
        bar_low: float,
        current_ts: str,
        current_ts_ms: float,
        round_trip_cost: float,
    ) -> Trade | None:
        """Check if the current bar triggers a stop-loss or take-profit exit."""
        entry_price: float = position["entry_price"]
        direction: Direction = position["direction"]
        stop: float = position["stop"]
        target: float = position["target"]

        if direction == Direction.LONG:
            if bar_low <= stop:
                exit_price = stop
                reason = ExitReason.STOP_LOSS.value
            elif bar_high >= target:
                exit_price = target
                reason = ExitReason.TAKE_PROFIT.value
            else:
                return None
        else:  # SHORT
            if bar_high >= stop:
                exit_price = stop
                reason = ExitReason.STOP_LOSS.value
            elif bar_low <= target:
                exit_price = target
                reason = ExitReason.TAKE_PROFIT.value
            else:
                return None

        gross_pnl = self._calc_pnl(direction, entry_price, exit_price)
        pnl = gross_pnl - round_trip_cost
        return Trade(
            symbol="",
            direction=direction.value,
            entry_price=entry_price,
            entry_time=position["entry_time"],
            entry_time_ms=position.get("entry_time_ms", 0.0),
            exit_price=exit_price,
            exit_time=current_ts,
            exit_time_ms=current_ts_ms,
            pnl_pct=pnl,
            exit_reason=reason,
            holding_hours=_holding_hours(position["entry_time"], current_ts),
        )

    @staticmethod
    def _calc_pnl(direction: Direction, entry: float, exit_price: float) -> float:
        """Calculate gross P&L percentage (before costs)."""
        if entry == 0:
            return 0.0
        if direction == Direction.LONG:
            return (exit_price - entry) / entry * 100
        return (entry - exit_price) / entry * 100

    @staticmethod
    def _compute_summary(trades: list[Trade], start: str, end: str, position_pct: float) -> dict[str, Any]:
        """Compute summary statistics from a list of trades."""
        n = len(trades)
        if n == 0:
            return {
                "total_trades": 0,
                "winning_trades": 0,
                "losing_trades": 0,
                "win_rate_pct": 0.0,
                "avg_pnl_pct": 0.0,
                "total_pnl_pct": 0.0,
                "max_drawdown_pct": 0.0,
                "avg_win_pnl_pct": 0.0,
                "avg_loss_pnl_pct": 0.0,
                "sharpe_ratio": 0.0,
                "calmar_ratio": 0.0,
                "profit_factor": 0.0,
                "avg_holding_hours": 0.0,
                "position_pct": position_pct,
            }

        wins = [t for t in trades if t.pnl_pct > 0]
        losses = [t for t in trades if t.pnl_pct <= 0]
        pnl_list = np.array([t.pnl_pct for t in trades], dtype=float)

        # Position-scaled PnL contribution: pnl_pct * position_pct / 100
        scale = position_pct / 100.0

        # Equity curve
        equity = 1.0
        peak = 1.0
        max_dd = 0.0
        for t in trades:
            equity *= 1.0 + t.pnl_pct * scale / 100.0
            if equity > peak:
                peak = equity
            dd = (peak - equity) / peak * 100
            if dd > max_dd:
                max_dd = dd

        total_return = (equity - 1) * 100

        # Period length in years
        try:
            t1 = datetime.fromisoformat(start)
            t2 = datetime.fromisoformat(end)
            period_years = max((t2 - t1).total_seconds() / (365.25 * 86400), 1 / 365.25)
        except Exception:
            period_years = 1.0

        # Sharpe ratio: annualized, using position-scaled PnL
        scaled_pnl = pnl_list * scale
        trades_per_year = n / period_years if period_years > 0 else n
        mean_ret = float(np.mean(scaled_pnl))
        std_ret = float(np.std(scaled_pnl, ddof=1)) if n > 1 else 1.0
        sharpe = (mean_ret / std_ret * np.sqrt(trades_per_year)) if std_ret > 0 else 0.0

        # Calmar ratio: total_return / max_drawdown
        calmar = abs(total_return / max_dd) if max_dd > 0 else 0.0

        # Profit factor: sum(wins) / abs(sum(losses))
        sum_wins = sum(t.pnl_pct for t in wins)
        sum_losses = abs(sum(t.pnl_pct for t in losses))
        profit_factor = sum_wins / sum_losses if sum_losses > 0 else float("inf")

        # Average holding time
        avg_holding = float(np.mean([t.holding_hours for t in trades if t.holding_hours > 0])) if trades else 0.0

        return {
            "total_trades": n,
            "winning_trades": len(wins),
            "losing_trades": len(losses),
            "win_rate_pct": round(len(wins) / n * 100, 2),
            "avg_pnl_pct": round(float(np.mean(pnl_list)), 4),
            "total_pnl_pct": round(total_return, 4),
            "max_drawdown_pct": round(max_dd, 4),
            "avg_win_pnl_pct": round(float(np.mean([t.pnl_pct for t in wins])), 4) if wins else 0.0,
            "avg_loss_pnl_pct": round(float(np.mean([t.pnl_pct for t in losses])), 4) if losses else 0.0,
            "sharpe_ratio": round(sharpe, 4),
            "calmar_ratio": round(calmar, 4),
            "profit_factor": round(profit_factor, 4),
            "avg_holding_hours": round(avg_holding, 2),
            "position_pct": position_pct,
        }

    @staticmethod
    def _trade_to_dict(t: Trade) -> dict[str, Any]:
        return {
            "symbol": t.symbol,
            "direction": t.direction,
            "entry_price": t.entry_price,
            "entry_time": t.entry_time,
            "exit_price": t.exit_price,
            "exit_time": t.exit_time,
            "pnl_pct": round(t.pnl_pct, 4),
            "exit_reason": t.exit_reason,
            "holding_hours": t.holding_hours,
        }


# ------------------------------------------------------------------
# CLI entry point
# ------------------------------------------------------------------


def _run_single(
    runner: BacktestRunner,
    symbol: str,
    start: str,
    end: str,
    timeframe: str,
    fee_pct: float,
    slippage_pct: float,
    position_pct: float,
    btc_data: dict | None,
) -> dict[str, Any] | None:
    """Run backtest for a single symbol. Returns result or None on error."""
    try:
        return runner.run(symbol, start, end, timeframe, fee_pct, slippage_pct, position_pct, btc_data=btc_data)
    except Exception as exc:
        return {"symbol": symbol, "error": str(exc)}


def main() -> None:
    """CLI entry point for kairos backtest."""
    parser = argparse.ArgumentParser(description="kairos backtest — strategy backtesting on historical OHLCV")
    parser.add_argument("--symbol", nargs="+", required=True, help="Trading pair(s), e.g. BTC/USDT ETH/USDT")
    parser.add_argument("--start", required=True, help="Start date YYYY-MM-DD")
    parser.add_argument("--end", required=True, help="End date YYYY-MM-DD")
    parser.add_argument("--timeframe", default="4h", help="OHLCV timeframe (default: 4h)")
    parser.add_argument("--exchange", default="okx", help="CCXT exchange id (default: okx)")
    parser.add_argument("--min-bars", type=int, default=10, help="Minimum bars for box formation (default: 10, lower = more signals)")
    parser.add_argument("--fee", type=float, default=0.04, help="Taker fee %% per side (default: 0.04)")
    parser.add_argument("--slippage", type=float, default=0.02, help="Slippage %% per side (default: 0.02)")
    parser.add_argument("--position-pct", type=float, default=100.0, help="Capital %% per trade (default: 100)")
    parser.add_argument("--parallel", type=int, default=2, help="Max parallel fetches (default: 2)")
    args = parser.parse_args()

    symbols: list[str] = args.symbol

    # Fetch BTC 1d data once for cycle detection (shared across all symbols)
    base_runner = BacktestRunner(exchange_id=args.exchange, box_config={"minBars": args.min_bars})
    btc_data = base_runner.fetch_btc_cycle_data(args.start, args.end)
    if btc_data is None and symbols != ["BTC/USDT"]:
        base_runner.logger.warning("Failed to fetch BTC 1d data, falling back to symbol's own data for cycle")

    if len(symbols) == 1:
        runner = BacktestRunner(exchange_id=args.exchange, box_config={"minBars": args.min_bars})
        result = runner.run(
            symbol=symbols[0],
            start=args.start,
            end=args.end,
            timeframe=args.timeframe,
            fee_pct=args.fee,
            slippage_pct=args.slippage,
            position_pct=args.position_pct,
            btc_data=btc_data,
        )
        print(json.dumps(result, indent=2, ensure_ascii=False))
        return

    # Multi-symbol: parallel execution with shared BTC data
    results: dict[str, Any] = {}
    from concurrent.futures import ThreadPoolExecutor, as_completed

    with ThreadPoolExecutor(max_workers=args.parallel) as executor:
        futures = {
            executor.submit(
                _run_single,
                BacktestRunner(exchange_id=args.exchange, box_config={"minBars": args.min_bars}),
                sym, args.start, args.end, args.timeframe,
                args.fee, args.slippage, args.position_pct,
                btc_data,
            ): sym
            for sym in symbols
        }
        for future in as_completed(futures):
            sym = futures[future]
            r_result: dict[str, Any] | None = future.result()
            if r_result is not None:
                results[sym] = r_result

    total_trades = sum(r.get("summary", {}).get("total_trades", 0) for r in results.values() if r and "summary" in r)
    combined = {
        "symbols": results,
        "combined_summary": {
            "total_symbols": len(symbols),
            "total_trades": total_trades,
        },
    }
    print(json.dumps(combined, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
