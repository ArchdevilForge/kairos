"""Tests for the no-LLM alert runner."""

import pytest

from kairos import alert_runner


def _scan_with_setups():
    return {
        "success": True,
        "data": {
            "setups": [
                {"symbol": "AAA/USDT:USDT", "action_state": "watch", "risk": {}},
                {
                    "symbol": "BBB/USDT:USDT",
                    "direction": "long",
                    "setup_type": "box_breakout",
                    "action_state": "prepare",
                    "setup_score": 5.8,
                    "threshold": 5.5,
                    "risk": {
                        "entry_zone": [10.0, 10.03],
                        "structural_stop": 9.5,
                        "targets": [11.0, 12.0],
                        "risk_reward": 2.1,
                        "max_position_pct": 33.0,
                        "max_leverage": 5.0,
                    },
                    "reasons": ["4h structure is usable"],
                    "warnings": [],
                },
                {
                    "symbol": "CCC/USDT:USDT",
                    "direction": "short",
                    "setup_type": "range_breakdown",
                    "action_state": "trade_candidate",
                    "setup_score": 7.8,
                    "threshold": 7.5,
                    "risk": {
                        "entry_zone": [20.0, 20.1],
                        "structural_stop": 21.0,
                        "targets": [18.5],
                        "risk_reward": 2.4,
                        "max_position_pct": 20.0,
                        "max_leverage": 3.0,
                    },
                    "reasons": [
                        "1d trend supports short",
                        "BTC resonance supports direction",
                        "risk/reward 2.40 meets requirement 2.20",
                    ],
                    "warnings": ["15m volume confirmation missing"],
                },
            ]
        },
    }


def test_select_setups_defaults_to_prepare_and_trade_candidates_first():
    selected = alert_runner.select_setups(_scan_with_setups())

    assert [item["symbol"] for item in selected] == ["CCC/USDT:USDT", "BBB/USDT:USDT"]


@pytest.mark.asyncio
async def test_run_once_dry_run_prints_without_telegram(monkeypatch, capsys):
    monkeypatch.setattr(alert_runner, "scan_market", lambda exchange=None: _scan_with_setups())

    result = await alert_runner.run_once(dry_run=True)

    assert result == 0
    output = capsys.readouterr().out
    assert "<b>Kairos 机会筛选</b> | <b>非指令</b> 仅供人工判断" in output
    assert "<b>[交易候选] CCC 做空</b> | 区间跌破 | 7.8/7.5" in output
    assert "<b>匹配</b>: 日线顺势、BTC共振、盈亏比达标" in output
    assert "<b>缺口</b>: 缺量能确认" in output
    assert "<b>位</b>: 入 20.0 - 20.1 | 损 21.0 | 目 18.5" in output
    assert "<b>RR/上限</b>: 2.4 | 20.0% / 3.0x" in output
    assert "CCC/USDT:USDT" not in output
