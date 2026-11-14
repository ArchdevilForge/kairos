import type { DeskClient } from '../services/deskClient';

export const TIMEFRAMES = ['1m', '5m', '15m', '1h', '4h', '1d'] as const;
export type Timeframe = (typeof TIMEFRAMES)[number];

export type CycleDirection = 'up' | 'down' | 'neutral';
export type WavePhase = 'spring' | 'summer' | 'autumn' | 'winter';
export type SeasonRegime = 'SPRING' | 'SUMMER' | 'AUTUMN' | 'WINTER' | '';
export type TimeframeRole = 'context' | 'setup' | 'trigger';
export type CycleAlignment = 'full_alignment' | 'counter_trend' | 'mixed' | 'no_trade';
export type TradeClass =
  | 'aligned_long'
  | 'aligned_short'
  | 'counter_trend_long'
  | 'counter_trend_short'
  | 'no_trade';

export type MarketState =
  | 'QUIET'
  | 'IMPULSE_UP'
  | 'IMPULSE_DOWN'
  | 'TRENDING_UP'
  | 'TRENDING_DOWN'
  | 'STRESS_UP'
  | 'STRESS_DOWN'
  | 'DECAY';

export type TicketGrade = 'A' | 'B' | 'C' | 'D';
export type TicketStatus =
  | 'open'
  | 'accepted'
  | 'waiting'
  | 'rejected'
  | 'missed'
  | 'closed'
  | 'expired';
export type HumanDecision = 'accepted' | 'waiting' | 'rejected' | 'missed';
export type Severity = 'LOW' | 'MEDIUM' | 'HIGH';
export type MarginMode = 'isolated' | 'cross' | string;
export type DrawingKind = 'hline' | 'ruler' | 'trend' | 'ray' | 'region' | 'fib';
export type ReviewSubjectType = 'ticket' | 'trade' | 'position';

export const REASON_CODES = [
  'structure_good',
  'structure_bad',
  'too_extended',
  'not_real_leader',
  'insufficient_room',
  'counter_trend',
  'market_breadth_weak',
  'funding_crowded',
  'late_to_event',
  'emotional_skip',
  'fear_of_loss',
  'manual_override',
] as const;
export type ReasonCode = (typeof REASON_CODES)[number];

export const SETUP_POINTS = [
  'POINT_1_BREAKOUT',
  'POINT_2_MINOR_DIV',
  'POINT_3_MID_TOP_SHORT',
  'POINT_4_SECOND_WAVE',
  'POINT_5_END_TOP_SHORT',
  'POINT_6_BOX_BOUNCE',
] as const;
export type SetupPoint = (typeof SETUP_POINTS)[number];

export const EXIT_REASONS = [
  'PROTECTIVE_STOP',
  'FAKEOUT_ABORT',
  'STRUCTURAL_TRAIL',
  'TARGET_PARTIAL',
  'IMPULSE_EARLY',
] as const;
export type ExitReason = (typeof EXIT_REASONS)[number];

export const GATE_CODES = [
  'GATE_CROSS_MARGIN',
  'GATE_SCALP_LT_5M',
  'GATE_COOLDOWN',
  'GATE_COOLDOWN_24H_LOCK',
  'GATE_COUNTER_CYCLE',
  'GATE_OUTSIDE_WINDOW',
  'GATE_RISK_TOO_LARGE',
  'GATE_NO_STOP',
  'GATE_NO_STOP_BASIS',
  'GATE_LEVERAGE_EXCEEDED',
  'GATE_NO_VALID_TICKET',
  'GATE_CHURN_FLIP_LT_5M',
  'GATE_SETUP_TRIAL_BUDGET',
] as const;
export type GateCode = (typeof GATE_CODES)[number];

export interface Bar {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  quote_vol: number;
}

export interface Evidence {
  code: string;
  description: string;
  value?: number;
}

export interface CycleNode {
  schema_version: string;
  timeframe: string;
  role: TimeframeRole;
  direction: CycleDirection;
  phase: WavePhase;
  trend_strength: number;
  structure_quality: number;
  momentum_change: number;
  volatility: number;
  volume_quality: number;
  room_up_pct: number;
  room_down_pct: number;
  confidence: number;
  evidence: Evidence[];
}

export interface CycleMap {
  schema_version: string;
  as_of_unix: number;
  legacy_climate: string;
  nodes: Record<string, CycleNode>;
  primary_direction: CycleDirection;
  alignment: CycleAlignment;
  trade_class: TradeClass;
  summary: string[];
}

export interface BoxPattern {
  symbol: string;
  timeframe: string;
  high: number;
  low: number;
  start_time: number;
  end_time: number;
  status: 'forming' | 'converging' | 'breakout_up' | 'breakout_down' | 'invalid';
  touch_high: number;
  touch_low: number;
  second_test_high: boolean;
  second_test_low: boolean;
  convergence_pct: number;
  volume_declining: boolean;
  breakout_price?: number | null;
  breakout_time?: number | null;
}

export interface MetricsPoint {
  time: number;
  open_interest?: number;
  oi_change_pct?: number;
  funding_rate?: number;
  long_short_ratio?: number;
  liq_total_usd?: number;
  liq_long_usd?: number;
  liq_short_usd?: number;
}

export interface MarketBarsResponse {
  symbol: string;
  tf: Timeframe;
  bars: Bar[];
  market_cycle?: CycleMap | null;
  symbol_cycle?: CycleMap | null;
  box?: BoxPattern | null;
  metrics: MetricsPoint[];
}

export interface AnomalyItem {
  symbol: string;
  event_type: string;
  severity: Severity;
  data: Record<string, unknown>;
  timestamp: number;
  exchange?: string;
  event_id?: string;
}

export type NoticeSceneFlag =
  | 'SCENE_POINT_1_BREAKOUT'
  | 'SCENE_POINT_2_MINOR_DIV'
  | 'SCENE_POINT_4_SECOND_WAVE'
  | 'SCENE_RESONANCE_LEADER';

export type NoticeBoxState = 'unknown' | 'inside_box' | 'boundary' | 'outside_box';
export type NoticeDeliveryStatus = 'sent' | 'suppressed' | 'dry_run' | 'failed' | 'coalescing';

export interface NoticeTriggerFacts {
  event_type: string;
  event_id?: string;
  exchange?: string;
  price?: number;
  volume_ratio?: number;
  volume_window_min?: number;
  price_change_pct?: number;
  velocity_window_sec?: number;
  oi_change_pct?: number;
  open_interest?: number;
  dimensions?: string[];
  dimension_count?: number;
  fact_summary: string;
}

export interface NoticeSceneDetail {
  flag: NoticeSceneFlag | string;
  summary: string;
}

export interface NoticeEnvTags {
  season_regime?: SeasonRegime;
  winter_defense_hint?: string;
  box_state: NoticeBoxState;
  box_width_pct?: number;
  box_pos?: number;
  btc_state?: string;
}

export interface Notice {
  schema_version: string;
  notice_id: string;
  ts: string;
  symbol: string;
  exchange?: string;
  trigger_facts: NoticeTriggerFacts;
  scenes?: NoticeSceneDetail[];
  env: NoticeEnvTags;
  delivery_status: NoticeDeliveryStatus;
  suppress_reason?: string;
  suppress_detail?: string;
  delivery_error?: string;
  disclaimer: string;
  rendered_telegram?: string;
}

export interface EntryPlan {
  mode: string;
  zone_low: number;
  zone_high: number;
  trigger: string;
  notes?: string[];
}

export interface RiskPlan {
  template_id: string;
  risk_budget_pct: number;
  entry_price: number;
  stop_price: number;
  stop_distance_pct: number;
  effective_stop_pct?: number;
  suggested_notional: number;
  max_notional: number;
  max_leverage: number;
  tranche_base_usd?: number;
  add_on_allowed: boolean;
}

export interface DecisionTicket {
  schema_version: string;
  id: string;
  session_id: string;
  wave_cluster_id?: string;
  symbol: string;
  playbook_id: string;
  setup_point?: SetupPoint | '';
  direction: CycleDirection;
  grade: TicketGrade;
  trade_class: TradeClass;
  market_cycle: CycleMap;
  symbol_cycle: CycleMap;
  entry_plan: EntryPlan;
  risk_plan: RiskPlan;
  stop_basis?: string;
  reasons: string[];
  warnings: string[];
  invalidations: string[];
  status: TicketStatus;
  season_regime?: SeasonRegime;
  winter_defense_hint?: string;
  exit_reason?: ExitReason | '';
  realized_funding_usd?: number;
  exit_price?: number;
  closed_at?: number;
  created_at: number;
  signal_at: number;
  entry_triggered_at?: number;
}

export interface DecisionRecord {
  schema_version: string;
  ticket_id: string;
  decision: HumanDecision;
  reason_codes: string[];
  note: string;
  planned_entry?: number | null;
  planned_stop?: number | null;
  planned_risk_r?: number | null;
  planned_max_loss_usd?: number | null;
  exit_reason?: ExitReason | '';
  realized_funding_usd?: number;
  exit_price?: number;
  closed_at?: number;
  touched_preset_stop?: boolean;
  fakeout_confirmed?: boolean;
  market_stress_active?: boolean;
  structure_intact?: boolean;
  decided_at: number;
}

export interface GateAnnotation {
  schema_version: string;
  ticket_id: string;
  codes: string[];
  override: boolean;
  note?: string;
  at: number;
}

export interface CounterfactualOutcome {
  schema_version: string;
  ticket_id: string;
  session_id: string;
  wave_cluster_id?: string;
  symbol: string;
  direction: CycleDirection;
  decision?: HumanDecision | '';
  season_regime?: SeasonRegime;
  winter_defense_hint?: string;
  return_5m?: number | null;
  return_15m?: number | null;
  return_1h?: number | null;
  return_4h?: number | null;
  mfe: number;
  mae: number;
  stop_hit_first: boolean;
  target_hit_first: boolean;
  max_realizable_r: number;
  mechanical_r: number;
  net_r: number;
  exit_reason?: ExitReason | '';
  realized_funding_usd?: number;
  realized_fee_usd?: number;
  realized_slippage_usd?: number;
  initial_1r_pct?: number;
  exit_price?: number;
  human_entry_price?: number;
  mech_human_entry_r?: number | null;
  policy_wave_r?: number | null;
  policy_5m_r?: number | null;
  policy_30m_r?: number | null;
  policy_bracket_r?: number | null;
  touched_preset_stop?: boolean;
  fakeout_confirmed?: boolean;
  market_stress_active?: boolean;
  structure_intact?: boolean;
  complete_5m: boolean;
  complete_15m: boolean;
  complete_1h: boolean;
  complete_4h: boolean;
  finalized: boolean;
  complete: boolean;
  path_start_unix?: number;
  path_end_unix?: number;
  as_of_unix: number;
}

export interface ReviewAnnotation {
  subject_type: ReviewSubjectType;
  subject_id: string;
  note: string;
  setup_tags: string[];
  error_tags: string[];
  grade: string;
  emotion: string;
  planned_stop?: number | null;
  planned_target?: number | null;
  linked_ticket_id?: string;
  updated_at: number;
}

export interface TradeFillItem {
  fill_id: string;
  order_id: string;
  symbol: string;
  side: 'BUY' | 'SELL' | string;
  position_side: 'BOTH' | 'LONG' | 'SHORT' | string;
  price: number;
  qty: number;
  quote_qty: number;
  commission: number;
  commission_asset: string;
  realized_pnl: number;
  time_ms: number;
  trade_id: string;
}

export interface SyncedTrade {
  id: string;
  symbol: string;
  direction: 'long' | 'short' | CycleDirection;
  position_side: string;
  margin_mode: MarginMode;
  leverage: number;
  entry_price: number;
  exit_price: number;
  qty: number;
  entry_time_ms: number;
  exit_time_ms: number;
  realized_pnl: number;
  fee_usd: number;
  funding_usd: number;
  net_pnl: number;
  margin: number;
  liq_price: number;
  status: 'open' | 'closed' | string;
  linked_ticket_id?: string;
  gate_code?: string;
  review?: ReviewAnnotation | null;
  fills?: TradeFillItem[];
}

export interface PositionSnapshot {
  symbol: string;
  position_side: string;
  margin_mode: MarginMode;
  leverage: number;
  qty: number;
  entry_price: number;
  mark_price: number;
  unrealized_pnl: number;
  liq_price: number;
  isolated_wallet: number;
  updated_at: number;
  linked_ticket_id?: string;
  review?: ReviewAnnotation | null;
}

export interface TicketBundle {
  ticket: DecisionTicket;
  decision?: DecisionRecord | null;
  outcome?: CounterfactualOutcome | null;
  annotations: GateAnnotation[];
  review?: ReviewAnnotation | null;
  fills?: TradeFillItem[];
  linked_trade?: SyncedTrade | null;
}

export interface SymbolMove {
  symbol: string;
  return_pct: number;
  relative_pct: number;
  z_score: number;
}

export interface MarketSnapshot {
  timestamp: number;
  universe_size: number;
  eligible_symbols: number;
  valid_symbols: number;
  valid_symbols_300s: number;
  valid_z_symbols: number;
  z_usable: boolean;
  fresh_ratio: number;
  median_return_60s: number;
  median_return_180s: number;
  median_return_300s: number;
  median_z_60s: number;
  up_breadth_60s: number;
  down_breadth_60s: number;
  advancers: number;
  decliners: number;
  neutral: number;
  btc_return_60s?: number | null;
  eth_return_60s?: number | null;
  leaders: SymbolMove[];
  laggards: SymbolMove[];
  data_ok: boolean;
  gate_reason?: string;
}

export interface PulseStatus {
  state: MarketState;
  direction?: CycleDirection | string;
  updated_at?: number;
  data_ok?: boolean;
  median_return_60s?: number;
  median_return_300s?: number;
  median_z_60s?: number;
  up_breadth_60s?: number;
  down_breadth_60s?: number;
  snapshot?: MarketSnapshot | null;
}

export interface CooldownEntry {
  symbol: string;
  expires_at: number;
  remaining_sec: number;
}

export interface DeskStatus {
  exchange: string;
  symbols: string[];
  available_symbols?: string[];
  timezone: string;
  in_live_window: boolean;
  window_start_hour: number;
  window_end_hour: number;
  pulse: PulseStatus;
  season_regime: SeasonRegime;
  winter_defense_hint?: string;
  market_cycle?: CycleMap | null;
  gate_health: {
    locked_24h_symbols: string[];
    active_cooldowns: CooldownEntry[];
    consecutive_stops_24h: number;
  };
  risk_config: {
    trading_bankroll_usd: number;
    max_loss_usd: number;
    max_major_leverage: number;
    max_alt_leverage: number;
    cooldown_minutes: number;
  };
  account: {
    configured: boolean;
    api_key_masked: string;
    wallet_balance_usd: number;
    unrealized_pnl_usd: number;
    last_sync_at: number;
    sync_error?: string;
  };
}

export interface BootstrapPayload {
  status: DeskStatus;
  tickets: TicketBundle[];
  unplanned_trades: SyncedTrade[];
  open_positions: PositionSnapshot[];
  notices: Notice[];
  anomalies: AnomalyItem[];
}

export interface GatePreviewRequest {
  symbol: string;
  direction: CycleDirection;
  margin_mode: MarginMode;
  entry_price: number;
  stop_price: number;
  target_price?: number;
  leverage: number;
  max_loss_usd?: number;
  stop_basis: string;
  setup_point?: SetupPoint | '';
  ticket_id?: string;
  new_setup_id?: string;
  new_structural_confirm_15m_1h?: boolean;
  playbook_switch?: string;
}

export interface GatePreviewResponse {
  live_eligible: boolean;
  non_overridable: boolean;
  rejections: string[];
  warnings: string[];
  one_r_distance: number;
  one_r_pct: number;
  max_loss_usd: number;
  suggested_notional: number;
  max_notional: number;
  max_leverage: number;
  risk_budget_pct: number;
  template_id: string;
  liq_price_est?: number;
  market_cycle?: CycleMap | null;
  symbol_cycle?: CycleMap | null;
}

export interface DraftTicketRequest {
  symbol: string;
  direction: CycleDirection;
  margin_mode: MarginMode;
  entry_price: number;
  stop_price: number;
  target_price?: number;
  leverage: number;
  max_loss_usd?: number;
  stop_basis: string;
  setup_point?: SetupPoint | '';
  playbook_id?: string;
  entry_mode?: string;
  zone_low?: number;
  zone_high?: number;
  trigger?: string;
  invalidations?: string[];
  reasons?: string[];
  reason_codes?: string[];
  note?: string;
  decision?: HumanDecision;
  new_setup_id?: string;
  new_structural_confirm_15m_1h?: boolean;
  playbook_switch?: string;
}

export interface DraftTicketResponse {
  bundle: TicketBundle;
  gate: GatePreviewResponse;
}

export interface DecideTicketRequest {
  decision: HumanDecision;
  reason_codes: string[];
  note: string;
  margin_mode?: MarginMode;
  leverage?: number;
  stop_basis?: string;
  setup_point?: SetupPoint | '';
  planned_entry?: number;
  planned_stop?: number;
  planned_risk_r?: number;
  planned_max_loss_usd?: number;
  override?: boolean;
  new_setup_id?: string;
  new_structural_confirm_15m_1h?: boolean;
  playbook_switch?: string;
}

export interface DecideTicketResponse {
  bundle: TicketBundle;
  gate: GatePreviewResponse;
}

export interface CloseTicketRequest {
  exit_reason: ExitReason;
  exit_price: number;
  closed_at?: number;
  realized_funding_usd?: number;
  touched_preset_stop?: boolean;
  fakeout_confirmed?: boolean;
  market_stress_active?: boolean;
  structure_intact?: boolean;
}

export interface ExitAuditResult {
  verdict: string;
  code?: string;
  is_violation: boolean;
  score_delta: number;
  suggestion?: string;
}

export interface CloseTicketResponse {
  bundle: TicketBundle;
  audit_code: string;
  audit?: ExitAuditResult;
}

export interface ChartDrawingPoint {
  time: number;
  price: number;
}

export interface ChartDrawingItem {
  id: number;
  symbol: string;
  kind: DrawingKind;
  points: ChartDrawingPoint[];
  label?: string;
  created_at: number;
  updated_at: number;
}

export interface CreateDrawingRequest {
  symbol: string;
  kind: DrawingKind;
  points: ChartDrawingPoint[];
  label?: string;
}

export interface UpdateDrawingRequest {
  points: ChartDrawingPoint[];
  label?: string;
}

export interface ReplayStateResponse {
  symbol: string;
  cursor_ts: number;
  bars_by_tf: Partial<Record<Timeframe, Bar[]>>;
  drawings: ChartDrawingItem[];
  anomalies: AnomalyItem[];
  notices: Notice[];
  tickets: TicketBundle[];
  fills: TradeFillItem[];
  market_cycle?: CycleMap | null;
  symbol_cycle?: CycleMap | null;
  box?: BoxPattern | null;
}

export interface UpsertReviewRequest {
  note: string;
  setup_tags: string[];
  error_tags: string[];
  grade: string;
  emotion: string;
  planned_stop?: number | null;
  planned_target?: number | null;
  linked_ticket_id?: string;
}

export interface UpdateSettingsRequest {
  trading_bankroll_usd?: number;
  max_loss_usd?: number;
  timezone?: string;
  binance_readonly_api_key?: string;
  binance_readonly_api_secret?: string;
}

export interface AccountSyncResponse {
  ok: boolean;
  synced_trades: number;
  synced_fills: number;
  open_positions: number;
  account: DeskStatus['account'];
}

export interface GroupStats {
  n: number;
  n_complete: number;
  mean_net_r: number;
  mean_mechanical_r: number;
  mean_max_r: number;
  mean_5m?: number;
  win_rate: number;
}

export interface SummaryReport {
  tickets_total: number;
  with_outcome: number;
  complete_outcomes: number;
  accepted: GroupStats;
  rejected: GroupStats;
  waiting: GroupStats;
  missed: GroupStats;
  all_qualified: GroupStats;
  selection_alpha: number;
  rejection_mean_net_r: number;
  human_alpha: number;
  mean_pool_mech_r: number;
  mean_selection_alpha: number;
  mean_timing_alpha: number;
  mean_management_alpha: number;
  max_abs_identity_residual: number;
  by_trade_class?: Record<string, GroupStats>;
  by_grade?: Record<string, GroupStats>;
  by_playbook?: Record<string, GroupStats>;
  by_direction?: Record<string, GroupStats>;
}

export interface EquityPoint {
  time: number;
  bankroll_usd: number;
  wallet_usd?: number | null;
  trade_pnl_usd: number;
  cumulative_r: number;
  drawdown_pct: number;
}

export interface WalletReconciliation {
  trading_bankroll_usd: number;
  realized_pnl_usd: number;
  fee_usd: number;
  funding_usd: number;
  net_pnl_usd: number;
  expected_bankroll_usd: number;
  wallet_balance_usd: number;
  unrealized_pnl_usd: number;
  discrepancy_usd: number;
  last_sync_at: number;
}

export interface RBucket {
  label: string;
  min_r: number;
  max_r: number;
  count: number;
  net_pnl_usd: number;
}

export interface RDistribution {
  buckets: RBucket[];
  total_trades: number;
  without_stop: number;
  mean_r: number;
  median_r: number;
  win_rate: number;
  profit_factor: number;
}

export interface GateComplianceReport {
  total_annotations: number;
  overrides: number;
  rejections_by_code: Record<string, number>;
  protective_stop_exemptions: number;
  early_profit_notes: number;
  scalp_violations: number;
  unplanned_trade_count: number;
  compliance_score: number;
}

export interface HourlyBucket {
  hour: number;
  label: string;
  count: number;
  wins: number;
  win_rate: number;
  net_pnl_usd: number;
  mean_r: number;
  in_live_window: boolean;
}

export interface TagMetricRow {
  tag: string;
  count: number;
  wins: number;
  win_rate: number;
  net_pnl_usd: number;
  mean_r: number;
  sample_ticket_id?: string;
  sample_trade_id?: string;
  sample_symbol?: string;
  sample_ts?: number;
}

export interface StatsPayload {
  equity_curve: EquityPoint[];
  wallet_reconciliation: WalletReconciliation;
  summary: SummaryReport;
  r_distribution: RDistribution;
  gate_compliance: GateComplianceReport;
  hourly_utc8: HourlyBucket[];
  tag_breakdown: {
    setup_tags: TagMetricRow[];
    error_tags: TagMetricRow[];
    grades: TagMetricRow[];
    emotions: TagMetricRow[];
  };
}

export type WsEvent =
  | {
      type: 'ticker';
      symbol: string;
      ts: number;
      price: number;
      change_24h_pct?: number;
      quote_volume_24h?: number;
      open_interest?: number;
      funding_rate?: number;
    }
  | {
      type: 'kline';
      symbol: string;
      tf: Timeframe;
      bar: Bar;
      closed: boolean;
    }
  | {
      type: 'pulse';
      pulse: PulseStatus;
      season_regime?: SeasonRegime;
    }
  | {
      type: 'anomaly';
      anomaly: AnomalyItem;
    }
  | {
      type: 'notice';
      notice: Notice;
    }
  | {
      type: 'ticket_updated';
      bundle: TicketBundle;
    }
  | {
      type: 'account_updated';
      account: DeskStatus['account'];
      open_positions?: PositionSnapshot[];
    }
  | {
      type: 'subscribed';
      symbol: string;
      tfs: Timeframe[];
    }
  | {
      type: 'error';
      message: string;
    };

export interface WatchWorkspaceProps {
  client: DeskClient;
  bootstrap: BootstrapPayload;
  activeSymbol: string;
  onSelectSymbol: (sym: string) => void;
  splitMode: boolean;
  onToggleSplit: () => void;
  activeTf: Timeframe;
  onSelectTf: (tf: Timeframe) => void;
}

export interface ReviewWorkspaceProps {
  client: DeskClient;
  bootstrap: BootstrapPayload;
  initialSymbol: string;
  initialCursorTs?: number;
  initialTicketId?: string;
  onSelectSymbol: (sym: string) => void;
  onRefreshBootstrap: () => Promise<void>;
}

export interface StatsWorkspaceProps {
  client: DeskClient;
  bootstrap: BootstrapPayload;
  onDrillDownReview: (symbol: string, cursorTs?: number, ticketId?: string) => void;
}

export interface KairosDesktopBridge {
  getConfig: () => Promise<{ url: string; wsUrl: string; token: string }>;
  syncNow: () => Promise<void>;
  notify: (payload: { id?: string; title: string; body: string; symbol?: string }) => void;
  onTriggerSync?: (cb: () => void) => () => void;
  onFocusSymbol?: (cb: (symbol: string) => void) => () => void;
}

declare global {
  interface Window {
    kairosDesktop?: KairosDesktopBridge;
  }
}
