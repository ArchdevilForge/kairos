export const chartZh = {
  // KairosChart - OHLC legend & loading
  'chart.noBars': '暂无 K 线数据',
  'chart.ohlcLegend':
    '开 {open}  高 {high}  低 {low}  收 {close}  {sign}{chg}%  振幅 {amp}%',
  'chart.loading': '加载中…',
  'chart.loadingMore': '加载历史…',

  // KairosChart - markers & tooltips
  'chart.noteAnomaly': '[异动 {severity}] {eventType} @ {time}',
  'chart.noteNotice': '[通知] {summary} · {time}',
  'chart.noteTicketSignal': '[Ticket 信号] {time}',

  // KairosChart - drawing hints
  'chart.hintHline': '点击放置水平线 · Esc 取消',
  'chart.hintRuler': '尺子：拖拽或点两点测距 · Esc 取消',
  'chart.hintTrend': '趋势线：拖拽或点两点 · Shift 锁角度',
  'chart.hintRay': '射线：拖拽或点两点向右延伸',
  'chart.hintRegion': '区间框：拖拽或点对角两点',
  'chart.hintFib': '斐波那契：拖拽或点高低两点',
  'chart.hintRulerStep2': '拖拽或移动再点完成',
  'chart.hintShapeStep2': '移动预览，再点或松开完成',
  'chart.rulerBars': '{count} 根',

  // KairosChart - replay & toolbar controls
  'chart.hideFuture': '藏未来',
  'chart.hideFutureOn': '藏未来 开',
  'chart.hideFutureTitle': 'Hide Future · 零前视隐藏未来',
  'chart.showFullBarsTitle': '显示完整 K 线',
  'chart.stepBackTitle': '后退步进 (←)',
  'chart.playTitle': '播放回放 (Space)',
  'chart.pauseTitle': '暂停回放 (Space)',
  'chart.stepForwardTitle': '前进步进 (→)',
  'chart.stepSizeAria': '单步步进',
  'chart.stepBarsOption': '{count}根',
  'chart.playSpeedAria': '回放倍速',
  'chart.smcTitle': '开关 SMC 市场结构 (BOS/CHoCH · 未回补 Order Block · EQH/EQL)',
  'chart.rulerTitle': '尺子测距（自动保存，点垃圾桶清空）',
  'chart.hlineTitle': '水平线（可上下拖动，点垃圾桶清空）',
  'chart.moreToolsTitle': '更多画线工具（趋势线 / 射线 / 区间 / 斐波那契）',
  'chart.toolTrend': '趋势线',
  'chart.toolRay': '射线',
  'chart.toolRegion': '区间框',
  'chart.toolFib': '斐波那契',
  'chart.magnetTitle': 'OHLC 价格磁吸（画线时按住 Ctrl/⌘ 可临时反转）',
  'chart.clearDrawingsTitle': '清空全部 {count} 条画线与尺子',
  'chart.closePaneTitle': '关闭对比副图',

  // EquityCurve
  'equity.title': '资金净值曲线',
  'equity.emptyHint': '暂无足够已结算样本以绘制权益与钱包对账曲线。',
  'equity.legendBankroll': '账面资金',
  'equity.legendWallet': '只读钱包',
  'equity.modeEquity': '资金曲线',
  'equity.modeDrawdown': '回撤 %',
  'equity.seriesBankroll': '账面',
  'equity.seriesWallet': '钱包',
  'equity.seriesDrawdown': '回撤%',
  'equity.readoutBankroll': '账面 {value}',
  'equity.readoutWallet': '钱包 {value}',
  'equity.readoutTrade': '单笔 {value}',
  'equity.readoutCumR': '累计 {value}',
  'equity.readoutDrawdown': '回撤 {value}',
} as const;

export const chartEn: Record<keyof typeof chartZh, string> = {
  // KairosChart - OHLC legend & loading
  'chart.noBars': 'No candlestick data',
  'chart.ohlcLegend':
    'O {open}  H {high}  L {low}  C {close}  {sign}{chg}%  Range {amp}%',
  'chart.loading': 'Loading…',
  'chart.loadingMore': 'Loading history…',

  // KairosChart - markers & tooltips
  'chart.noteAnomaly': '[Anomaly {severity}] {eventType} @ {time}',
  'chart.noteNotice': '[Notice] {summary} · {time}',
  'chart.noteTicketSignal': '[Ticket Signal] {time}',

  // KairosChart - drawing hints
  'chart.hintHline': 'Click to place horizontal line · Esc to cancel',
  'chart.hintRuler': 'Ruler: drag or click 2 points to measure · Esc to cancel',
  'chart.hintTrend': 'Trend line: drag or click 2 points · Shift to lock angle',
  'chart.hintRay': 'Ray: drag or click 2 points extending right',
  'chart.hintRegion': 'Region box: drag or click 2 opposite corners',
  'chart.hintFib': 'Fibonacci: drag or click high & low points',
  'chart.hintRulerStep2': 'Drag or click a second point to finish',
  'chart.hintShapeStep2': 'Move to preview, click or release to finish',
  'chart.rulerBars': '{count} bars',

  // KairosChart - replay & toolbar controls
  'chart.hideFuture': 'Hide Future',
  'chart.hideFutureOn': 'Hide Future On',
  'chart.hideFutureTitle': 'Hide Future · Zero-lookahead mode',
  'chart.showFullBarsTitle': 'Show full candlesticks',
  'chart.stepBackTitle': 'Step back (←)',
  'chart.playTitle': 'Play replay (Space)',
  'chart.pauseTitle': 'Pause replay (Space)',
  'chart.stepForwardTitle': 'Step forward (→)',
  'chart.stepSizeAria': 'Step size',
  'chart.stepBarsOption': '{count} bars',
  'chart.playSpeedAria': 'Playback speed',
  'chart.smcTitle': 'Toggle SMC market structure (BOS/CHoCH · Unmitigated Order Block · EQH/EQL)',
  'chart.rulerTitle': 'Ruler tool (auto-saved, click trash to clear)',
  'chart.hlineTitle': 'Horizontal line (drag vertically, click trash to clear)',
  'chart.moreToolsTitle': 'More drawing tools (Trend / Ray / Region / Fib)',
  'chart.toolTrend': 'Trend Line',
  'chart.toolRay': 'Ray',
  'chart.toolRegion': 'Region Box',
  'chart.toolFib': 'Fibonacci',
  'chart.magnetTitle': 'OHLC price magnet (hold Ctrl/⌘ to invert temporarily)',
  'chart.clearDrawingsTitle': 'Clear all {count} drawings & rulers',
  'chart.closePaneTitle': 'Close comparison pane',

  // EquityCurve
  'equity.title': 'Equity Curve',
  'equity.emptyHint': 'Not enough settled trades to plot the equity and wallet reconciliation curve.',
  'equity.legendBankroll': 'Trading Bankroll',
  'equity.legendWallet': 'Read-Only Wallet',
  'equity.modeEquity': 'Equity',
  'equity.modeDrawdown': 'Drawdown %',
  'equity.seriesBankroll': 'Bankroll',
  'equity.seriesWallet': 'Wallet',
  'equity.seriesDrawdown': 'DD%',
  'equity.readoutBankroll': 'Bankroll {value}',
  'equity.readoutWallet': 'Wallet {value}',
  'equity.readoutTrade': 'Trade {value}',
  'equity.readoutCumR': 'Cum. R {value}',
  'equity.readoutDrawdown': 'DD {value}',
};
