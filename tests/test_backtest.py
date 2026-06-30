"""Tests for BacktestRunner covering initialization, trade simulation, PnL, and summary."""

from unittest.mock import MagicMock, patch

import numpy as np
import pytest

from kairos.analysis.box_pattern import BoxDetector, BoxPattern, BoxStatus
from kairos.analysis.cycle import CycleDetector, MarketCycle, MarketPhase
from kairos.backtest import (
    BacktestRunner,
    Direction,
    Trade,
    main,
)

# ── helpers ──────────────────────────────────────────────────────────────

BASE_TS = 1700000000000  # 2024-11-15 in ms
INTERVAL_MS = 14_400_000  # 4 hours


def make_ohlcv_arrays(n_bars=200, closes=None, highs=None, lows=None, volumes=None):
    """Create OHLCV arrays dict. If values not given, builds trend data."""
    ts = np.arange(n_bars) * INTERVAL_MS + BASE_TS
    if closes is None:
        closes = np.linspace(100, 150, n_bars) + np.random.default_rng(42).normal(0, 1, n_bars)
    else:
        closes = np.asarray(closes, dtype=float)
    if highs is None:
        highs = closes + np.abs(np.random.default_rng(42).normal(0, 2, n_bars))
    else:
        highs = np.asarray(highs, dtype=float)
    if lows is None:
        lows = closes - np.abs(np.random.default_rng(42).normal(0, 2, n_bars))
    else:
        lows = np.asarray(lows, dtype=float)
    if volumes is None:
        volumes = np.full(n_bars, 5000.0)
    else:
        volumes = np.asarray(volumes, dtype=float)

    return {
        "timestamps": ts,
        "opens": np.roll(closes, 1),
        "highs": highs,
        "lows": lows,
        "closes": closes,
        "volumes": volumes,
    }


def make_box(high=52000.0, low=50000.0, status=BoxStatus.CONVERGING, end_time=None, **kw):
    """Create a BoxPattern for mocking."""
    defaults = dict(
        symbol="BTC/USDT", timeframe="1h",
        high=high, low=low,
        start_time=BASE_TS,
        end_time=end_time or (BASE_TS + 49 * INTERVAL_MS),
        status=status,
        touch_high=3, touch_low=2,
        second_test_high=True,
        convergence_pct=0.8,
        volume_declining=True,
    )
    defaults.update(kw)
    return BoxPattern(**defaults)


def make_cycle(phase=MarketPhase.SPRING):
    return MarketCycle(
        phase=phase, confidence=0.8, btc_trend="up",
        btc_change_30d=15.0, btc_change_7d=5.0, volatility=3.0,
        volume_trend="increasing", altcoin_correlation=0.85,
        funding_rates_avg=0.01, market_cap_change_30d=10.0,
    )


# ── test 1: initialization ──────────────────────────────────────────────

def test_backtest_runner_initialization():
    """Creates BacktestRunner, checks attributes."""
    mock_ex = MagicMock()
    mock_ex.fetch_ohlcv = MagicMock()

    runner = BacktestRunner(exchange=mock_ex)
    assert runner.exchange is mock_ex
    assert isinstance(runner.box_detector, BoxDetector)
    assert isinstance(runner.cycle_detector, CycleDetector)

    # With custom box_detector
    bd = BoxDetector({"minBars": 5})
    runner2 = BacktestRunner.__new__(BacktestRunner)
    runner2.box_detector = bd
    runner2.logger = MagicMock()
    assert runner2.box_detector.min_bars == 5


# ── test 2: insufficient data ───────────────────────────────────────────

def test_run_no_trades_insufficient_data():
    """Short OHLCV series, returns empty trades."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.logger = MagicMock()

    # None data
    with patch.object(runner, "_fetch_historical", return_value=None):
        with pytest.raises(ValueError, match="Insufficient"):
            runner.run("BTC/USDT", "2024-01-01", "2024-06-01")

    # Too few bars
    data = make_ohlcv_arrays(n_bars=10)
    with patch.object(runner, "_fetch_historical", return_value=data):
        with pytest.raises(ValueError, match="Insufficient"):
            runner.run("BTC/USDT", "2024-01-01", "2024-06-01")


# ── test 3: box breakout long ───────────────────────────────────────────

def test_run_with_box_breakout_long():
    """Mock OHLCV that forms a box then breaks up → 1 long trade with positive PnL."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    runner.cycle_detector = CycleDetector()
    runner.logger = MagicMock()

    # Explicit data: box high=52000, breakthrough at 52000*1.005=52260. Bar 70 high=53000.
    n = 200
    ts = np.arange(n) * INTERVAL_MS + BASE_TS
    closes = np.linspace(50000, 56000, n)
    highs = closes + 200
    lows = closes - 200
    volumes = np.full(n, 5000.0)
    highs[70] = 53000.0   # above breakout threshold 52260
    volumes[70] = 20000.0  # volume spike

    data = {
        "timestamps": ts, "opens": np.roll(closes, 1),
        "highs": highs, "lows": lows, "closes": closes, "volumes": volumes,
    }

    box = make_box(high=52000.0, low=50000.0, end_time=data["timestamps"][60])

    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.SPRING)),
    ):
        result = runner.run("BTC/USDT", "2024-01-01", "2024-06-01")

    assert len(result["trades"]) >= 1
    trade = result["trades"][0]
    assert trade["direction"] == "long"
    assert trade["entry_price"] == pytest.approx(52260.0)
    assert trade["pnl_pct"] > 0


# ── test 4: box breakout short ──────────────────────────────────────────

def test_run_with_box_breakout_short():
    """Mock OHLCV that forms a box then breaks down → 1 short trade."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    runner.cycle_detector = CycleDetector()
    runner.logger = MagicMock()

    # Explicit data: box low=3300, breakdown threshold=3300*0.995=3283.5. Bar 70 low=3250.
    n = 200
    ts = np.arange(n) * INTERVAL_MS + BASE_TS
    closes = np.linspace(3400, 3100, n)
    highs = closes + 50
    lows = closes - 50
    volumes = np.full(n, 5000.0)
    lows[70] = 3250.0     # below breakdown threshold
    volumes[70] = 20000.0  # volume spike

    data = {
        "timestamps": ts, "opens": np.roll(closes, 1),
        "highs": highs, "lows": lows, "closes": closes, "volumes": volumes,
    }

    box = make_box(high=3500.0, low=3300.0, end_time=data["timestamps"][60])

    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.WINTER)),
    ):
        result = runner.run("BTC/USDT", "2024-01-01", "2024-06-01")

    assert len(result["trades"]) >= 1
    trade = result["trades"][0]
    assert trade["direction"] == "short"


# ── test 5: stop loss hit ───────────────────────────────────────────────

def test_run_stop_loss_hit():
    """Breakout then price reverses through stop."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    runner.cycle_detector = CycleDetector()
    runner.logger = MagicMock()

    # Data: breakout up, then reversal below box low
    n = 200
    ts = np.arange(n) * INTERVAL_MS + BASE_TS
    closes = np.full(n, 50000.0)
    highs = np.full(n, 50100.0)
    lows = np.full(n, 49900.0)
    volumes = np.full(n, 5000.0)

    # Breakout bar at 70: high > 52260, volume spike
    closes[70] = 52500.0
    highs[70] = 52800.0
    lows[70] = 52100.0
    volumes[70] = 20000.0

    # Reversal through stop at 75: low goes below 49750
    closes[75] = 49600.0
    highs[75] = 52000.0
    lows[75] = 49500.0

    data = {
        "timestamps": ts, "opens": np.roll(closes, 1),
        "highs": highs, "lows": lows, "closes": closes, "volumes": volumes,
    }

    box = make_box(end_time=data["timestamps"][60])

    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.SPRING)),
    ):
        result = runner.run("BTC/USDT", "2024-01-01", "2024-06-01")

    assert len(result["trades"]) >= 1
    trade = result["trades"][0]
    assert trade["exit_reason"] == "stop_loss"
    assert trade["pnl_pct"] < 0


# ── test 6: target hit ──────────────────────────────────────────────────

def test_run_target_hit():
    """Breakout then price reaches target."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    runner.cycle_detector = CycleDetector()
    runner.logger = MagicMock()

    n = 200
    ts = np.arange(n) * INTERVAL_MS + BASE_TS
    closes = np.full(n, 50000.0)
    highs = np.full(n, 50100.0)
    lows = np.full(n, 49900.0)
    volumes = np.full(n, 5000.0)

    # Breakout bar at 70
    closes[70] = 52500.0
    highs[70] = 52800.0
    lows[70] = 52100.0
    volumes[70] = 20000.0

    # Target hit at 72: high above 52260 + max(2000, 522.6) = 54260
    closes[72] = 54000.0
    highs[72] = 54300.0
    lows[72] = 53500.0

    data = {
        "timestamps": ts, "opens": np.roll(closes, 1),
        "highs": highs, "lows": lows, "closes": closes, "volumes": volumes,
    }

    box = make_box(end_time=data["timestamps"][60])

    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.SPRING)),
    ):
        result = runner.run("BTC/USDT", "2024-01-01", "2024-06-01")

    assert len(result["trades"]) >= 1
    trade = result["trades"][0]
    assert trade["exit_reason"] == "take_profit"
    assert trade["pnl_pct"] > 0


# ── test 7: PnL calculation long ────────────────────────────────────────

def test_pnl_calculation_long():
    """Verify PnL math for long trade."""
    assert BacktestRunner._calc_pnl(Direction.LONG, 100.0, 110.0) == pytest.approx(10.0)
    assert BacktestRunner._calc_pnl(Direction.LONG, 100.0, 90.0) == pytest.approx(-10.0)
    assert BacktestRunner._calc_pnl(Direction.LONG, 100.0, 100.0) == pytest.approx(0.0)
    assert BacktestRunner._calc_pnl(Direction.LONG, 0.0, 100.0) == 0.0


# ── test 8: PnL calculation short ───────────────────────────────────────

def test_pnl_calculation_short():
    """Verify PnL math for short trade."""
    assert BacktestRunner._calc_pnl(Direction.SHORT, 100.0, 90.0) == pytest.approx(10.0)
    assert BacktestRunner._calc_pnl(Direction.SHORT, 100.0, 110.0) == pytest.approx(-10.0)
    assert BacktestRunner._calc_pnl(Direction.SHORT, 100.0, 100.0) == pytest.approx(0.0)


# ── test 9: summary statistics ──────────────────────────────────────────

def test_summary_statistics():
    """Verify summary counts for a list of trades."""
    trades = [
        Trade("BTC/USDT", "long", 50000, "t1", 55000, "t2", 10.0, "take_profit"),
        Trade("BTC/USDT", "short", 50000, "t3", 47500, "t4", 5.0, "take_profit"),
        Trade("BTC/USDT", "long", 50000, "t5", 49000, "t6", -2.0, "stop_loss"),
        Trade("BTC/USDT", "short", 50000, "t7", 51000, "t8", -2.0, "stop_loss"),
    ]
    summary = BacktestRunner._compute_summary(trades, "2024-01-01", "2024-01-03", 100.0)
    assert summary["total_trades"] == 4
    assert summary["winning_trades"] == 2
    assert summary["losing_trades"] == 2
    assert summary["win_rate_pct"] == 50.0
    assert summary["avg_win_pnl_pct"] == pytest.approx(7.5)
    assert summary["avg_loss_pnl_pct"] == pytest.approx(-2.0)
    assert summary["avg_pnl_pct"] == pytest.approx(2.75)
    # total_return: 1.0*1.10*1.05*0.98*0.98 = 1.109262 → 10.9262%
    assert summary["total_pnl_pct"] == pytest.approx(10.9262, abs=1e-4)


def test_summary_empty():
    s = BacktestRunner._compute_summary([], "2024-01-01", "2024-01-03", 100.0)
    assert s["total_trades"] == 0
    assert s["win_rate_pct"] == 0.0


# -- test 10: cycle supports direction --

def test_cycle_supports_spring_long_only():
    assert BacktestRunner._cycle_supports(MarketPhase.SPRING, Direction.LONG) is True
    assert BacktestRunner._cycle_supports(MarketPhase.SPRING, Direction.SHORT) is False


def test_cycle_supports_summer_long_only():
    assert BacktestRunner._cycle_supports(MarketPhase.SUMMER, Direction.LONG) is True
    assert BacktestRunner._cycle_supports(MarketPhase.SUMMER, Direction.SHORT) is False


def test_cycle_supports_winter_short_only():
    assert BacktestRunner._cycle_supports(MarketPhase.WINTER, Direction.SHORT) is True
    assert BacktestRunner._cycle_supports(MarketPhase.WINTER, Direction.LONG) is False


def test_cycle_supports_autumn_both():
    assert BacktestRunner._cycle_supports(MarketPhase.AUTUMN, Direction.LONG) is True
    assert BacktestRunner._cycle_supports(MarketPhase.AUTUMN, Direction.SHORT) is True


# -- test 11: detect breakout --

def test_detect_breakout_long():
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    box = make_box(high=52000, low=50000)
    volumes = np.full(100, 5000.0)
    volumes[50] = 20000.0
    result = runner._detect_breakout(box, 52400, 51900, volumes, 50)
    assert result == Direction.LONG


def test_detect_breakout_short():
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    box = make_box(high=52000, low=50000)
    volumes = np.full(100, 5000.0)
    volumes[50] = 20000.0
    result = runner._detect_breakout(box, 50100, 49600, volumes, 50)
    assert result == Direction.SHORT


def test_detect_breakout_no_volume_spike():
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    box = make_box(high=52000, low=50000)
    volumes = np.full(100, 5000.0)
    result = runner._detect_breakout(box, 52400, 51900, volumes, 50)
    assert result is None


# -- test 12: check exit --

def test_check_exit_long_target():
    runner = BacktestRunner.__new__(BacktestRunner)
    pos = {"direction": Direction.LONG, "entry_price": 50000, "entry_time": "t0", "stop": 49750, "target": 54000}
    t = runner._check_exit(pos, 54100, 53500, "t1", 0.0, 0.0)
    assert t is not None
    assert t.exit_reason == "take_profit"
    assert t.exit_price == 54000
    assert t.pnl_pct == pytest.approx(8.0)


def test_check_exit_long_stop():
    runner = BacktestRunner.__new__(BacktestRunner)
    pos = {"direction": Direction.LONG, "entry_price": 50000, "entry_time": "t0", "stop": 49750, "target": 54000}
    t = runner._check_exit(pos, 50000, 49600, "t1", 0.0, 0.0)
    assert t is not None
    assert t.exit_reason == "stop_loss"
    assert t.exit_price == 49750


def test_check_exit_no_exit():
    runner = BacktestRunner.__new__(BacktestRunner)
    pos = {"direction": Direction.LONG, "entry_price": 50000, "entry_time": "t0", "stop": 49750, "target": 54000}
    t = runner._check_exit(pos, 51000, 50200, "t1", 0.0, 0.0)
    assert t is None


# -- test 13: trade_to_dict --

def test_trade_to_dict():
    t = Trade("BTC/USDT", "long", 50000.0, "2024-01-01T00:00:00+00:00", 52000.0, "2024-01-02T00:00:00+00:00", 4.0, "take_profit")
    d = BacktestRunner._trade_to_dict(t)
    assert d["symbol"] == "BTC/USDT"
    assert d["direction"] == "long"
    assert d["pnl_pct"] == 4.0
    assert d["exit_reason"] == "take_profit"


# -- test 14: end-of-period exit --

def test_end_of_period_exit():
    """Open position not closed by SL/TP exits at last bar."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    runner.cycle_detector = CycleDetector()
    runner.logger = MagicMock()

    n = 200
    ts = np.arange(n) * INTERVAL_MS + BASE_TS
    closes = np.full(n, 50000.0)
    highs = np.full(n, 50100.0)
    lows = np.full(n, 49900.0)
    volumes = np.full(n, 5000.0)

    # Breakout at bar 70
    closes[70] = 52500.0
    highs[70] = 52800.0
    lows[70] = 52100.0
    volumes[70] = 20000.0

    # After entry at 52260 (box.high*1.005), price stays between stop (49750) and target (54260)
    # Keep price around 53000 - within range, no stop/target hit
    for i in range(71, n):
        closes[i] = 53000.0
        highs[i] = 53200.0
        lows[i] = 52800.0

    data = {
        "timestamps": ts, "opens": np.roll(closes, 1),
        "highs": highs, "lows": lows, "closes": closes, "volumes": volumes,
    }

    box = make_box(end_time=data["timestamps"][60])

    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.SPRING)),
    ):
        result = runner.run("BTC/USDT", "2024-01-01", "2024-06-01")

    trades = result["trades"]
    assert len(trades) == 1
    assert trades[0]["exit_reason"] == "end_of_period"
    # Entry at 52260, exit at 53000: gross (53000-52260)/52260*100 ≈ 1.416, net = 1.416 - 0.12 = 1.296
    assert trades[0]["pnl_pct"] == pytest.approx(1.296, abs=0.01)


# -- test 15: cycle blocks long in winter --

def test_cycle_blocks_trade():
    """Winter cycle should block long entries even if breakout occurs."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    runner.cycle_detector = CycleDetector()
    runner.logger = MagicMock()

    data = make_ohlcv_arrays(n_bars=200, closes=np.linspace(50000, 55000, 200))
    data["volumes"][70] = 20000.0

    box = make_box(end_time=data["timestamps"][60])

    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.WINTER)),
    ):
        result = runner.run("BTC/USDT", "2024-01-01", "2024-06-01")

    assert result["summary"]["total_trades"] == 0


# -- test 16: CLI help --

def test_cli_help(capsys):
    with patch("sys.argv", ["kairos-backtest", "--help"]):
        with pytest.raises(SystemExit):
            main()
    captured = capsys.readouterr()
    assert "kairos backtest" in captured.out.lower() or "usage" in captured.out.lower()


# -- test 17: fee deduction in PnL --

def test_fee_deduction():
    """Round-trip cost (fee + slippage) × 2 deducted from gross PnL."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    runner.cycle_detector = CycleDetector()
    runner.logger = MagicMock()

    data = make_ohlcv_arrays(n_bars=200, closes=np.linspace(50000, 55000, 200))
    data["volumes"][70] = 20000.0

    box = make_box(end_time=data["timestamps"][60])

    # With default fees: 0.04 + 0.02 per side = 0.12% round-trip
    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.SPRING)),
    ):
        result_fee = runner.run("BTC/USDT", "2024-01-01", "2024-06-01", fee_pct=0.04, slippage_pct=0.02)

    # With zero fees
    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.SPRING)),
    ):
        result_no_fee = runner.run("BTC/USDT", "2024-01-01", "2024-06-01", fee_pct=0.0, slippage_pct=0.0)

    assert len(result_fee["trades"]) == len(result_no_fee["trades"])
    if result_fee["trades"]:
        # Fee version should have lower PnL
        assert result_fee["trades"][0]["pnl_pct"] < result_no_fee["trades"][0]["pnl_pct"]
        # Difference should be ~0.12% (round_trip_cost)
        diff = result_no_fee["trades"][0]["pnl_pct"] - result_fee["trades"][0]["pnl_pct"]
        assert diff == pytest.approx(0.12, abs=0.005)


# -- test 18: position sizing --

def test_position_sizing():
    """Position sizing scales the equity curve contribution."""
    trades = [
        Trade("BTC/USDT", "long", 50000, "t1", 55000, "t2", 10.0, "take_profit"),
        Trade("BTC/USDT", "short", 50000, "t3", 47500, "t4", 5.0, "take_profit"),
    ]
    # 100% position: equity = 1.0 * 1.10 * 1.05 = 1.155 → +15.5%
    s_full = BacktestRunner._compute_summary(trades, "2024-01-01", "2024-01-03", 100.0)
    # 33% position: equity = 1.0 * 1.033 * 1.0165 = 1.0501 → +5.01%
    s_partial = BacktestRunner._compute_summary(trades, "2024-01-01", "2024-01-03", 33.0)

    assert s_full["total_pnl_pct"] == pytest.approx(15.5, abs=0.1)
    assert s_partial["total_pnl_pct"] == pytest.approx(5.01, abs=0.1)
    assert s_full["total_pnl_pct"] > s_partial["total_pnl_pct"]


# -- test 19: new summary fields --

def test_summary_new_fields():
    """Summary includes sharpe, calmar, profit_factor, avg_holding_hours."""
    trades = [
        Trade("BTC/USDT", "long", 50000, "2024-01-01T00:00:00+00:00", 55000, "2024-01-02T00:00:00+00:00", 10.0, "take_profit", holding_hours=24.0),
        Trade("BTC/USDT", "short", 50000, "2024-01-02T00:00:00+00:00", 47500, "2024-01-03T00:00:00+00:00", 5.0, "take_profit", holding_hours=24.0),
        Trade("BTC/USDT", "long", 50000, "2024-01-03T00:00:00+00:00", 49000, "2024-01-04T00:00:00+00:00", -2.0, "stop_loss", holding_hours=24.0),
    ]
    s = BacktestRunner._compute_summary(trades, "2024-01-01", "2024-01-04", 100.0)

    assert "sharpe_ratio" in s
    assert "calmar_ratio" in s
    assert "profit_factor" in s
    assert "avg_holding_hours" in s
    assert "position_pct" in s
    assert s["avg_holding_hours"] == 24.0
    assert s["profit_factor"] > 1.0  # wins > losses
    assert s["position_pct"] == 100.0


# -- test 20: BTC cycle data fallback --

def test_btc_cycle_fallback():
    """When btc_data is None, falls back to symbol's own data for cycle."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.box_detector = BoxDetector()
    runner.cycle_detector = CycleDetector()
    runner.logger = MagicMock()

    # Explicit data with guaranteed breakout
    n = 200
    ts = np.arange(n) * 14400000 + BASE_TS
    closes = np.full(n, 50000.0)
    highs = np.full(n, 50100.0)
    lows = np.full(n, 49900.0)
    volumes = np.full(n, 5000.0)
    highs[70] = 52800.0
    volumes[70] = 20000.0

    data = {
        "timestamps": ts, "opens": np.roll(closes, 1),
        "highs": highs, "lows": lows, "closes": closes, "volumes": volumes,
    }

    box = make_box(end_time=data["timestamps"][60])

    with (
        patch.object(runner, "_fetch_historical", return_value=data),
        patch.object(BoxDetector, "detect", return_value=[box]),
        patch.object(CycleDetector, "detect_phase", return_value=make_cycle(MarketPhase.SPRING)),
    ):
        # btc_data=None should work (uses symbol's own data)
        result = runner.run("ETH/USDT", "2024-01-01", "2024-06-01", btc_data=None)

    assert result["summary"]["total_trades"] >= 1


# -- test 21: holding hours in trade dict --

def test_trade_dict_holding_hours():
    """_trade_to_dict includes holding_hours."""
    t = Trade(
        "BTC/USDT", "long", 50000.0,
        "2024-01-01T00:00:00+00:00", 52000.0, "2024-01-02T00:00:00+00:00",
        4.0, "take_profit",
        holding_hours=24.0,
    )
    d = BacktestRunner._trade_to_dict(t)
    assert d["holding_hours"] == 24.0


# -- test 22: fetch_btc_cycle_data caching --

def test_fetch_btc_cycle_data_cache():
    """fetch_btc_cycle_data caches result per instance."""
    runner = BacktestRunner.__new__(BacktestRunner)
    runner.logger = MagicMock()
    runner._btc_data_cache = None

    mock_data = {"closes": np.array([100.0, 101.0]), "volumes": np.array([1000.0, 1100.0])}

    with patch.object(runner, "_fetch_historical", return_value=mock_data) as mock_fetch:
        result1 = runner.fetch_btc_cycle_data("2024-01-01", "2024-01-03")
        result2 = runner.fetch_btc_cycle_data("2024-01-01", "2024-01-03")

    assert result1 is mock_data
    assert result2 is mock_data
    mock_fetch.assert_called_once()  # cached, not refetched
