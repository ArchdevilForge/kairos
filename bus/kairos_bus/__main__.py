"""kairos-bus CLI — run / ingest / status。

用法:
    kairos-bus run              # 轮询循环(生产)
    kairos-bus run --once       # 单轮(调试 / cron)
    kairos-bus status           # 楼层与门控统计
"""

from __future__ import annotations

import argparse
import logging
import time

from .config import BusConfig
from .gate import Gate
from .ingest import Ingestor
from .notify import TelegramNotifier
from .sink import JsonlSink

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("kairos-bus")


def _process_once(cfg: BusConfig, gate: Gate, notifier: TelegramNotifier, sink: JsonlSink) -> tuple[int, int]:
    ingestor = Ingestor(cfg)
    events, malformed = ingestor.poll_once()
    for evt in events:
        allowed, reason = gate.decide(evt)
        sent = False
        if allowed:
            if notifier.enabled:
                sent = notifier.send(evt)
                if sent:
                    gate.commit_success(evt)
                else:
                    log.warning("telegram_send_failed event=%s", evt.dedup_key)
            else:
                # 未配置 Telegram:只记录,不提交 cooldown(等同发送失败)
                log.info("telegram_disabled event=%s", evt.dedup_key)
        sink.record(evt, allowed, reason, sent)
    if events or malformed:
        log.info("cycle_done events=%d malformed=%d gated=%d", len(events), malformed, sum(gate.reasons.values()))
    return len(events), malformed


def _run(args: argparse.Namespace) -> int:
    cfg = BusConfig.load(args.config)
    gate = Gate(cfg)
    notifier = TelegramNotifier.from_env(cfg.telegram_token_env, cfg.telegram_chat_env)
    sink = JsonlSink(cfg.out_dir)
    if not notifier.enabled:
        log.warning("telegram not configured (env %s/%s) — 只落聚合 JSONL", cfg.telegram_token_env, cfg.telegram_chat_env)
    while True:
        _process_once(cfg, gate, notifier, sink)
        if args.once:
            break
        time.sleep(cfg.poll_seconds)
    return 0


def _status(args: argparse.Namespace) -> int:
    cfg = BusConfig.load(args.config)
    print(f"bus: poll={cfg.poll_seconds}s dedup={cfg.dedup_window_seconds}s "
          f"cooldown={cfg.symbol_cooldown_seconds}s max_alerts/day={cfg.max_alerts_per_day}")
    for name, fc in cfg.floors.items():
        print(f"  floor {name}: {fc.source} allowed={fc.allowed_event_types} min={fc.min_severity} "
              f"blacklist={len(fc.blacklist)}")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="kairos-bus", description="Trading floor event bus")
    parser.add_argument("--config", default="config.toml", help="bus 配置文件路径")
    sub = parser.add_subparsers(dest="cmd", required=True)
    p_run = sub.add_parser("run", help="摄入 + 门控 + 推送 + 聚合落盘")
    p_run.add_argument("--once", action="store_true", help="只跑一轮")
    p_run.set_defaults(func=_run)
    p_status = sub.add_parser("status", help="显示配置")
    p_status.set_defaults(func=_status)
    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
