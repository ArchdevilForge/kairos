#!/usr/bin/env python3
"""Phase 3c Notice Offline Evaluation Skeleton (§3 B5).

口径说明:
- 仅读取离线 JSONL (`notice-journal.jsonl` + `trading-journal.jsonl`)，无任何在线调用或自动结论。
- 将来到期后比较「通知来源交易」(所有者标记 feedback=acted/做了 且关联到已结案交易票) 与
  「非通知交易」(同期未关联通知的已结案交易票) 的所有者实际交易结果。
- 严格样本门槛: 当任一组已结案样本数 < min_trades_per_arm (默认 30) 时，只输出客观计数，
  明确拒绝产出任何评估结论。
"""
from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
import json
from pathlib import Path
from statistics import median
from typing import Any

DEFAULT_MIN_TRADES_PER_ARM = 30
DEFAULT_LINK_WINDOW_HOURS = 4.0


@dataclass
class ClosedOwnerTrade:
    """Represents one closed accepted trade executed by the owner."""

    ticket_id: str
    symbol: str
    direction: str
    created_at_sec: int
    closed_at_sec: int
    realized_r: float
    exit_reason: str
    linked_notice_id: str | None = None


@dataclass
class NoticeEvalSummary:
    """Structured counts and optional arm metrics for Phase 3c Notice evaluation."""

    total_notices: int = 0
    sent_notices: int = 0
    dry_run_notices: int = 0
    failed_notices: int = 0
    suppressed_notices: int = 0
    suppressed_by_reason: dict[str, int] = field(default_factory=dict)

    feedback_seen: int = 0
    feedback_acted: int = 0
    feedback_ignored: int = 0
    feedback_pending: int = 0

    notice_trades: list[ClosedOwnerTrade] = field(default_factory=list)
    non_notice_trades: list[ClosedOwnerTrade] = field(default_factory=list)
    min_trades_per_arm: int = DEFAULT_MIN_TRADES_PER_ARM

    @property
    def sufficient_sample(self) -> bool:
        return (
            len(self.notice_trades) >= self.min_trades_per_arm
            and len(self.non_notice_trades) >= self.min_trades_per_arm
        )


def _parse_iso_to_unix(val: Any) -> int:
    if isinstance(val, (int, float)):
        return int(val)
    if not isinstance(val, str) or not val.strip():
        return 0
    s = val.strip()
    if s.endswith("Z"):
        s = s[:-1] + "+00:00"
    try:
        return int(datetime.fromisoformat(s).timestamp())
    except ValueError:
        return 0


def load_notice_journal(notice_journal_path: str | Path | None) -> tuple[dict[str, dict[str, Any]], dict[str, dict[str, Any]]]:
    """Load last-write-wins notices and feedbacks from notice-journal.jsonl."""
    notices: dict[str, dict[str, Any]] = {}
    feedbacks: dict[str, dict[str, Any]] = {}
    if not notice_journal_path:
        return notices, feedbacks
    p = Path(notice_journal_path)
    if not p.is_file():
        return notices, feedbacks

    for raw in p.read_text(encoding="utf-8").splitlines():
        raw = raw.strip()
        if not raw:
            continue
        try:
            line = json.loads(raw)
        except json.JSONDecodeError:
            continue
        kind = line.get("kind")
        payload = line.get("payload") or {}
        if not isinstance(payload, dict):
            continue
        nid = str(payload.get("notice_id") or line.get("id") or "").strip()
        if not nid:
            continue
        if kind == "notice":
            notices[nid] = payload
        elif kind == "notice_feedback":
            feedbacks[nid] = payload
    return notices, feedbacks


def _compute_realized_r(
    ticket: dict[str, Any],
    decision: dict[str, Any],
    outcome: dict[str, Any] | None,
) -> float | None:
    if outcome is not None:
        for k in ("net_r", "realized_r", "r_multiple"):
            v = outcome.get(k)
            if isinstance(v, (int, float)):
                return float(v)

    rp = ticket.get("risk_plan") or {}
    entry = float(rp.get("entry_price") or 0.0)
    stop = float(rp.get("stop_price") or 0.0)
    exit_px = float(decision.get("exit_price") or ticket.get("exit_price") or 0.0)
    one_r = abs(entry - stop)
    if entry <= 0 or one_r <= 0 or exit_px <= 0:
        if (decision.get("exit_reason") or ticket.get("exit_reason")) == "PROTECTIVE_STOP" or decision.get("touched_preset_stop"):
            return -1.0
        return None

    direction = str(ticket.get("direction") or "long").lower()
    if direction == "short":
        gross_r = (entry - exit_px) / one_r
    else:
        gross_r = (exit_px - entry) / one_r

    funding_usd = float(decision.get("realized_funding_usd") or ticket.get("realized_funding_usd") or 0.0)
    notional = float(rp.get("suggested_notional") or 0.0)
    one_r_usd = notional * (one_r / entry) if notional > 0 else 0.0
    funding_r = (funding_usd / one_r_usd) if one_r_usd > 0 else 0.0
    return gross_r - funding_r


def load_closed_owner_trades(trading_journal_path: str | Path | None) -> list[ClosedOwnerTrade]:
    """Load closed owner-accepted trades from trading-journal.jsonl."""
    if not trading_journal_path:
        return []
    p = Path(trading_journal_path)
    if not p.is_file():
        return []

    tickets: dict[str, dict[str, Any]] = {}
    decisions: dict[str, dict[str, Any]] = {}
    outcomes: dict[str, dict[str, Any]] = {}

    for raw in p.read_text(encoding="utf-8").splitlines():
        raw = raw.strip()
        if not raw:
            continue
        try:
            line = json.loads(raw)
        except json.JSONDecodeError:
            continue
        kind = line.get("kind")
        payload = line.get("payload") or {}
        if not isinstance(payload, dict):
            continue
        if kind == "ticket":
            tid = str(payload.get("id") or line.get("id") or "").strip()
            if tid:
                tickets[tid] = payload
        elif kind == "decision":
            tid = str(payload.get("ticket_id") or line.get("id") or "").strip()
            if tid:
                decisions[tid] = payload
        elif kind == "outcome":
            tid = str(payload.get("ticket_id") or line.get("id") or "").strip()
            if tid:
                outcomes[tid] = payload

    closed: list[ClosedOwnerTrade] = []
    for tid, tkt in tickets.items():
        dec = decisions.get(tid)
        if not dec or str(dec.get("action") or "") != "accepted":
            continue
        closed_at = int(dec.get("closed_at") or tkt.get("closed_at") or 0)
        status = str(tkt.get("status") or "")
        if closed_at <= 0 and status != "closed":
            continue
        r_val = _compute_realized_r(tkt, dec, outcomes.get(tid))
        if r_val is None:
            continue
        closed.append(
            ClosedOwnerTrade(
                ticket_id=tid,
                symbol=str(tkt.get("symbol") or ""),
                direction=str(tkt.get("direction") or ""),
                created_at_sec=int(tkt.get("created_at") or 0),
                closed_at_sec=closed_at,
                realized_r=r_val,
                exit_reason=str(dec.get("exit_reason") or tkt.get("exit_reason") or ""),
            )
        )
    closed.sort(key=lambda x: (x.created_at_sec, x.ticket_id))
    return closed


def evaluate_notice_cohorts(
    notice_journal_path: str | Path | None,
    trading_journal_path: str | Path | None,
    *,
    min_trades_per_arm: int = DEFAULT_MIN_TRADES_PER_ARM,
    link_window_hours: float = DEFAULT_LINK_WINDOW_HOURS,
) -> NoticeEvalSummary:
    """Partition closed owner trades into notice-sourced vs non-notice cohorts."""
    notices, feedbacks = load_notice_journal(notice_journal_path)
    trades = load_closed_owner_trades(trading_journal_path)

    summary = NoticeEvalSummary(min_trades_per_arm=max(1, int(min_trades_per_arm)))
    summary.total_notices = len(notices)

    explicit_ticket_to_notice: dict[str, str] = {}
    acted_notices_by_symbol: dict[str, list[tuple[int, str]]] = {}

    for nid, ntc in notices.items():
        status = str(ntc.get("delivery_status") or "")
        if status == "sent":
            summary.sent_notices += 1
        elif status == "dry_run":
            summary.dry_run_notices += 1
        elif status == "failed":
            summary.failed_notices += 1
        elif status == "suppressed":
            summary.suppressed_notices += 1
            reason = str(ntc.get("suppress_reason") or "unspecified")
            summary.suppressed_by_reason[reason] = summary.suppressed_by_reason.get(reason, 0) + 1

        fb = feedbacks.get(nid)
        if status in ("sent", "dry_run"):
            if not fb:
                summary.feedback_pending += 1
            else:
                act = str(fb.get("action") or "")
                if act == "seen":
                    summary.feedback_seen += 1
                elif act == "acted":
                    summary.feedback_acted += 1
                elif act == "ignored":
                    summary.feedback_ignored += 1
                else:
                    summary.feedback_pending += 1

        if fb and str(fb.get("action") or "") == "acted":
            linked_tid = str(fb.get("ticket_id") or "").strip()
            if linked_tid:
                explicit_ticket_to_notice[linked_tid] = nid
            sym = str(ntc.get("symbol") or "")
            ts_sec = _parse_iso_to_unix(ntc.get("ts"))
            if sym and ts_sec > 0:
                acted_notices_by_symbol.setdefault(sym, []).append((ts_sec, nid))

    for sym_list in acted_notices_by_symbol.values():
        sym_list.sort(key=lambda pair: pair[0])

    max_lag_sec = int(max(0.0, link_window_hours) * 3600)
    for tr in trades:
        linked_nid = explicit_ticket_to_notice.get(tr.ticket_id)
        if not linked_nid and tr.symbol in acted_notices_by_symbol and tr.created_at_sec > 0:
            for n_ts, nid in reversed(acted_notices_by_symbol[tr.symbol]):
                if 0 <= tr.created_at_sec - n_ts <= max_lag_sec:
                    linked_nid = nid
                    break
        if linked_nid:
            tr.linked_notice_id = linked_nid
            summary.notice_trades.append(tr)
        else:
            summary.non_notice_trades.append(tr)

    return summary


def _arm_stats_row(label: str, trades: list[ClosedOwnerTrade]) -> str:
    rs = [t.realized_r for t in trades]
    n = len(rs)
    mean_r = sum(rs) / n
    med_r = median(rs)
    total_r = sum(rs)
    stop_hits = sum(1 for t in trades if t.exit_reason == "PROTECTIVE_STOP" or t.realized_r <= -0.95)
    stop_rate = stop_hits / n * 100.0
    return f"| {label} | {n} | {mean_r:+.3f}R | {med_r:+.3f}R | {total_r:+.2f}R | {stop_rate:.1f}% |"


def format_notice_eval_markdown(summary: NoticeEvalSummary, generated_date: str | None = None) -> str:
    """Format offline Notice cohort summary; strictly count-only when sample is insufficient."""
    day = generated_date or datetime.now(timezone.utc).strftime("%Y-%m-%d")
    sup_parts = [f"{k}={v}" for k, v in sorted(summary.suppressed_by_reason.items())]
    sup_detail = ", ".join(sup_parts) if sup_parts else "无"

    lines = [
        f"# Phase 3c 机会通知层离线对比报告（{day}）",
        "",
        "> 口径说明：本报告仅汇总离线账本事实，将「通知来源交易（`feedback=acted/做了` 关联交易）」与同期「非通知交易」的所有者实际结案结果并列记录；不含任何自动信号背书。",
        "",
        "## 1. 通知与反馈计数",
        "",
        f"- 通知总记录数: `{summary.total_notices}`",
        f"- 已发送 (`sent`): `{summary.sent_notices}`（干跑 `dry_run`: `{summary.dry_run_notices}`，失败 `failed`: `{summary.failed_notices}`）",
        f"- 已抑制 (`suppressed`): `{summary.suppressed_notices}`（拆分: `{sup_detail}`）",
        f"- 所有者反馈计数: 看了=`{summary.feedback_seen}` · 做了=`{summary.feedback_acted}` · 忽略=`{summary.feedback_ignored}` · 待反馈=`{summary.feedback_pending}`",
        f"- 已结案「通知来源交易」样本数: `{len(summary.notice_trades)}`",
        f"- 已结案「非通知交易」样本数: `{len(summary.non_notice_trades)}`",
        "",
        "## 2. 队列结果对比",
        "",
    ]

    if not summary.sufficient_sample:
        lines.extend(
            [
                f"**样本不足**（通知来源交易 `n={len(summary.notice_trades)}`，非通知交易 `n={len(summary.non_notice_trades)}`，最低对比门槛每组 `≥{summary.min_trades_per_arm}` 笔已结案交易）：**仅报计数，不产出任何评估结论。**",
            ]
        )
    else:
        lines.extend(
            [
                "| 交易来源分组 | 已结案笔数 n | 单笔均值 R | 单笔中位数 R | 累计实现 R | 保护止损占比 |",
                "|---|---:|---:|---:|---:|---:|",
                _arm_stats_row("通知来源交易 (Notice-Linked)", summary.notice_trades),
                _arm_stats_row("非通知交易 (Non-Notice)", summary.non_notice_trades),
                "",
                "> 注：上表仅呈现所有者主观决策后的实际结案分布差异，解释权与去留决定权归所有者。",
            ]
        )

    return "\n".join(lines)
