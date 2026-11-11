from __future__ import annotations

import json
from pathlib import Path

from notice_eval import evaluate_notice_cohorts, format_notice_eval_markdown


def _write_jsonl(path: Path, rows: list[dict]) -> None:
    path.write_text("\n".join(json.dumps(r, ensure_ascii=False) for r in rows) + "\n", encoding="utf-8")


def test_notice_eval_insufficient_sample_reports_counts_only(tmp_path: Path) -> None:
    notice_jf = tmp_path / "notice-journal.jsonl"
    trading_jf = tmp_path / "trading-journal.jsonl"

    _write_jsonl(
        notice_jf,
        [
            {
                "kind": "notice",
                "id": "ntc-1",
                "at": "2026-11-11T10:00:00Z",
                "payload": {
                    "notice_id": "ntc-1",
                    "ts": "2026-11-11T10:00:00Z",
                    "symbol": "SOL/USDT:USDT",
                    "delivery_status": "sent",
                },
            },
            {
                "kind": "notice",
                "id": "ntc-2",
                "at": "2026-11-11T10:15:00Z",
                "payload": {
                    "notice_id": "ntc-2",
                    "ts": "2026-11-11T10:15:00Z",
                    "symbol": "SOL/USDT:USDT",
                    "delivery_status": "suppressed",
                    "suppress_reason": "symbol_cooldown",
                },
            },
            {
                "kind": "notice_feedback",
                "id": "ntc-1",
                "at": "2026-11-11T10:05:00Z",
                "payload": {
                    "notice_id": "ntc-1",
                    "action": "acted",
                    "ticket_id": "tkt-1",
                },
            },
        ],
    )

    _write_jsonl(
        trading_jf,
        [
            {
                "kind": "ticket",
                "id": "tkt-1",
                "at": "2026-11-11T10:06:00Z",
                "payload": {
                    "id": "tkt-1",
                    "symbol": "SOL/USDT:USDT",
                    "direction": "long",
                    "status": "closed",
                    "created_at": 1794391560,
                    "closed_at": 1794395160,
                    "exit_price": 104.0,
                    "risk_plan": {"entry_price": 100.0, "stop_price": 98.0, "suggested_notional": 500.0},
                },
            },
            {
                "kind": "decision",
                "id": "tkt-1",
                "at": "2026-11-11T10:06:30Z",
                "payload": {
                    "ticket_id": "tkt-1",
                    "action": "accepted",
                    "closed_at": 1794395160,
                    "exit_price": 104.0,
                    "exit_reason": "TARGET_PARTIAL",
                },
            },
            {
                "kind": "ticket",
                "id": "tkt-2",
                "at": "2026-11-11T11:00:00Z",
                "payload": {
                    "id": "tkt-2",
                    "symbol": "ETH/USDT:USDT",
                    "direction": "long",
                    "status": "closed",
                    "created_at": 1794394800,
                    "closed_at": 1794398400,
                    "exit_price": 99.0,
                    "risk_plan": {"entry_price": 100.0, "stop_price": 99.0, "suggested_notional": 500.0},
                },
            },
            {
                "kind": "decision",
                "id": "tkt-2",
                "at": "2026-11-11T11:01:00Z",
                "payload": {
                    "ticket_id": "tkt-2",
                    "action": "accepted",
                    "closed_at": 1794398400,
                    "exit_price": 99.0,
                    "exit_reason": "PROTECTIVE_STOP",
                },
            },
        ],
    )

    summary = evaluate_notice_cohorts(notice_jf, trading_jf, min_trades_per_arm=30)
    assert summary.total_notices == 2
    assert summary.sent_notices == 1
    assert summary.suppressed_notices == 1
    assert summary.suppressed_by_reason == {"symbol_cooldown": 1}
    assert summary.feedback_acted == 1
    assert len(summary.notice_trades) == 1
    assert len(summary.non_notice_trades) == 1
    assert not summary.sufficient_sample

    md = format_notice_eval_markdown(summary, generated_date="2026-11-11")
    assert "仅报计数，不产出任何评估结论" in md
    assert "单笔均值 R" not in md


def test_notice_eval_unlocks_descriptive_table_when_min_sample_met(tmp_path: Path) -> None:
    notice_jf = tmp_path / "notice-journal.jsonl"
    trading_jf = tmp_path / "trading-journal.jsonl"

    notice_rows = []
    trading_rows = []
    base_ts = 1_760_000_000
    for i in range(3):
        nid = f"ntc-{i}"
        tid_n = f"tkt-n-{i}"
        tid_u = f"tkt-u-{i}"
        notice_rows.append(
            {
                "kind": "notice",
                "id": nid,
                "payload": {
                    "notice_id": nid,
                    "ts": "2026-11-11T10:00:00Z",
                    "symbol": "SOL/USDT:USDT",
                    "delivery_status": "sent",
                },
            }
        )
        notice_rows.append(
            {
                "kind": "notice_feedback",
                "id": nid,
                "payload": {"notice_id": nid, "action": "acted", "ticket_id": tid_n},
            }
        )
        for tid, exit_px in ((tid_n, 103.0), (tid_u, 99.0)):
            trading_rows.append(
                {
                    "kind": "ticket",
                    "id": tid,
                    "payload": {
                        "id": tid,
                        "symbol": "SOL/USDT:USDT" if tid == tid_n else "BTC/USDT:USDT",
                        "direction": "long",
                        "status": "closed",
                        "created_at": base_ts + i * 60,
                        "closed_at": base_ts + i * 60 + 300,
                        "exit_price": exit_px,
                        "risk_plan": {"entry_price": 100.0, "stop_price": 99.0, "suggested_notional": 500.0},
                    },
                }
            )
            trading_rows.append(
                {
                    "kind": "decision",
                    "id": tid,
                    "payload": {
                        "ticket_id": tid,
                        "action": "accepted",
                        "closed_at": base_ts + i * 60 + 300,
                        "exit_price": exit_px,
                    },
                }
            )

    _write_jsonl(notice_jf, notice_rows)
    _write_jsonl(trading_jf, trading_rows)

    summary = evaluate_notice_cohorts(notice_jf, trading_jf, min_trades_per_arm=3)
    assert summary.sufficient_sample
    md = format_notice_eval_markdown(summary, generated_date="2026-11-11")
    assert "通知来源交易 (Notice-Linked)" in md
    assert "非通知交易 (Non-Notice)" in md
