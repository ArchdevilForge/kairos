import type {
  CycleAlignment,
  CycleDirection,
  ExitReason,
  HumanDecision,
  MarketState,
  ReasonCode,
  SeasonRegime,
  SetupPoint,
  TradeClass,
  WavePhase,
} from '../types/kairos';

export const REASON_CODE_LABELS: Record<ReasonCode, string> = {
  structure_good: '结构清晰 (structure_good)',
  structure_bad: '结构凌乱 (structure_bad)',
  too_extended: '涨幅/跌幅过伸 (too_extended)',
  not_real_leader: '非真龙头/核心弱势 (not_real_leader)',
  insufficient_room: '盈亏比/空间不足 (insufficient_room)',
  counter_trend: '逆主周期方向 (counter_trend)',
  market_breadth_weak: '大盘广度不足 (market_breadth_weak)',
  funding_crowded: '资金费率/拥挤度过高 (funding_crowded)',
  late_to_event: '错过最佳触发 (late_to_event)',
  emotional_skip: '情绪犹豫跳过 (emotional_skip)',
  fear_of_loss: '惧损放弃 (fear_of_loss)',
  manual_override: '人工特批/覆盖 (manual_override)',
};

export const SETUP_POINT_META: Record<
  SetupPoint,
  { key: string; short: string; label: string; hint: string }
> = {
  POINT_1_BREAKOUT: {
    key: '1',
    short: '点位1',
    label: '点位1 · 平台/箱体突破启动',
    hint: 'Context/Setup 共振突破长期盘整箱体',
  },
  POINT_2_MINOR_DIV: {
    key: '2',
    short: '点位2',
    label: '点位2 · 主升途中回踩(小分歧)',
    hint: '主浪趋势中缩量浅回踩承接',
  },
  POINT_3_MID_TOP_SHORT: {
    key: '3',
    short: '点位3',
    label: '点位3 · 主升途中逆势摸顶空',
    hint: '逆势试错（C级封顶，禁止加仓）',
  },
  POINT_4_SECOND_WAVE: {
    key: '4',
    short: '点位4',
    label: '点位4 · 大分歧后二波启动',
    hint: '大分歧充分换手后的二波确认',
  },
  POINT_5_END_TOP_SHORT: {
    key: '5',
    short: '点位5',
    label: '点位5 · 主升结束摸顶空',
    hint: '结构破位/竭尽后的左侧转空确认',
  },
  POINT_6_BOX_BOUNCE: {
    key: '6',
    short: '点位6',
    label: '点位6 · 箱体下沿反弹',
    hint: '箱体边界明确止损位内的轮动反弹',
  },
};

export const EXIT_REASON_META: Record<
  ExitReason,
  { key: string; label: string; desc: string }
> = {
  PROTECTIVE_STOP: {
    key: '1 / P',
    label: '保护性止损 (PROTECTIVE_STOP)',
    desc: '触及预设结构止损位离场（Audit 豁免 +1.0）',
  },
  FAKEOUT_ABORT: {
    key: '2 / F',
    label: '假突破中止 (FAKEOUT_ABORT)',
    desc: '确认假突破立即纠错离场（Audit 豁免 +1.0）',
  },
  STRUCTURAL_TRAIL: {
    key: '3 / S',
    label: '结构移动止盈 (STRUCTURAL_TRAIL)',
    desc: '按高周期结构破位跟踪退出',
  },
  TARGET_PARTIAL: {
    key: '4 / T',
    label: '目标位分批止盈 (TARGET_PARTIAL)',
    desc: '到达既定阻力/目标区减仓落袋',
  },
  IMPULSE_EARLY: {
    key: '5 / I',
    label: '无结构早退 (IMPULSE_EARLY)',
    desc: '未破位也未到目标即受情绪扰动平仓',
  },
};

export const GATE_CODE_META: Record<
  string,
  { label: string; rule: string; nonOverridable?: boolean }
> = {
  GATE_CROSS_MARGIN: {
    label: '禁止全仓 (Cross Margin)',
    rule: '§9b-1 仅允许 Isolated 逐仓，防一笔连环爆仓',
  },
  GATE_SCALP_LT_5M: {
    label: '禁止 <5m 无结构剥头皮',
    rule: '§9b-2 超短持仓且无合规退出依据',
  },
  GATE_COOLDOWN: {
    label: '同币亏损冷却中',
    rule: '§9b-3 亏损后默认冷却 45m（需新 Setup / 15m-1h 结构确认 / 二波解锁）',
  },
  GATE_COOLDOWN_24H_LOCK: {
    label: '24h 刚性连损锁',
    rule: '§9b-3b 24h 内连损 >=2 笔或累计试错 <=-1.5R，不可覆盖',
    nonOverridable: true,
  },
  GATE_COUNTER_CYCLE: {
    label: '逆 CycleMap 主周期',
    rule: '§9b-4 严禁脱离 Context 方向凭感觉摸顶/抄底',
  },
  GATE_OUTSIDE_WINDOW: {
    label: '非 Live 交易时段',
    rule: '§9b-5 仅 18:00–01:00 (UTC+8) 允许实盘；其余时段仅 Observe/Shadow',
  },
  GATE_RISK_TOO_LARGE: {
    label: '单笔最大亏损超限',
    rule: '§9b-6 预设 MaxLossUSD 须 > 0 且 <= 配置上限（默认 $4）',
  },
  GATE_NO_STOP: {
    label: '缺失安全止损',
    rule: '§9b-7a 必须入场前设止损，且与强平价距离 >= 0.5%',
  },
  GATE_NO_STOP_BASIS: {
    label: '缺失结构失效依据',
    rule: '§9b-7b StopBasis 必须说明破坏哪处结构才止损',
  },
  GATE_LEVERAGE_EXCEEDED: {
    label: '超出硬杠杆上限',
    rule: '§9b-8 BTC/ETH <= 10x，山寨币 <= 5x',
  },
  GATE_NO_VALID_TICKET: {
    label: '无票擅自开单',
    rule: '§9b-9 实盘成交未匹配任何有效 accepted Ticket',
  },
  GATE_CHURN_FLIP_LT_5M: {
    label: '5m 内频繁多空反手',
    rule: '§9b-10 同币 <5m 多次多空翻倍刷单',
  },
  GATE_SETUP_TRIAL_BUDGET: {
    label: '同点位试错预算耗尽',
    rule: '§9b-11 24h 同 (币种, SetupPoint) 第 4 票硬拒',
  },
};

export const MARKET_STATE_META: Record<
  MarketState,
  { label: string; tone: 'bull' | 'bear' | 'warn' | 'info' | 'muted' }
> = {
  QUIET: { label: '平静观察 (QUIET)', tone: 'muted' },
  IMPULSE_UP: { label: '脉冲拉升 (IMPULSE_UP)', tone: 'bull' },
  IMPULSE_DOWN: { label: '脉冲下杀 (IMPULSE_DOWN)', tone: 'bear' },
  TRENDING_UP: { label: '多头趋势 (TRENDING_UP)', tone: 'bull' },
  TRENDING_DOWN: { label: '空头趋势 (TRENDING_DOWN)', tone: 'bear' },
  STRESS_UP: { label: '极端逼空 (STRESS_UP)', tone: 'warn' },
  STRESS_DOWN: { label: '极端恐慌 (STRESS_DOWN)', tone: 'warn' },
  DECAY: { label: '广度衰减 (DECAY)', tone: 'info' },
};

export const DIRECTION_LABEL: Record<CycleDirection, string> = {
  up: '多 (UP)',
  down: '空 (DOWN)',
  neutral: '中性 (NEUTRAL)',
};

export const PHASE_LABEL: Record<WavePhase, string> = {
  spring: '春 · 启',
  summer: '夏 · 盛',
  autumn: '秋 · 尾',
  winter: '冬 · 蛰',
};

export const SEASON_LABEL: Record<SeasonRegime, string> = {
  SPRING: 'SPRING 春',
  SUMMER: 'SUMMER 夏',
  AUTUMN: 'AUTUMN 秋',
  WINTER: 'WINTER 冬',
  '': '未测',
};

export const ALIGNMENT_LABEL: Record<CycleAlignment, string> = {
  full_alignment: '三周期共振',
  counter_trend: '逆主趋势',
  mixed: '周期分歧',
  no_trade: '禁止交易',
};

export const TRADE_CLASS_LABEL: Record<TradeClass, string> = {
  aligned_long: '顺势多',
  aligned_short: '顺势空',
  counter_trend_long: '逆势多 (C封顶)',
  counter_trend_short: '逆势空 (C封顶)',
  no_trade: '观望 / NO_TRADE',
};

export const DECISION_LABEL: Record<HumanDecision, string> = {
  accepted: '执行 (Accepted)',
  waiting: '等待 (Waiting)',
  rejected: '拒绝 (Rejected)',
  missed: '错过 (Missed)',
};

export const REVIEW_SETUP_TAG_PRESETS = [
  '平台突破',
  '主升回踩',
  '二波确认',
  '龙头领涨',
  '箱体边界',
  '顺大盘共振',
];

export const REVIEW_ERROR_TAG_PRESETS = [
  '逆势摸顶/抄底',
  '无票冲动开单',
  '止损不坚决/扛单',
  '情绪上头反手',
  '未等收盘提前抢跑',
  '过早恐慌止盈',
  '非黄金窗口操作',
];

export const REVIEW_GRADE_PRESETS = ['A+', 'A', 'B', 'C', 'D'];

export const REVIEW_EMOTION_PRESETS = ['冷静专注', 'FOMO追高', '恐惧犹豫', '上头急躁', '疲惫麻木'];

/** Normalize symbol strings across BTC/USDT:USDT and BTCUSDT for resilient comparisons. */
export function normalizeSymbolKey(sym: string): string {
  return sym
    .toUpperCase()
    .replace(':USDT', '')
    .replace(/[^A-Z0-9]/g, '');
}

export function sameSymbol(a: string | undefined, b: string | undefined): boolean {
  if (!a || !b) return false;
  return normalizeSymbolKey(a) === normalizeSymbolKey(b);
}
