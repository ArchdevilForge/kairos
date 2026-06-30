"""No-LLM scanner alerts for human-controlled trading."""

from __future__ import annotations

import argparse
import asyncio
import html
import os
import sys
from typing import Any

from kairos.scanner import scan_market
from kairos.telegram import TelegramClient

ACTION_RANK = {
    "no_trade": 0,
    "watch": 1,
    "prepare": 2,
    "trade_candidate": 3,
}


def select_setups(scan: dict[str, Any], min_state: str = "prepare", limit: int = 5) -> list[dict[str, Any]]:
    """Return scanner setups worth showing to a human."""
    raw_data = scan.get("data")
    data = raw_data if isinstance(raw_data, dict) else {}
    setups: list[Any] = data.get("setups") or data.get("qualified_setups") or []
    minimum = ACTION_RANK[min_state]
    selected = [
        setup
        for setup in setups
        if ACTION_RANK.get(str(setup.get("action_state")), -1) >= minimum
    ]
    selected.sort(key=lambda item: ACTION_RANK.get(str(item.get("action_state")), -1), reverse=True)
    return selected[: max(1, limit)]


def format_alert(setups: list[dict[str, Any]]) -> str:
    """Build one compact Telegram HTML alert."""
    parts = ["<b>Kairos 机会筛选</b> | <b>非指令</b> 仅供人工判断"]
    for setup in setups:
        raw_risk = setup.get("risk")
        risk = raw_risk if isinstance(raw_risk, dict) else {}
        raw_reasons = setup.get("reasons")
        reasons = raw_reasons if isinstance(raw_reasons, list) else []
        raw_warnings = setup.get("warnings")
        warnings = raw_warnings if isinstance(raw_warnings, list) else []
        direction = html.escape(_direction_zh(str(setup.get("direction") or "?")))
        symbol = html.escape(_display_symbol(str(setup.get("symbol") or "?")))
        state = html.escape(_state_zh(str(setup.get("action_state") or "?")))
        setup_type = html.escape(_setup_type_zh(str(setup.get("setup_type") or "?")))
        matched, missing = _strategy_points(reasons, warnings)
        parts.extend(
            [
                "",
                f"<b>[{state}] {symbol} {direction}</b> | {setup_type} | {setup.get('setup_score')}/{setup.get('threshold')}",
                f"<b>匹配</b>: {'、'.join(matched) if matched else '-'}",
                f"<b>缺口</b>: {'、'.join(missing) if missing else '-'}",
                f"<b>位</b>: 入 {_fmt_list(risk.get('entry_zone'), ' - ')} | 损 {risk.get('structural_stop')} | 目 {_fmt_list(risk.get('targets'), ' / ')}",
                f"<b>RR/上限</b>: {risk.get('risk_reward')} | {risk.get('max_position_pct')}% / {risk.get('max_leverage')}x",
            ]
        )
    return "\n".join(parts)


def _fmt_list(value: Any, sep: str = ", ") -> str:
    if not isinstance(value, list):
        return "-"
    return sep.join(str(item) for item in value[:3]) or "-"


def _display_symbol(symbol: str) -> str:
    return symbol.replace("/USDT:USDT", "").replace("/USDT", "")


def _strategy_points(reasons: list[Any], warnings: list[Any]) -> tuple[list[str], list[str]]:
    reason_text = "\n".join(map(str, reasons))
    warning_text = "\n".join(map(str, warnings))
    matched: list[str] = []
    missing: list[str] = []

    if "1d trend supports" in reason_text:
        matched.append("日线顺势")
    elif "1d trend conflicts" in warning_text:
        missing.append("日线逆势")

    if "4h " in reason_text and "structure is usable" in reason_text:
        matched.append("4H结构")
    elif "4h structure is not usable" in warning_text:
        missing.append("4H结构不足")

    if "BTC resonance supports direction" in reason_text or "BTC setup has no separate BTC resonance requirement" in reason_text:
        matched.append("BTC共振")
    elif "BTC resonance conflicts" in warning_text:
        missing.append("BTC不共振")
    elif "BTC resonance is neutral" in warning_text:
        missing.append("BTC中性")

    if "15m trigger is active" in reason_text:
        matched.append("15m触发")
    elif "15m price is near trigger" in reason_text:
        matched.append("接近触发")

    if "15m volume confirms move" in reason_text:
        matched.append("量能确认")
    elif "15m volume confirmation missing" in warning_text:
        missing.append("缺量能确认")

    if "risk/reward" in reason_text and "meets requirement" in reason_text:
        matched.append("盈亏比达标")
    elif "risk/reward" in warning_text and "below requirement" in warning_text:
        missing.append("盈亏比不足")

    if "cycle component=" in reason_text:
        matched.append("周期支持")
    elif "cycle does not support" in warning_text:
        missing.append("周期不支持")

    return matched[:5], missing[:4]


def _direction_zh(direction: str) -> str:
    return {"long": "做多", "short": "做空", "LONG": "做多", "SHORT": "做空"}.get(direction, direction)


def _state_zh(state: str) -> str:
    return {
        "no_trade": "不交易",
        "watch": "观察",
        "prepare": "准备",
        "trade_candidate": "交易候选",
    }.get(state, state)


def _setup_type_zh(setup_type: str) -> str:
    return {
        "box_breakout": "箱体突破",
        "box_breakdown": "箱体跌破",
        "range_breakout": "区间突破",
        "range_breakdown": "区间跌破",
    }.get(setup_type, setup_type)


async def run_once(
    *,
    exchange: str = "",
    min_state: str = "prepare",
    limit: int = 5,
    dry_run: bool = False,
    telegram: TelegramClient | None = None,
) -> int:
    """Scan once and optionally send candidate alerts to Telegram."""
    scan = scan_market(exchange=exchange or None)
    if not scan.get("success"):
        print("; ".join(map(str, scan.get("errors") or ["scan failed"])), file=sys.stderr)
        return 1

    setups = select_setups(scan, min_state=min_state, limit=limit)
    if not setups:
        print(f"没有达到 {min_state} 以上的候选。")
        return 0

    text = format_alert(setups)
    if dry_run:
        print(text)
        return 0
    telegram = telegram or TelegramClient()
    if not telegram.is_configured():
        print("需要设置 TELEGRAM_BOT_TOKEN 和 TELEGRAM_CHAT_ID。", file=sys.stderr)
        return 2

    await telegram.send_text(text)
    print(f"已发送 {len(setups)} 个候选。")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Send no-LLM Kairos scanner alerts to Telegram.")
    parser.add_argument("--exchange", default="", help="Exchange override, defaults to config primary exchange.")
    parser.add_argument("--min-state", choices=ACTION_RANK.keys(), default=os.getenv("KAIROS_ALERT_MIN_STATE", "prepare"))
    parser.add_argument("--limit", type=int, default=int(os.getenv("KAIROS_ALERT_LIMIT", "5")))
    parser.add_argument("--dry-run", action="store_true", help="Print the message instead of sending Telegram.")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    return asyncio.run(
        run_once(
            exchange=args.exchange,
            min_state=args.min_state,
            limit=args.limit,
            dry_run=args.dry_run,
        )
    )


if __name__ == "__main__":
    raise SystemExit(main())
