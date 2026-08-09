from __future__ import annotations

import structlog

from pm_bot.core.clob import ClobTrader


def run_settle(
    all_positions: bool = False,
    condition_ids_str: str | None = None,
    list_only: bool = False,
    debug: bool = False,
) -> None:
    log = structlog.get_logger()

    trader = ClobTrader()
    if not trader.is_configured():
        print("Error: CLOB credentials not configured. Run `pm-bot config --init`.")
        return

    if list_only:
        positions = trader.get_redeemable_positions()
        if not positions:
            print("No redeemable positions found.")
            return
        print(f"Redeemable positions ({len(positions)}):")
        total_value = 0.0
        for p in positions:
            cid = p.get("conditionId", "?")
            size = float(p.get("size", 0))
            outcome = p.get("outcome", "?")
            title = p.get("title", p.get("market", "?"))
            print(f"  {outcome}: {size:.2f} tokens — {title[:60]}")
            print(f"    conditionId: {cid}")
            total_value += size
        print(f"\nTotal redeemable value: ${total_value:.2f}")
        return

    condition_ids = None
    if condition_ids_str:
        condition_ids = [cid.strip() for cid in condition_ids_str.split(",") if cid.strip()]

    if not all_positions and not condition_ids:
        positions = trader.get_redeemable_positions()
        if not positions:
            print("No redeemable positions found. Use --all to check all positions.")
            return
        condition_ids = list({str(p["conditionId"]) for p in positions if p.get("conditionId")})
        if not condition_ids:
            print("No condition IDs found in redeemable positions.")
            return

    result = trader.settle_resolved(condition_ids=condition_ids)

    redeemed = result.get("redeemed", 0)
    errors = result.get("errors", [])
    if redeemed > 0:
        print(f"Successfully redeemed {redeemed} position(s).")
        log.info("settle_complete", redeemed=redeemed)
    else:
        print("No positions redeemed.")
    if errors:
        print(f"Errors: {errors}")
