"""告警门控 — 纪律 1:1 移植自 kairos internal/engine/pipeline.go。

规则(与 kairos 一致):
1. 每楼层事件类型 allow-list
2. 每楼层 min severity 门控
3. 每楼层 blacklist(symbol)
4. dedup:(floor, event_type, key) 窗口内丢弃;attempt 级,立即提交 ——
   即使推送失败也限流重试,防止故障通道被锤爆
5. symbol cooldown:发送成功后才提交 —— notifier 短暂故障不烧掉长冷却
6. skip_cooldown_event_types(市场级事件)不占 symbol cooldown:
   市场级有自己的 direction 感知去重,单边冲动不应压掉几分钟后的反向信号
7. never_notify_event_types(calibration/outcome)永不推送,但永远进聚合 JSONL
8. bus 级每日 attention budget

聚合 JSONL 记录所有事件(含被 gate 拒绝的),Telegram 只发通过的 ——
calibration 需要完整事件流,推送只需要高信号。
"""

from __future__ import annotations

import time
from collections import Counter

from .config import BusConfig
from .model import Event


class Gate:
    def __init__(self, cfg: BusConfig) -> None:
        self.cfg = cfg
        self.dedup_last: dict[str, float] = {}
        self.cooldown_last: dict[str, float] = {}
        self.sent_today: list[float] = []
        self.reasons: Counter = Counter()

    def decide(self, evt: Event, now: float | None = None) -> tuple[bool, str | None]:
        """返回 (是否推送, 拒绝原因)。不修改推送状态之外的状态;dedup 立即提交。"""
        now = now if now is not None else time.time()
        floor_cfg = self.cfg.floor(evt.floor)
        if floor_cfg is None:
            self.reasons["unknown_floor"] += 1
            return False, "unknown_floor"
        if evt.event_type in floor_cfg.never_notify_event_types:
            self.reasons["never_notify"] += 1
            return False, "never_notify"
        if evt.event_type not in floor_cfg.allowed_event_types:
            self.reasons["event_type"] += 1
            return False, "event_type"
        if evt.symbol and evt.symbol in floor_cfg.blacklist:
            self.reasons["blacklist"] += 1
            return False, "blacklist"
        if evt.severity_rank < floor_cfg.min_severity_rank:
            self.reasons["severity"] += 1
            return False, "severity"
        if len(self.sent_today) >= self.cfg.max_alerts_per_day:
            self.reasons["attention_budget"] += 1
            return False, "attention_budget"

        if now - self.dedup_last.get(evt.dedup_key, -1e9) < self.cfg.dedup_window_seconds:
            self.reasons["dedup"] += 1
            return False, "dedup"
        if (
            evt.symbol
            and evt.event_type not in floor_cfg.skip_cooldown_event_types
            and now - self.cooldown_last.get(evt.symbol, -1e9) < self.cfg.symbol_cooldown_seconds
        ):
            self.reasons["cooldown"] += 1
            return False, "cooldown"

        # dedup 立即提交(attempt 级)
        self.dedup_last[evt.dedup_key] = now
        return True, None

    def commit_success(self, evt: Event, now: float | None = None) -> None:
        """推送成功后提交 cooldown + attention budget(与 kairos 一致)。"""
        now = now if now is not None else time.time()
        floor_cfg = self.cfg.floor(evt.floor)
        if evt.symbol and floor_cfg is not None and evt.event_type not in floor_cfg.skip_cooldown_event_types:
            self.cooldown_last[evt.symbol] = now
        self.sent_today.append(now)
        # 只保留当日窗口,避免无限增长
        cutoff = now - 86400
        self.sent_today = [t for t in self.sent_today if t >= cutoff]
