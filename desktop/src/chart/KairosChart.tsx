import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {
  CandlestickSeries,
  ColorType,
  CrosshairMode,
  HistogramSeries,
  LineStyle,
  createChart,
  createSeriesMarkers,
  type IChartApi,
  type IPriceLine,
  type ISeriesApi,
  type ISeriesMarkersPluginApi,
  type Logical,
  type MouseEventParams,
  type SeriesMarker,
  type Time,
} from 'lightweight-charts';
import {
  ChevronLeft,
  ChevronRight,
  Eye,
  EyeOff,
  Minus,
  Pause,
  Play,
  Ruler,
  Trash2,
  X,
} from 'lucide-react';
import {
  TIMEFRAMES,
  type AnomalyItem,
  type Bar,
  type ChartDrawingItem,
  type ChartDrawingPoint,
  type CycleDirection,
  type DrawingKind,
  type MetricsPoint,
  type Notice,
  type Timeframe,
  type TradeFillItem,
} from '../types/kairos';
import { findBarByTime, snapPriceToOhlc } from '../utils/chartMagnet';
import {
  clearRulerCanvas,
  drawPersistedDrawing,
  drawPriceDragPreview,
  drawReplayCursor,
  drawRulerOnCanvas,
  hitTestDrawingDetailed,
  measureRuler,
  syncOverlayCanvasSize,
  type DrawingHitPart,
  type DrawingHitResult,
  type DrawingVisualState,
  type Point2D,
  type RulerCorner,
} from '../utils/chartRulerOverlay';
import { type AnthropicChartTheme, readChartTheme, withAlpha } from '../utils/chartTheme';
import {
  TIMEFRAME_SECONDS,
  fmtDateTimeSec,
  fmtIsoUtc8,
  fmtPrice,
  fmtQty,
} from '../utils/format';
import {
  RIGHT_PRICE_SCALE_MIN_WIDTH,
  logicalToTimestamp,
  timestampToLogical,
  type SyncChartHandle,
} from './syncCharts';
import { KairosOverlayPrimitive, type TicketOverlaySpec } from './ticketPrimitive';
import { computeSmcOverlay } from '../utils/smc';

export interface ChartPriceLineSpec {
  id: string;
  price: number;
  title: string;
  color: string;
  dashed?: boolean;
  draggable?: boolean;
}

type DrawMode = 'none' | 'ruler' | 'hline' | 'trend' | 'ray' | 'region' | 'fib';

const TWO_POINT_HINTS: Partial<Record<DrawMode, string>> = {
  trend: '趋势线：拖拽或点两点 · Shift 锁角度',
  ray: '射线：拖拽或点两点向右延伸',
  region: '区间框：拖拽或点对角两点',
  fib: '斐波那契：拖拽或点高低两点',
};

export interface KairosChartProps {
  chartId?: string;
  symbol: string;
  timeframe: Timeframe;
  availableTimeframes?: readonly Timeframe[];
  bars: Bar[];
  metrics?: MetricsPoint[];
  entryZone?: { low: number; high: number; signalAt?: number | null } | null;
  direction?: CycleDirection;
  priceLines?: ChartPriceLineSpec[];
  anomalies?: AnomalyItem[];
  notices?: Notice[];
  fills?: TradeFillItem[];
  ticketSignalAt?: number | null;
  drawings?: ChartDrawingItem[];
  loading?: boolean;
  error?: string | null;
  statusSlot?: ReactNode;
  symbolSlot?: ReactNode;
  headerBadge?: string;
  showTimeframeTabs?: boolean;
  showMetricsPane?: boolean;
  visibleRange?: { from: Time; to: Time } | null;
  followLatest?: boolean;
  loadingMore?: boolean;

  // Replay props (ReviewWorkspace)
  cursorTs?: number | null;
  onCursorDrag?: (tsSec: number) => void;
  onStepCursor?: (delta: 1 | -1) => void;
  playing?: boolean;
  onTogglePlay?: () => void;
  playSpeed?: number;
  onPlaySpeedChange?: (speed: number) => void;
  stepBars?: number;
  onStepBarsChange?: (bars: number) => void;
  hideFuture?: boolean;
  onToggleHideFuture?: () => void;
  futureCount?: { shown: number; total: number } | null;

  // Callbacks
  onTimeframeChange?: (tf: Timeframe) => void;
  onLoadMoreHistory?: (oldestTimeSec: number) => void;
  onClosePane?: () => void;
  onDragPriceLine?: (id: string, price: number) => void;
  onUserDrawing?: (kind: DrawingKind, points: ChartDrawingPoint[]) => void;
  onUpdateDrawing?: (id: number, points: ChartDrawingPoint[]) => void;
  onDeleteDrawing?: (id: number) => void;
  onEraseDrawings?: () => void;
  onChartReady?: (handle: SyncChartHandle | null) => void;
}

interface ChartBundle {
  chart: IChartApi;
  candleSeries: ISeriesApi<'Candlestick'>;
  volumeSeries: ISeriesApi<'Histogram'>;
  markerApi: ISeriesMarkersPluginApi<Time>;
  primitive: KairosOverlayPrimitive;
}

interface PinTooltipInfo {
  timeSec: number;
  items: string[];
}

function snapTimestampToBars(bars: readonly Bar[], tsSec: number): number | null {
  if (bars.length === 0 || !Number.isFinite(tsSec) || tsSec <= 0) return null;
  if (tsSec < bars[0].time) return null;
  const bar = findBarByTime(bars, tsSec, false);
  return bar ? bar.time : null;
}

function buildChartMarkers(
  bars: readonly Bar[],
  anomalies: readonly AnomalyItem[],
  notices: readonly Notice[],
  fills: readonly TradeFillItem[],
  ticketSignalAt: number | null | undefined,
  theme: AnthropicChartTheme,
): { markers: SeriesMarker<Time>[]; notesByTime: Map<number, string[]> } {
  const markers: SeriesMarker<Time>[] = [];
  const notesByTime = new Map<number, string[]>();

  const pushNote = (timeSec: number, text: string) => {
    const arr = notesByTime.get(timeSec);
    if (arr) arr.push(text);
    else notesByTime.set(timeSec, [text]);
  };

  const eventRankByBar = new Map<number, number>();
  const bumpEventRank = (timeSec: number, rank: number) => {
    const prev = eventRankByBar.get(timeSec) ?? -1;
    if (rank > prev) eventRankByBar.set(timeSec, rank);
  };

  for (const a of anomalies) {
    const snapped = snapTimestampToBars(bars, a.timestamp);
    if (snapped == null) continue;
    const rank = a.severity === 'HIGH' ? 3 : a.severity === 'MEDIUM' ? 2 : 1;
    bumpEventRank(snapped, rank);
    pushNote(snapped, `[异动 ${a.severity}] ${a.event_type} @ ${fmtDateTimeSec(a.timestamp)}`);
  }

  for (const n of notices) {
    const tsMs = Date.parse(n.ts);
    if (!Number.isFinite(tsMs)) continue;
    const snapped = snapTimestampToBars(bars, Math.floor(tsMs / 1000));
    if (snapped == null) continue;
    bumpEventRank(snapped, 0);
    pushNote(
      snapped,
      `[Notice] ${n.trigger_facts?.fact_summary || n.trigger_facts?.event_type} · ${fmtIsoUtc8(n.ts)}`,
    );
  }

  for (const [barTime, rank] of eventRankByBar.entries()) {
    const color =
      rank >= 3
        ? theme.candleBear
        : rank === 2
          ? theme.stateWarn
          : rank === 1
            ? theme.accentInfo
            : theme.accentBrand;
    markers.push({
      time: barTime as Time,
      position: 'aboveBar',
      color,
      shape: 'circle',
    });
  }

  if (ticketSignalAt != null && ticketSignalAt > 0) {
    const snapped = snapTimestampToBars(bars, ticketSignalAt);
    if (snapped != null) {
      markers.push({
        time: snapped as Time,
        position: 'aboveBar',
        color: theme.accentBrand,
        shape: 'arrowDown',
        text: 'T',
      });
      pushNote(snapped, `[Ticket 信号] ${fmtDateTimeSec(ticketSignalAt)}`);
    }
  }

  // Group fills on the same bar so markers stay legible.
  const fillsByBar = new Map<number, TradeFillItem[]>();
  for (const f of fills) {
    const sec = Math.floor(f.time_ms / 1000);
    const snapped = snapTimestampToBars(bars, sec);
    if (snapped == null) continue;
    const list = fillsByBar.get(snapped);
    if (list) list.push(f);
    else fillsByBar.set(snapped, [f]);
  }
  for (const [barTime, list] of fillsByBar.entries()) {
    const buys = list.filter((f) => f.side.toUpperCase() === 'BUY');
    const sells = list.filter((f) => f.side.toUpperCase() === 'SELL');
    if (buys.length > 0) {
      markers.push({
        time: barTime as Time,
        position: 'belowBar',
        color: theme.candleBull,
        shape: 'arrowUp',
        text: buys.length > 1 ? `B×${buys.length}` : 'B',
      });
    }
    if (sells.length > 0) {
      markers.push({
        time: barTime as Time,
        position: 'aboveBar',
        color: theme.candleBear,
        shape: 'arrowDown',
        text: sells.length > 1 ? `S×${sells.length}` : 'S',
      });
    }
    for (const f of list) {
      pushNote(
        barTime,
        `[${f.side}] ${fmtQty(f.qty)} @ ${fmtPrice(f.price)} (PnL ${f.realized_pnl >= 0 ? '+' : ''}${f.realized_pnl.toFixed(2)})`,
      );
    }
  }

  markers.sort((a, b) => Number(a.time) - Number(b.time));
  return { markers, notesByTime };
}

function toChartOptions(theme: AnthropicChartTheme) {
  return {
    layout: {
      background: { type: ColorType.Solid, color: theme.bgCanvas },
      textColor: theme.textSecondary,
      fontFamily: theme.fontMono,
      fontSize: 11,
    },
    grid: {
      vertLines: { color: theme.gridLine },
      horzLines: { color: theme.gridLine },
    },
    crosshair: {
      mode: CrosshairMode.Normal,
      vertLine: {
        color: withAlpha(theme.textSecondary, 0.35),
        width: 1 as const,
        style: LineStyle.Dashed,
        labelBackgroundColor: theme.bgElevated,
      },
      horzLine: {
        color: withAlpha(theme.textSecondary, 0.35),
        width: 1 as const,
        style: LineStyle.Dashed,
        labelBackgroundColor: theme.bgElevated,
      },
    },
    rightPriceScale: {
      borderColor: theme.borderHairline,
      minimumWidth: RIGHT_PRICE_SCALE_MIN_WIDTH,
      scaleMargins: { top: 0.08, bottom: 0.16 },
    },
    timeScale: {
      borderColor: theme.borderHairline,
      timeVisible: true,
      secondsVisible: false,
      rightOffset: 6,
      barSpacing: 8,
      minBarSpacing: 1.5,
      shiftVisibleRangeOnNewBar: true,
    },
    localization: {
      priceFormatter: (price: number) => fmtPrice(price),
    },
  };
}

export default function KairosChart({
  chartId = 'main',
  symbol,
  timeframe,
  availableTimeframes = TIMEFRAMES,
  bars,
  entryZone = null,
  direction,
  priceLines = [],
  anomalies = [],
  notices = [],
  fills = [],
  ticketSignalAt = null,
  drawings = [],
  loading = false,
  error = null,
  statusSlot,
  symbolSlot,
  headerBadge,
  showTimeframeTabs = true,
  visibleRange = null,
  followLatest = false,
  loadingMore = false,
  cursorTs = null,
  onCursorDrag,
  onStepCursor,
  playing = false,
  onTogglePlay,
  playSpeed = 8,
  onPlaySpeedChange,
  stepBars = 1,
  onStepBarsChange,
  hideFuture = false,
  onToggleHideFuture,
  futureCount = null,
  onTimeframeChange,
  onLoadMoreHistory,
  onClosePane,
  onDragPriceLine,
  onUserDrawing,
  onUpdateDrawing,
  onEraseDrawings,
  onChartReady,
}: KairosChartProps) {
  const shellRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const overlayCanvasRef = useRef<HTMLCanvasElement>(null);
  const legendRef = useRef<HTMLDivElement>(null);

  const bundleRef = useRef<ChartBundle | null>(null);
  const managedPriceLinesRef = useRef<IPriceLine[]>([]);
  const barsRef = useRef<Bar[]>(bars);
  barsRef.current = bars;
  const timeframeRef = useRef<Timeframe>(timeframe);
  timeframeRef.current = timeframe;
  const drawingsRef = useRef<ChartDrawingItem[]>(drawings);
  drawingsRef.current = drawings;
  const priceLinesRef = useRef<ChartPriceLineSpec[]>(priceLines);
  priceLinesRef.current = priceLines;
  const notesByTimeRef = useRef<Map<number, string[]>>(new Map());

  const [theme, setTheme] = useState<AnthropicChartTheme>(() => readChartTheme());
  const themeRef = useRef(theme);
  themeRef.current = theme;

  const magnetRef = useRef(false);

  const [selectedDrawingId, setSelectedDrawingId] = useState<number | null>(null);
  const selectedDrawingIdRef = useRef<number | null>(null);
  selectedDrawingIdRef.current = selectedDrawingId;

  const [drawMode, setDrawMode] = useState<DrawMode>('none');
  const drawModeRef = useRef(drawMode);
  drawModeRef.current = drawMode;

  const [rulerHint, setRulerHint] = useState('');
  const [smcOpen, setSmcOpen] = useState<boolean>(false);
  const [pinTooltip, setPinTooltip] = useState<PinTooltipInfo | null>(null);
  const [chartEpoch, setChartEpoch] = useState(0);

  const rulerCornerRef = useRef<RulerCorner | null>(null);
  const rulerPreviewRef = useRef<RulerCorner | null>(null);
  const rulerResultRef = useRef<{ a: RulerCorner; b: RulerCorner } | null>(null);
  const hoverPreviewRef = useRef<RulerCorner | null>(null);
  const magnetRingRef = useRef<Point2D | null>(null);
  const drawPressStartRef = useRef<Point2D | null>(null);
  const quickRulerActiveRef = useRef(false);
  const quickRulerStartRef = useRef<Point2D | null>(null);
  const hoveredHitRef = useRef<DrawingHitResult | null>(null);
  const editDragRef = useRef<{
    drawing: ChartDrawingItem;
    part: DrawingHitPart;
    startX: number;
    startY: number;
    origPts: Array<{ x: number; y: number; time: number; price: number }>;
    previewPoints: ChartDrawingPoint[];
    moved: boolean;
  } | null>(null);
  const dragLineRef = useRef<{ id: string; title: string; color: string; price: number } | null>(
    null,
  );
  const cursorDragRef = useRef(false);
  const cursorTsRef = useRef<number | null>(cursorTs);
  cursorTsRef.current = cursorTs;
  const onCursorDragRef = useRef(onCursorDrag);
  onCursorDragRef.current = onCursorDrag;
  const onDragPriceLineRef = useRef(onDragPriceLine);
  onDragPriceLineRef.current = onDragPriceLine;
  const onUserDrawingRef = useRef(onUserDrawing);
  onUserDrawingRef.current = onUserDrawing;
  const onUpdateDrawingRef = useRef(onUpdateDrawing);
  onUpdateDrawingRef.current = onUpdateDrawing;
  const onLoadMoreHistoryRef = useRef(onLoadMoreHistory);
  onLoadMoreHistoryRef.current = onLoadMoreHistory;

  // Incremental update tracking
  const prevSeriesKeyRef = useRef('');
  const prevFirstTimeRef = useRef<number | null>(null);
  const prevLenRef = useRef(0);
  const didInitViewRef = useRef(false);
  const followEndRef = useRef(true);
  const isHoveringCrosshairRef = useRef(false);
  const isRestoringPrependRef = useRef(false);
  const renderLegendRef = useRef<(bar?: Bar) => void>(() => {});
  const paintOverlayRef = useRef<() => void>(() => {});
  const rafPaintRef = useRef<number | null>(null);
  const schedulePaintOverlay = useCallback(() => {
    if (rafPaintRef.current !== null) return;
    rafPaintRef.current = requestAnimationFrame(() => {
      rafPaintRef.current = null;
      paintOverlayRef.current();
    });
  }, []);

  // Observe theme changes on <html data-theme="...">
  useEffect(() => {
    const observer = new MutationObserver(() => {
      setTheme(readChartTheme());
    });
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    });
    return () => observer.disconnect();
  }, []);

  const timeToContinuousX = useCallback((tsSec: number): number | null => {
    const bundle = bundleRef.current;
    if (!bundle || !Number.isFinite(tsSec)) return null;
    const list = barsRef.current;
    const tsApi = bundle.chart.timeScale();
    if (list.length === 0) {
      const x = tsApi.timeToCoordinate(tsSec as Time);
      return x != null && Number.isFinite(x) ? x : null;
    }
    const tfSec = TIMEFRAME_SECONDS[timeframeRef.current] ?? 300;
    const logical = timestampToLogical(list, tfSec, tsSec);
    if (logical == null) return null;
    const lo = Math.floor(logical);
    const frac = logical - lo;
    const x0 = tsApi.logicalToCoordinate(lo as Logical);
    if (x0 == null || !Number.isFinite(x0)) return null;
    if (Math.abs(frac) < 1e-6) return x0;
    const x1 = tsApi.logicalToCoordinate((lo + 1) as Logical);
    return x1 != null && Number.isFinite(x1) ? x0 + frac * (x1 - x0) : x0;
  }, []);

  const priceToY = useCallback((price: number): number | null => {
    const bundle = bundleRef.current;
    if (!bundle) return null;
    const y = bundle.candleSeries.priceToCoordinate(price);
    return y != null && Number.isFinite(y) ? y : null;
  }, []);

  const mapDrawingPoint = useCallback(
    (p: ChartDrawingPoint): Point2D | null => {
      const x = timeToContinuousX(p.time);
      const y = priceToY(p.price);
      if (x == null || y == null || !Number.isFinite(x) || !Number.isFinite(y)) return null;
      return { x, y };
    },
    [timeToContinuousX, priceToY],
  );

  const remapRulerCorner = useCallback(
    (c: RulerCorner): RulerCorner | null => {
      const tSec = Number(c.time);
      const x = timeToContinuousX(tSec);
      const y = priceToY(c.price);
      if (x == null || y == null || !Number.isFinite(x) || !Number.isFinite(y)) return null;
      const list = barsRef.current;
      const tfSec = TIMEFRAME_SECONDS[timeframeRef.current] ?? 300;
      const logical =
        list.length > 0 ? (timestampToLogical(list, tfSec, tSec) ?? c.logical) : c.logical;
      return { time: c.time, price: c.price, x, y, logical };
    },
    [timeToContinuousX, priceToY],
  );

  const xToContinuousPoint = useCallback(
    (
      x: number,
      y: number,
      opts?: {
        forceMagnet?: boolean;
        shiftAnchor?: RulerCorner | null;
        allowAngleLock?: boolean;
      },
    ): (RulerCorner & { snapped?: boolean }) | null => {
      const bundle = bundleRef.current;
      if (!bundle) return null;
      const rawPrice = bundle.candleSeries.coordinateToPrice(y) as number | null;
      if (rawPrice == null || !Number.isFinite(rawPrice) || rawPrice <= 0) return null;

      const tsApi = bundle.chart.timeScale();
      const list = barsRef.current;
      const tfSec = TIMEFRAME_SECONDS[timeframeRef.current] ?? 300;

      if (list.length === 0) {
        const t = tsApi.coordinateToTime(x) as Time | null;
        if (t == null) return null;
        return { time: t, price: rawPrice, x, y, snapped: false };
      }

      const nearLog = tsApi.coordinateToLogical(x);
      if (nearLog == null || !Number.isFinite(Number(nearLog))) return null;

      const baseIdx = Math.round(Number(nearLog));
      const x0 = tsApi.logicalToCoordinate(baseIdx as Logical);
      const x1 = tsApi.logicalToCoordinate((baseIdx + 1) as Logical);
      const stepPx = x0 != null && x1 != null && x1 !== x0 ? x1 - x0 : 8;
      const exactLogical =
        x0 != null && stepPx > 0 ? baseIdx + (x - x0) / stepPx : Number(nearLog);

      let outLogical = exactLogical;
      let outX = x;
      let outY = y;
      let outPrice = rawPrice;
      const rawTs = logicalToTimestamp(list, tfSec, exactLogical);
      if (rawTs == null) return null;
      let outTimeSec = Math.round(rawTs);
      let snapped = false;

      if (opts?.forceMagnet && baseIdx >= 0 && baseIdx < list.length) {
        const bar = list[baseIdx];
        const mag = snapPriceToOhlc(
          rawPrice,
          bar,
          (p) => bundle.candleSeries.priceToCoordinate(p) as number | null,
          y,
          12,
        );
        if (mag.snapped) {
          snapped = true;
          outPrice = mag.price;
          outTimeSec = bar.time;
          outLogical = baseIdx;
          if (x0 != null && Number.isFinite(x0)) outX = x0;
          const sy = bundle.candleSeries.priceToCoordinate(mag.price) as number | null;
          if (sy != null && Number.isFinite(sy)) outY = sy;
        }
      }

      if (opts?.shiftAnchor && opts?.allowAngleLock) {
        const anc = opts.shiftAnchor;
        const ancMapped = remapRulerCorner(anc) ?? anc;
        const dx = outX - ancMapped.x;
        const dy = outY - ancMapped.y;
        if (Math.abs(dy) <= Math.abs(dx) * 0.42) {
          outPrice = anc.price;
          outY = ancMapped.y;
        } else if (Math.abs(dx) <= Math.abs(dy) * 0.42) {
          outTimeSec = Number(anc.time);
          outLogical = anc.logical ?? outLogical;
          outX = ancMapped.x;
        } else {
          const diagY = ancMapped.y + Math.sign(dy) * Math.abs(dx);
          const diagP = bundle.candleSeries.coordinateToPrice(diagY) as number | null;
          if (diagP != null && Number.isFinite(diagP) && diagP > 0) {
            outY = diagY;
            outPrice = diagP;
          }
        }
      }

      return {
        time: outTimeSec as Time,
        price: outPrice,
        x: outX,
        y: outY,
        logical: outLogical,
        snapped,
      };
    },
    [remapRulerCorner],
  );

  const paintOverlay = useCallback(() => {
    const canvas = overlayCanvasRef.current;
    const container = containerRef.current;
    const bundle = bundleRef.current;
    if (!canvas || !container) return;

    const { width, height } = syncOverlayCanvasSize(canvas, container);
    clearRulerCanvas(canvas);
    const ctx = canvas.getContext('2d');
    if (!ctx || !bundle) return;

    const currentTheme = themeRef.current;
    const plotWidth =
      bundle.chart.timeScale().width() || Math.max(1, width - RIGHT_PRICE_SCALE_MIN_WIDTH);
    const pane0Height =
      bundle.chart.panes()[0]?.getHeight?.() ?? height;

    ctx.save();
    ctx.beginPath();
    ctx.rect(0, 0, plotWidth, pane0Height);
    ctx.clip();

    const activeEdit = editDragRef.current;
    const selId = selectedDrawingIdRef.current;
    const hoverId = hoveredHitRef.current?.drawing.id ?? null;
    const stepSec = TIMEFRAME_SECONDS[timeframeRef.current] ?? 300;

    for (const drawing of drawingsRef.current) {
      const isEditingThis = activeEdit != null && activeEdit.drawing.id === drawing.id;
      const spec = isEditingThis
        ? { ...drawing, points: activeEdit.previewPoints }
        : drawing;
      const visualState: DrawingVisualState =
        isEditingThis || drawing.id === selId
          ? 'selected'
          : drawing.id === hoverId
            ? 'hover'
            : 'normal';

      drawPersistedDrawing(
        ctx,
        plotWidth,
        pane0Height,
        spec,
        mapDrawingPoint,
        priceToY,
        currentTheme,
        visualState,
        stepSec,
      );
    }

    const mode = drawModeRef.current;
    const pair = rulerResultRef.current;

    if (pair) {
      const a = remapRulerCorner(pair.a);
      const b = remapRulerCorner(pair.b);
      if (a && b) {
        drawRulerOnCanvas(
          ctx,
          plotWidth,
          pane0Height,
          a,
          b,
          measureRuler(a, b, stepSec),
          currentTheme,
        );
      }
    } else if (mode === 'hline' && hoverPreviewRef.current) {
      drawPersistedDrawing(
        ctx,
        plotWidth,
        pane0Height,
        {
          kind: 'hline',
          points: [{ time: Number(hoverPreviewRef.current.time), price: hoverPreviewRef.current.price }],
        },
        mapDrawingPoint,
        priceToY,
        currentTheme,
        'preview',
      );
    } else if (rulerCornerRef.current && rulerPreviewRef.current) {
      const a = remapRulerCorner(rulerCornerRef.current) ?? rulerCornerRef.current;
      const b = remapRulerCorner(rulerPreviewRef.current) ?? rulerPreviewRef.current;
      if (quickRulerActiveRef.current || mode === 'ruler') {
        drawRulerOnCanvas(
          ctx,
          plotWidth,
          pane0Height,
          a,
          b,
          measureRuler(a, b, stepSec),
          currentTheme,
        );
      } else if (mode !== 'none') {
        drawPersistedDrawing(
          ctx,
          plotWidth,
          pane0Height,
          {
            kind: mode as DrawingKind,
            points: [
              { time: Number(a.time), price: a.price },
              { time: Number(b.time), price: b.price },
            ],
          },
          mapDrawingPoint,
          priceToY,
          currentTheme,
          'preview',
        );
      }
    }

    const magPt = magnetRingRef.current;
    if (magPt) {
      ctx.save();
      ctx.strokeStyle = currentTheme.accentBrand;
      ctx.fillStyle = withAlpha(currentTheme.accentBrand, 0.22);
      ctx.lineWidth = 1.5;
      ctx.beginPath();
      ctx.arc(magPt.x, magPt.y, 4.5, 0, Math.PI * 2);
      ctx.fill();
      ctx.stroke();
      ctx.restore();
    }

    const drag = dragLineRef.current;
    if (drag) {
      const y = priceToY(drag.price);
      if (y != null) {
        drawPriceDragPreview(ctx, plotWidth, y, drag.title, drag.price, drag.color, currentTheme);
      }
    }

    ctx.restore();

    // Replay cursor spans all panes vertically
    const ct = cursorTsRef.current;
    if (ct != null && ct > 0) {
      const x = timeToContinuousX(ct);
      if (x != null && Number.isFinite(x)) {
        drawReplayCursor(ctx, plotWidth, height, x, ct, currentTheme);
      }
    }
  }, [mapDrawingPoint, priceToY, remapRulerCorner]);

  paintOverlayRef.current = paintOverlay;

  const clearTransientRuler = useCallback(() => {
    rulerCornerRef.current = null;
    rulerPreviewRef.current = null;
    rulerResultRef.current = null;
    setRulerHint('');
    paintOverlayRef.current();
  }, []);

  // Mount chart instance (remounts only when metrics pane presence changes)
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const currentTheme = themeRef.current;
    const chart = createChart(el, {
      ...toChartOptions(currentTheme),
      width: Math.max(1, el.clientWidth),
      height: Math.max(1, el.clientHeight),
    });

    const candleSeries = chart.addSeries(
      CandlestickSeries,
      {
        upColor: currentTheme.candleBull,
        downColor: currentTheme.candleBear,
        wickUpColor: currentTheme.candleBullWick,
        wickDownColor: currentTheme.candleBearWick,
        borderUpColor: currentTheme.candleBull,
        borderDownColor: currentTheme.candleBear,
        priceLineVisible: true,
        lastValueVisible: true,
      },
      0,
    );

    const volumeSeries = chart.addSeries(
      HistogramSeries,
      {
        priceScaleId: 'vol',
        priceFormat: { type: 'volume' },
        priceLineVisible: false,
        lastValueVisible: false,
      },
      0,
    );
    volumeSeries.priceScale().applyOptions({
      visible: false,
      scaleMargins: { top: 0.86, bottom: 0 },
    });

    const markerApi = createSeriesMarkers(candleSeries, []);
    const primitive = new KairosOverlayPrimitive();
    candleSeries.attachPrimitive(primitive);

    bundleRef.current = {
      chart,
      candleSeries,
      volumeSeries,
      markerApi,
      primitive,
    };
    didInitViewRef.current = false;
    prevSeriesKeyRef.current = '';
    prevLenRef.current = 0;
    setChartEpoch((e) => e + 1);

    const updateLegend = (bar?: Bar) => {
      const target = legendRef.current;
      if (!target) return;
      if (!bar) {
        target.textContent = '暂无 K 线数据';
        target.style.color = 'var(--text-muted)';
        return;
      }
      const chg = bar.open !== 0 ? ((bar.close - bar.open) / bar.open) * 100 : 0;
      const amp = bar.open !== 0 ? ((bar.high - bar.low) / bar.open) * 100 : 0;
      const sign = chg >= 0 ? '+' : '';
      target.textContent =
        `开 ${fmtPrice(bar.open)}  高 ${fmtPrice(bar.high)}  低 ${fmtPrice(bar.low)}  收 ${fmtPrice(bar.close)}` +
        `  ${sign}${chg.toFixed(2)}%  振幅 ${amp.toFixed(2)}%`;
      target.style.color = chg >= 0 ? 'var(--candle-bull)' : 'var(--candle-bear)';
    };
    renderLegendRef.current = updateLegend;

    const onCrosshairMove = (param: MouseEventParams<Time>) => {
      const list = barsRef.current;
      if (param.time == null) {
        isHoveringCrosshairRef.current = false;
        updateLegend(list[list.length - 1]);
        setPinTooltip((prev) => (prev === null ? prev : null));
        return;
      }
      isHoveringCrosshairRef.current = true;
      const tSec = Number(param.time);
      const bar = findBarByTime(list, tSec, true) ?? list[list.length - 1];
      updateLegend(bar);

      const notes = notesByTimeRef.current.get(tSec);
      if (notes && notes.length > 0) {
        setPinTooltip({ timeSec: tSec, items: notes });
      } else {
        setPinTooltip((prev) => (prev === null ? prev : null));
      }
    };

    const onVisibleRangeChange = () => {
      if (followLatest) {
        try {
          const range = chart.timeScale().getVisibleRange();
          const lastBar = barsRef.current[barsRef.current.length - 1];
          if (range && lastBar && typeof range.to === 'number') {
            const step = TIMEFRAME_SECONDS[timeframeRef.current] ?? 300;
            followEndRef.current = range.to >= lastBar.time - step * 3;
          }
        } catch {
          // Ignore range read error.
        }
      }
      schedulePaintOverlay();
    };

    const onLogicalRangeChange = () => {
      schedulePaintOverlay();
      if (isRestoringPrependRef.current) return;
      try {
        const logical = chart.timeScale().getVisibleLogicalRange();
        if (logical && logical.from < 50 && barsRef.current.length > 0) {
          onLoadMoreHistoryRef.current?.(barsRef.current[0].time);
        }
      } catch {
        // Ignore unready range read.
      }
    };

    chart.subscribeCrosshairMove(onCrosshairMove);
    chart.timeScale().subscribeVisibleTimeRangeChange(onVisibleRangeChange);
    chart.timeScale().subscribeVisibleLogicalRangeChange(onLogicalRangeChange);

    const ro = new ResizeObserver(() => {
      if (!containerRef.current || containerRef.current.clientWidth === 0) return;
      chart.applyOptions({
        width: containerRef.current.clientWidth,
        height: containerRef.current.clientHeight,
      });
      paintOverlayRef.current();
    });
    ro.observe(el);

    onChartReady?.({
      id: chartId,
      chart,
      series: candleSeries,
      getBars: () => barsRef.current,
      getTimeframeSec: () => TIMEFRAME_SECONDS[timeframeRef.current] ?? 300,
      requestHistoryBefore: (tsSec) => onLoadMoreHistoryRef.current?.(tsSec),
    });

    return () => {
      if (rafPaintRef.current !== null) {
        cancelAnimationFrame(rafPaintRef.current);
        rafPaintRef.current = null;
      }
      onChartReady?.(null);
      ro.disconnect();
      chart.unsubscribeCrosshairMove(onCrosshairMove);
      chart.timeScale().unsubscribeVisibleTimeRangeChange(onVisibleRangeChange);
      chart.timeScale().unsubscribeVisibleLogicalRangeChange(onLogicalRangeChange);
      try {
        candleSeries.detachPrimitive(primitive);
      } catch {
        // Ignore if disposed.
      }
      chart.remove();
      bundleRef.current = null;
    };
  }, [chartId, followLatest, onChartReady, schedulePaintOverlay]);

  // Apply theme updates live without recreating chart
  useEffect(() => {
    const bundle = bundleRef.current;
    if (!bundle) return;
    bundle.chart.applyOptions(toChartOptions(theme));
    bundle.candleSeries.applyOptions({
      upColor: theme.candleBull,
      downColor: theme.candleBear,
      wickUpColor: theme.candleBullWick,
      wickDownColor: theme.candleBearWick,
      borderUpColor: theme.candleBull,
      borderDownColor: theme.candleBear,
    });
    bundle.primitive.refreshTheme(theme);
    paintOverlayRef.current();
  }, [theme]);

  // Update candle, volume, and metrics data
  useEffect(() => {
    const bundle = bundleRef.current;
    if (!bundle) return;

    const seriesKey = `${symbol}|${timeframe}`;
    const firstTime = bars[0]?.time ?? null;
    const bullVolColor = withAlpha(theme.candleBull, 0.22);
    const bearVolColor = withAlpha(theme.candleBear, 0.22);
    const canIncrementalUpdate =
      prevSeriesKeyRef.current === seriesKey &&
      prevFirstTimeRef.current === firstTime &&
      prevLenRef.current > 0 &&
      bars.length >= prevLenRef.current &&
      bars.length <= prevLenRef.current + 2;

    const isPrepend =
      !canIncrementalUpdate &&
      prevSeriesKeyRef.current === seriesKey &&
      prevFirstTimeRef.current != null &&
      firstTime != null &&
      firstTime < prevFirstTimeRef.current;

    if (canIncrementalUpdate) {
      const startIdx = Math.max(0, prevLenRef.current - 1);
      for (let i = startIdx; i < bars.length; i += 1) {
        const b = bars[i];
        bundle.candleSeries.update({
          time: b.time as Time,
          open: b.open,
          high: b.high,
          low: b.low,
          close: b.close,
        });
        bundle.volumeSeries.update({
          time: b.time as Time,
          value: b.volume,
          color: b.close >= b.open ? bullVolColor : bearVolColor,
        });
      }
    } else {
      let savedLogical: { from: number; to: number } | null = null;
      const oldFirstTime = prevFirstTimeRef.current;
      if (isPrepend) {
        try {
          const lr = bundle.chart.timeScale().getVisibleLogicalRange();
          if (lr && Number.isFinite(lr.from) && Number.isFinite(lr.to)) {
            savedLogical = { from: lr.from, to: lr.to };
          }
        } catch {
          savedLogical = null;
        }
        isRestoringPrependRef.current = true;
      }

      try {
        bundle.candleSeries.setData(
          bars.map((b) => ({
            time: b.time as Time,
            open: b.open,
            high: b.high,
            low: b.low,
            close: b.close,
          })),
        );
        bundle.volumeSeries.setData(
          bars.map((b) => ({
            time: b.time as Time,
            value: b.volume,
            color: b.close >= b.open ? bullVolColor : bearVolColor,
          })),
        );

        if (isPrepend && savedLogical && oldFirstTime != null) {
          const shift = bars.findIndex((b) => b.time === oldFirstTime);
          if (shift > 0) {
            const nextFrom = savedLogical.from + shift;
            const nextTo = savedLogical.to + shift;
            try {
              bundle.chart.timeScale().setVisibleLogicalRange({
                from: nextFrom,
                to: nextTo,
              });
            } catch {
              // Ignore restore error.
            }
            if (nextFrom < 50 && bars.length > 0) {
              queueMicrotask(() => {
                onLoadMoreHistoryRef.current?.(barsRef.current[0]?.time ?? bars[0].time);
              });
            }
          }
        }
      } finally {
        isRestoringPrependRef.current = false;
      }
    }

    if (bars.length > 0) {
      const seriesChanged = prevSeriesKeyRef.current !== seriesKey;
      if (!didInitViewRef.current || seriesChanged) {
        if (visibleRange && visibleRange.from != null && visibleRange.to != null) {
          try {
            bundle.chart.timeScale().setVisibleRange(visibleRange);
          } catch {
            bundle.chart.timeScale().fitContent();
          }
        } else if (bars.length > 160) {
          try {
            bundle.chart.timeScale().setVisibleLogicalRange({
              from: bars.length - 160,
              to: bars.length - 1 + 6,
            });
          } catch {
            bundle.chart.timeScale().fitContent();
          }
        } else {
          bundle.chart.timeScale().fitContent();
        }
        didInitViewRef.current = true;
        followEndRef.current = true;
      } else if (!isPrepend && followLatest && followEndRef.current && bars.length > prevLenRef.current) {
        try {
          bundle.chart.timeScale().scrollToRealTime();
        } catch {
          // Ignore scroll error.
        }
      }
      prevSeriesKeyRef.current = seriesKey;
      prevFirstTimeRef.current = firstTime;
      prevLenRef.current = bars.length;
    } else {
      prevSeriesKeyRef.current = '';
      prevFirstTimeRef.current = null;
      prevLenRef.current = 0;
    }

    if (!isHoveringCrosshairRef.current) {
      renderLegendRef.current(bars[bars.length - 1]);
    }
    paintOverlayRef.current();
  }, [bars, symbol, timeframe, theme, visibleRange, followLatest, chartEpoch]);

  // Apply explicit visibleRange updates when requested by parent (e.g. jump to ticket anchor)
  useEffect(() => {
    const bundle = bundleRef.current;
    if (!bundle || !visibleRange || barsRef.current.length === 0) return;
    try {
      bundle.chart.timeScale().setVisibleRange(visibleRange);
      paintOverlayRef.current();
    } catch {
      // Ignore invalid range before data settles.
    }
  }, [visibleRange, chartEpoch]);

  const markerBarKey = `${bars.length}|${bars[0]?.time ?? 0}|${bars[bars.length - 1]?.time ?? 0}`;
  const smcBarKey = `${bars.length}|${bars[0]?.time ?? 0}|${bars[bars.length - 1]?.time ?? 0}|${bars.length >= 2 ? bars[bars.length - 2].close : 0}`;

  // Update markers
  useEffect(() => {
    const bundle = bundleRef.current;
    if (!bundle || !markerBarKey) return;
    const { markers, notesByTime } = buildChartMarkers(
      barsRef.current,
      anomalies,
      notices,
      fills,
      ticketSignalAt,
      theme,
    );
    notesByTimeRef.current = notesByTime;
    bundle.markerApi.setMarkers(markers);
  }, [markerBarKey, anomalies, notices, fills, ticketSignalAt, theme, chartEpoch]);

  const smcOverlay = useMemo(
    () => (smcOpen && smcBarKey ? computeSmcOverlay(bars) : null),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [smcOpen, smcBarKey],
  );

  // Update single reusable ISeriesPrimitive (SMC + EntryZone + 1R/2R/3R)
  useEffect(() => {
    const bundle = bundleRef.current;
    if (!bundle) return;
    const entryLine = priceLines.find((l) => l.id === 'entry');
    const stopLine = priceLines.find((l) => l.id === 'stop');
    const targetLine = priceLines.find((l) => l.id === 'target');
    const overlaySpec: TicketOverlaySpec = {
      smc: smcOverlay,
      zoneLow: entryZone?.low ?? null,
      zoneHigh: entryZone?.high ?? null,
      signalAt: entryZone?.signalAt ?? ticketSignalAt ?? null,
      entryPrice: entryLine?.price ?? null,
      stopPrice: stopLine?.price ?? null,
      targetPrice: targetLine?.price ?? null,
      direction,
      firstBarTime: bars[0]?.time ?? null,
      lastBarTime: bars[bars.length - 1]?.time ?? null,
    };
    bundle.primitive.setData(overlaySpec, theme);
  }, [smcOverlay, entryZone, ticketSignalAt, priceLines, direction, bars, theme, chartEpoch]);

  // Sync horizontal price lines (Entry / Stop / Target / Liquidation)
  useEffect(() => {
    const bundle = bundleRef.current;
    if (!bundle) return;
    for (const line of managedPriceLinesRef.current) {
      try {
        bundle.candleSeries.removePriceLine(line);
      } catch {
        // Already removed.
      }
    }
    managedPriceLinesRef.current = [];
    for (const spec of priceLines) {
      if (!Number.isFinite(spec.price) || spec.price <= 0) continue;
      try {
        const created = bundle.candleSeries.createPriceLine({
          price: spec.price,
          color: spec.color,
          lineWidth: spec.dashed ? 1 : 2,
          lineStyle: spec.dashed ? LineStyle.Dashed : LineStyle.Solid,
          axisLabelVisible: true,
          title: spec.title,
        });
        managedPriceLinesRef.current.push(created);
      } catch {
        // Series disposed.
      }
    }
  }, [priceLines, chartEpoch]);

  // Repaint overlay canvas when drawings or cursorTs change
  useEffect(() => {
    paintOverlay();
  }, [drawings, cursorTs, theme, paintOverlay]);

  // Clear local transient drawing/selection state when switching symbol
  useEffect(() => {
    clearTransientRuler();
    setSelectedDrawingId(null);
    hoveredHitRef.current = null;
    editDragRef.current = null;
    hoverPreviewRef.current = null;
    magnetRingRef.current = null;
  }, [symbol, clearTransientRuler]);

  // Pointer interaction: dual-gesture drawing, quick Shift+Drag ruler, selection/handle drag, right-click delete
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const getPlotBounds = (rect: DOMRect) => {
      const bundle = bundleRef.current;
      const plotWidth = bundle
        ? bundle.chart.timeScale().width() || Math.max(1, rect.width - RIGHT_PRICE_SCALE_MIN_WIDTH)
        : Math.max(1, rect.width - RIGHT_PRICE_SCALE_MIN_WIDTH);
      const pane0Height = bundle?.chart.panes()[0]?.getHeight?.() ?? rect.height;
      return { plotWidth, pane0Height };
    };

    const isInsidePlot = (x: number, y: number, plotWidth: number, pane0Height: number) =>
      x >= 0 && x <= plotWidth && y >= 0 && y <= pane0Height;

    const hitTestPriceLine = (y: number): ChartPriceLineSpec | null => {
      if (!onDragPriceLineRef.current) return null;
      const bundle = bundleRef.current;
      if (!bundle) return null;
      let best: { spec: ChartPriceLineSpec; dist: number } | null = null;
      for (const spec of priceLinesRef.current) {
        if (!spec.draggable || !Number.isFinite(spec.price)) continue;
        const ly = bundle.candleSeries.priceToCoordinate(spec.price);
        if (ly == null || !Number.isFinite(ly)) continue;
        const dist = Math.abs(ly - y);
        if (dist <= 8 && (best == null || dist < best.dist)) {
          best = { spec, dist };
        }
      }
      return best?.spec ?? null;
    };

    const getCursorX = (): number | null => {
      const ct = cursorTsRef.current;
      const bundle = bundleRef.current;
      if (ct == null || !onCursorDragRef.current || !bundle) return null;
      const x = timeToContinuousX(ct);
      return x != null && Number.isFinite(x) ? x : null;
    };

    const commitTwoPointShape = (mode: DrawMode, pending: RulerCorner, end: RulerCorner) => {
      if (mode !== 'none' && mode !== 'hline') {
        onUserDrawingRef.current?.(mode as DrawingKind, [
          { time: Number(pending.time), price: pending.price },
          { time: Number(end.time), price: end.price },
        ]);
      }
      rulerCornerRef.current = null;
      rulerPreviewRef.current = null;
      rulerResultRef.current = null;
      drawPressStartRef.current = null;
      magnetRingRef.current = null;
      setRulerHint('');
      setDrawMode('none');
      paintOverlayRef.current();
    };

    let moveRaf = 0;

    const onMouseDownCapture = (e: MouseEvent) => {
      if (e.button !== 0) return;
      const target = e.target as Node | null;
      if (!target || !el.contains(target)) return;
      const rect = el.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;
      const { plotWidth, pane0Height } = getPlotBounds(rect);
      if (!isInsidePlot(x, y, plotWidth, pane0Height)) return;
      if (
        drawModeRef.current !== 'none' ||
        e.shiftKey ||
        editDragRef.current != null ||
        cursorDragRef.current ||
        dragLineRef.current != null
      ) {
        e.preventDefault();
        e.stopPropagation();
      }
    };

    const onPointerDown = (e: PointerEvent) => {
      if (e.button !== 0) return;
      const target = e.target as Node | null;
      if (!target || !el.contains(target)) return;
      const bundle = bundleRef.current;
      if (!bundle) return;

      const rect = el.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;
      const { plotWidth, pane0Height } = getPlotBounds(rect);
      if (!isInsidePlot(x, y, plotWidth, pane0Height)) return;

      const mode = drawModeRef.current;
      const useMagnet = magnetRef.current !== (e.ctrlKey || e.metaKey);

      if (mode !== 'none') {
        const allowAngle = e.shiftKey && (mode === 'trend' || mode === 'ray');
        const pt = xToContinuousPoint(x, y, {
          forceMagnet: useMagnet,
          shiftAnchor: rulerCornerRef.current,
          allowAngleLock: allowAngle,
        });
        if (!pt) return;
        rulerResultRef.current = null;

        if (mode === 'hline') {
          hoverPreviewRef.current = null;
          magnetRingRef.current = null;
          onUserDrawingRef.current?.('hline', [{ time: Number(pt.time), price: pt.price }]);
          setDrawMode('none');
          paintOverlayRef.current();
          e.preventDefault();
          e.stopPropagation();
          return;
        }

        const pending = rulerCornerRef.current;
        if (!pending) {
          rulerCornerRef.current = pt;
          rulerPreviewRef.current = pt;
          drawPressStartRef.current = { x, y };
          setRulerHint(mode === 'ruler' ? '拖拽或移动再点完成' : '移动预览，再点或松开完成');
          paintOverlayRef.current();
        } else {
          commitTwoPointShape(mode, pending, pt);
        }
        e.preventDefault();
        e.stopPropagation();
        return;
      }

      // Click anywhere in idle mode dismisses previous finished ruler
      if (rulerResultRef.current) {
        rulerResultRef.current = null;
        paintOverlayRef.current();
      }

      // Shift+Drag Quick Ruler
      if (e.shiftKey) {
        const pt = xToContinuousPoint(x, y, { forceMagnet: useMagnet });
        if (!pt) return;
        quickRulerActiveRef.current = true;
        quickRulerStartRef.current = { x, y };
        rulerCornerRef.current = pt;
        rulerPreviewRef.current = pt;
        paintOverlayRef.current();
        e.preventDefault();
        e.stopPropagation();
        return;
      }

      // Replay vertical cursor drag (priority within ±6px)
      const cx = getCursorX();
      if (cx != null && Math.abs(x - cx) <= 6) {
        cursorDragRef.current = true;
        e.preventDefault();
        e.stopPropagation();
        return;
      }

      // Draggable Entry / Stop / Target price line
      const line = hitTestPriceLine(y);
      if (line) {
        const price = bundle.candleSeries.coordinateToPrice(y) as number | null;
        if (price == null || !Number.isFinite(price)) return;
        dragLineRef.current = {
          id: line.id,
          title: line.title,
          color: line.color,
          price,
        };
        paintOverlayRef.current();
        e.preventDefault();
        e.stopPropagation();
        return;
      }

      // Hit-test persisted drawings for selection + handle/body drag
      const hit = hitTestDrawingDetailed(
        x,
        y,
        plotWidth,
        drawingsRef.current,
        mapDrawingPoint,
        priceToY,
        selectedDrawingIdRef.current,
      );
      if (hit) {
        selectedDrawingIdRef.current = hit.drawing.id;
        setSelectedDrawingId(hit.drawing.id);
        const origPts = hit.drawing.points.map((p) => ({
          x: timeToContinuousX(p.time) ?? x,
          y: priceToY(p.price) ?? y,
          time: p.time,
          price: p.price,
        }));
        editDragRef.current = {
          drawing: hit.drawing,
          part: hit.part,
          startX: x,
          startY: y,
          origPts,
          previewPoints: hit.drawing.points.map((p) => ({ ...p })),
          moved: false,
        };
        paintOverlayRef.current();
        e.preventDefault();
        e.stopPropagation();
        return;
      }

      if (selectedDrawingIdRef.current != null) {
        selectedDrawingIdRef.current = null;
        setSelectedDrawingId(null);
        paintOverlayRef.current();
      }
    };

    const onPointerMove = (e: PointerEvent) => {
      const bundle = bundleRef.current;
      if (!bundle) return;
      const rect = el.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;
      const { plotWidth, pane0Height } = getPlotBounds(rect);
      const useMagnet = magnetRef.current !== (e.ctrlKey || e.metaKey);

      if (cursorDragRef.current) {
        const pt = xToContinuousPoint(x, y, { forceMagnet: false });
        if (pt && Number.isFinite(Number(pt.time))) {
          onCursorDragRef.current?.(Number(pt.time));
        }
        e.preventDefault();
        return;
      }

      if (dragLineRef.current) {
        const pt = xToContinuousPoint(x, y, { forceMagnet: useMagnet });
        if (pt && pt.price > 0) {
          dragLineRef.current = { ...dragLineRef.current, price: pt.price };
          if (moveRaf) cancelAnimationFrame(moveRaf);
          moveRaf = requestAnimationFrame(() => paintOverlayRef.current());
        }
        e.preventDefault();
        return;
      }

      const ed = editDragRef.current;
      if (ed) {
        if (Math.hypot(x - ed.startX, y - ed.startY) >= 2) {
          ed.moved = true;
        }
        const kind = ed.drawing.kind;
        if (kind === 'hline') {
          const pt = xToContinuousPoint(x, y, { forceMagnet: useMagnet });
          if (pt) {
            ed.previewPoints = [{ time: ed.origPts[0]?.time ?? Number(pt.time), price: pt.price }];
          }
        } else if (ed.origPts.length >= 2) {
          const part = ed.part;
          if (part === 'p0' || part === 'p1') {
            const ancIdx = part === 'p0' ? 1 : 0;
            const editIdx = part === 'p0' ? 0 : 1;
            const ancOrig = ed.origPts[ancIdx];
            const ancCorner: RulerCorner = {
              time: ancOrig.time as Time,
              price: ancOrig.price,
              x: ancOrig.x,
              y: ancOrig.y,
            };
            const pt = xToContinuousPoint(x, y, {
              forceMagnet: useMagnet,
              shiftAnchor: ancCorner,
              allowAngleLock: e.shiftKey && (kind === 'trend' || kind === 'ray'),
            });
            if (pt) {
              ed.previewPoints[editIdx] = { time: Number(pt.time), price: pt.price };
              magnetRingRef.current = pt.snapped ? { x: pt.x, y: pt.y } : null;
            }
          } else if (part === 'p01' || part === 'p10') {
            const pt = xToContinuousPoint(x, y, { forceMagnet: useMagnet });
            if (pt) {
              const p0 = { ...ed.previewPoints[0] };
              const p1 = { ...ed.previewPoints[1] };
              if (part === 'p01') {
                p0.time = Number(pt.time);
                p1.price = pt.price;
              } else {
                p1.time = Number(pt.time);
                p0.price = pt.price;
              }
              ed.previewPoints = [p0, p1];
              magnetRingRef.current = pt.snapped ? { x: pt.x, y: pt.y } : null;
            }
          } else {
            const dx = x - ed.startX;
            const dy = y - ed.startY;
            const next0 = xToContinuousPoint(ed.origPts[0].x + dx, ed.origPts[0].y + dy, {
              forceMagnet: false,
            });
            const next1 = xToContinuousPoint(ed.origPts[1].x + dx, ed.origPts[1].y + dy, {
              forceMagnet: false,
            });
            if (next0 && next1) {
              ed.previewPoints = [
                { time: Number(next0.time), price: next0.price },
                { time: Number(next1.time), price: next1.price },
              ];
            }
          }
        }
        el.style.cursor = ed.part === 'body' ? 'grabbing' : 'crosshair';
        if (moveRaf) cancelAnimationFrame(moveRaf);
        moveRaf = requestAnimationFrame(() => paintOverlayRef.current());
        e.preventDefault();
        return;
      }

      const mode = drawModeRef.current;
      if (quickRulerActiveRef.current || mode !== 'none') {
        const inside = isInsidePlot(x, y, plotWidth, pane0Height);
        el.style.cursor = inside ? 'crosshair' : '';
        if (!inside) {
          magnetRingRef.current = null;
          hoverPreviewRef.current = null;
          return;
        }
        const allowAngle = e.shiftKey && (mode === 'trend' || mode === 'ray');
        const pt = xToContinuousPoint(x, y, {
          forceMagnet: useMagnet,
          shiftAnchor: rulerCornerRef.current,
          allowAngleLock: allowAngle,
        });
        magnetRingRef.current = pt?.snapped ? { x: pt.x, y: pt.y } : null;
        if (mode === 'hline') {
          hoverPreviewRef.current = pt;
          if (moveRaf) cancelAnimationFrame(moveRaf);
          moveRaf = requestAnimationFrame(() => paintOverlayRef.current());
        } else if (rulerCornerRef.current && pt) {
          rulerPreviewRef.current = pt;
          if (moveRaf) cancelAnimationFrame(moveRaf);
          moveRaf = requestAnimationFrame(() => paintOverlayRef.current());
        } else if (useMagnet) {
          if (moveRaf) cancelAnimationFrame(moveRaf);
          moveRaf = requestAnimationFrame(() => paintOverlayRef.current());
        }
        return;
      }

      if (magnetRingRef.current) {
        magnetRingRef.current = null;
        if (moveRaf) cancelAnimationFrame(moveRaf);
        moveRaf = requestAnimationFrame(() => paintOverlayRef.current());
      }

      if (!isInsidePlot(x, y, plotWidth, pane0Height)) {
        if (hoveredHitRef.current) {
          hoveredHitRef.current = null;
          paintOverlayRef.current();
        }
        if (el.style.cursor !== '') el.style.cursor = '';
        return;
      }

      const cx = getCursorX();
      const nearCursor = cx != null && Math.abs(x - cx) <= 6;
      const nearLine = hitTestPriceLine(y) != null;
      const hit =
        !nearCursor && !nearLine
          ? hitTestDrawingDetailed(
              x,
              y,
              plotWidth,
              drawingsRef.current,
              mapDrawingPoint,
              priceToY,
              selectedDrawingIdRef.current,
            )
          : null;

      const prevHoverId = hoveredHitRef.current?.drawing.id ?? null;
      const nextHoverId = hit?.drawing.id ?? null;
      hoveredHitRef.current = hit;
      if (prevHoverId !== nextHoverId) {
        if (moveRaf) cancelAnimationFrame(moveRaf);
        moveRaf = requestAnimationFrame(() => paintOverlayRef.current());
      }

      const wantCursor = nearCursor
        ? 'ew-resize'
        : nearLine
          ? 'ns-resize'
          : hit
            ? hit.part === 'body'
              ? 'grab'
              : 'pointer'
            : '';
      if (el.style.cursor !== wantCursor) {
        el.style.cursor = wantCursor;
      }
    };

    const onPointerUp = (e: PointerEvent) => {
      const rect = el.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;
      const useMagnet = magnetRef.current !== (e.ctrlKey || e.metaKey);

      if (cursorDragRef.current) {
        cursorDragRef.current = false;
        return;
      }
      const drag = dragLineRef.current;
      if (drag) {
        dragLineRef.current = null;
        paintOverlayRef.current();
        onDragPriceLineRef.current?.(drag.id, drag.price);
        return;
      }

      const ed = editDragRef.current;
      if (ed) {
        editDragRef.current = null;
        magnetRingRef.current = null;
        paintOverlayRef.current();
        if (ed.moved) {
          onUpdateDrawingRef.current?.(ed.drawing.id, ed.previewPoints);
        }
        return;
      }

      if (quickRulerActiveRef.current) {
        quickRulerActiveRef.current = false;
        const start = quickRulerStartRef.current;
        quickRulerStartRef.current = null;
        const pending = rulerCornerRef.current;
        const pt = xToContinuousPoint(x, y, { forceMagnet: useMagnet });
        if (pending && pt && start && Math.hypot(x - start.x, y - start.y) >= 4) {
          onUserDrawingRef.current?.('ruler', [
            { time: Number(pending.time), price: pending.price },
            { time: Number(pt.time), price: pt.price },
          ]);
        }
        rulerCornerRef.current = null;
        rulerPreviewRef.current = null;
        rulerResultRef.current = null;
        magnetRingRef.current = null;
        paintOverlayRef.current();
        return;
      }

      const press = drawPressStartRef.current;
      if (press) {
        drawPressStartRef.current = null;
        const mode = drawModeRef.current;
        const pending = rulerCornerRef.current;
        if (
          mode !== 'none' &&
          mode !== 'hline' &&
          pending &&
          Math.hypot(x - press.x, y - press.y) >= 5
        ) {
          const allowAngle = e.shiftKey && (mode === 'trend' || mode === 'ray');
          const pt =
            xToContinuousPoint(x, y, {
              forceMagnet: useMagnet,
              shiftAnchor: pending,
              allowAngleLock: allowAngle,
            }) ?? rulerPreviewRef.current;
          if (pt) {
            commitTwoPointShape(mode, pending, pt);
          }
        }
      }
    };

    const onContextMenu = (e: MouseEvent) => {
      if (
        drawModeRef.current !== 'none' ||
        rulerCornerRef.current ||
        rulerResultRef.current
      ) {
        e.preventDefault();
        clearTransientRuler();
        setDrawMode('none');
      }
    };

    window.addEventListener('mousedown', onMouseDownCapture, { capture: true });
    window.addEventListener('pointerdown', onPointerDown, { capture: true });
    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
    el.addEventListener('contextmenu', onContextMenu);

    return () => {
      if (moveRaf) cancelAnimationFrame(moveRaf);
      window.removeEventListener('mousedown', onMouseDownCapture, {
        capture: true,
      } as EventListenerOptions);
      window.removeEventListener('pointerdown', onPointerDown, {
        capture: true,
      } as EventListenerOptions);
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', onPointerUp);
      el.removeEventListener('contextmenu', onContextMenu);
    };
  }, [mapDrawingPoint, priceToY, timeToContinuousX, xToContinuousPoint, clearTransientRuler]);

  // Escape exits active drawing/selection
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const active = document.activeElement;
      if (
        active instanceof HTMLInputElement ||
        active instanceof HTMLTextAreaElement ||
        active instanceof HTMLSelectElement ||
        (active instanceof HTMLElement && active.isContentEditable)
      ) {
        return;
      }
      if (e.key === 'Escape') {
        if (
          drawModeRef.current !== 'none' ||
          rulerCornerRef.current ||
          rulerResultRef.current
        ) {
          clearTransientRuler();
          setDrawMode('none');
          return;
        }
        if (selectedDrawingIdRef.current != null) {
          selectedDrawingIdRef.current = null;
          setSelectedDrawingId(null);
          paintOverlayRef.current();
        }
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [clearTransientRuler]);

  const toggleDrawTool = (next: DrawMode) => {
    rulerCornerRef.current = null;
    rulerPreviewRef.current = null;
    drawPressStartRef.current = null;
    hoverPreviewRef.current = null;
    magnetRingRef.current = null;
    rulerResultRef.current = null;
    setRulerHint('');
    setDrawMode((prev) => (prev === next ? 'none' : next));
  };

  const displaySymbol = symbol ? symbol.replace(/:USDT$/, '') : '—';
  const activeToolHint =
    drawMode === 'none'
      ? ''
      : rulerHint ||
        (drawMode === 'hline'
          ? '点击放置水平线 · Esc 取消'
          : drawMode === 'ruler'
            ? '尺子：拖拽或点两点测距 · Esc 取消'
            : TWO_POINT_HINTS[drawMode] || '');

  return (
    <div
      ref={shellRef}
      className="flex min-h-0 flex-1 flex-col overflow-hidden"
      style={{ backgroundColor: 'var(--bg-canvas)' }}
    >
      {/* Toolbar */}
      <div
        className="flex h-[34px] shrink-0 flex-nowrap items-center justify-between gap-2 overflow-hidden border-b px-3"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-hairline)',
        }}
      >
        <div className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden">
          {symbolSlot}
          {headerBadge && !symbolSlot ? (
            <>
              <span
                className="font-mono text-xs font-semibold tracking-tight tabular-nums"
                style={{ color: 'var(--text-primary)' }}
              >
                {displaySymbol}
              </span>
              <span
                className="rounded px-1.5 py-0.5 text-[11px] font-medium"
                style={{
                  backgroundColor: 'var(--bg-elevated)',
                  color: 'var(--text-secondary)',
                }}
              >
                {headerBadge}
              </span>
            </>
          ) : headerBadge ? (
            <span
              className="rounded px-1.5 py-0.5 text-[11px] font-medium"
              style={{
                backgroundColor: 'var(--bg-elevated)',
                color: 'var(--text-secondary)',
              }}
            >
              {headerBadge}
            </span>
          ) : null}

          {showTimeframeTabs ? (
            <div className="flex shrink-0 items-center gap-0.5 rounded p-0.5" style={{ backgroundColor: 'var(--bg-canvas)' }}>
              {availableTimeframes.map((tf) => {
                const active = tf === timeframe;
                return (
                  <button
                    key={tf}
                    type="button"
                    onClick={() => onTimeframeChange?.(tf)}
                    className="rounded px-1.5 py-0.5 font-mono text-[11px] transition-colors tabular-nums"
                    style={{
                      backgroundColor: active ? 'var(--bg-elevated)' : 'transparent',
                      color: active ? 'var(--accent-brand)' : 'var(--text-secondary)',
                    }}
                  >
                    {tf}
                  </button>
                );
              })}
            </div>
          ) : (
            <span
              className="font-mono text-xs tabular-nums"
              style={{ color: 'var(--text-secondary)' }}
            >
              {timeframe}
            </span>
          )}

          {loading ? (
            <span className="shrink-0 whitespace-nowrap text-[11px]" style={{ color: 'var(--text-muted)' }}>
              加载中…
            </span>
          ) : loadingMore ? (
            <span className="shrink-0 whitespace-nowrap text-[11px]" style={{ color: 'var(--text-muted)' }}>
              加载历史…
            </span>
          ) : null}
          {error && bars.length === 0 ? (
            <span
              className="max-w-[220px] truncate text-[11px]"
              style={{ color: 'var(--candle-bear)' }}
              role="alert"
            >
              {error}
            </span>
          ) : null}
          {statusSlot}

          {hideFuture ? (
            <span
              className="shrink-0 whitespace-nowrap rounded px-1.5 py-0.5 font-mono text-[11px] tabular-nums"
              style={{
                backgroundColor: 'var(--bg-elevated)',
                color: 'var(--accent-brand)',
              }}
            >
              藏未来
              {futureCount ? ` ${futureCount.shown}/${futureCount.total}` : ''}
            </span>
          ) : null}

          {/* DOM-direct OHLC legend (zero React re-renders on crosshair hover) */}
          <div
            ref={legendRef}
            className="min-w-0 truncate font-mono text-[11px] tabular-nums"
            style={{ color: 'var(--text-secondary)' }}
          />
        </div>

        {/* Right tools & replay bar */}
        <div className="flex shrink-0 flex-nowrap items-center gap-1">
          {onStepCursor ? (
            <div
              className="mr-1 flex items-center gap-1 border-r pr-2"
              style={{ borderColor: 'var(--border-hairline)' }}
            >
              <button
                type="button"
                title="后退步进 (←)"
                onClick={() => onStepCursor(-1)}
                className="rounded p-1 transition-colors hover:opacity-80"
                style={{ color: 'var(--text-secondary)' }}
              >
                <ChevronLeft className="h-3.5 w-3.5" />
              </button>
              {onTogglePlay ? (
                <button
                  type="button"
                  title={playing ? '暂停回放 (Space)' : '播放回放 (Space)'}
                  onClick={onTogglePlay}
                  className="rounded p-1 transition-colors"
                  style={{
                    backgroundColor: playing ? 'var(--bg-elevated)' : 'transparent',
                    color: playing ? 'var(--accent-brand)' : 'var(--text-primary)',
                  }}
                >
                  {playing ? (
                    <Pause className="h-3.5 w-3.5" />
                  ) : (
                    <Play className="h-3.5 w-3.5" />
                  )}
                </button>
              ) : null}
              <button
                type="button"
                title="前进步进 (→)"
                onClick={() => onStepCursor(1)}
                className="rounded p-1 transition-colors hover:opacity-80"
                style={{ color: 'var(--text-secondary)' }}
              >
                <ChevronRight className="h-3.5 w-3.5" />
              </button>

              {onStepBarsChange ? (
                <select
                  aria-label="单步步进"
                  value={stepBars}
                  onChange={(e) => onStepBarsChange(Number(e.target.value))}
                  className="rounded border px-1.5 py-0.5 font-mono text-[11px] tabular-nums"
                  style={{
                    backgroundColor: 'var(--bg-canvas)',
                    borderColor: 'var(--border-hairline)',
                    color: 'var(--text-secondary)',
                  }}
                >
                  {[1, 5, 15, 60].map((n) => (
                    <option key={n} value={n}>
                      {n}根
                    </option>
                  ))}
                </select>
              ) : null}

              {onPlaySpeedChange ? (
                <select
                  aria-label="回放倍速"
                  value={playSpeed}
                  onChange={(e) => onPlaySpeedChange(Number(e.target.value))}
                  className="rounded border px-1.5 py-0.5 font-mono text-[11px] tabular-nums"
                  style={{
                    backgroundColor: 'var(--bg-canvas)',
                    borderColor: 'var(--border-hairline)',
                    color: 'var(--text-secondary)',
                  }}
                >
                  {[1, 2, 5, 10, 20].map((s) => (
                    <option key={s} value={s}>
                      {s}x
                    </option>
                  ))}
                </select>
              ) : null}
            </div>
          ) : null}

          {onToggleHideFuture ? (
            <button
              type="button"
              onClick={onToggleHideFuture}
              title={hideFuture ? '显示完整 K 线' : 'Hide Future · 零前视隐藏未来'}
              className="flex items-center gap-1 rounded border px-2 py-0.5 text-[11px] font-medium transition-colors"
              style={{
                backgroundColor: hideFuture ? 'var(--bg-elevated)' : 'transparent',
                borderColor: hideFuture ? 'var(--accent-brand)' : 'var(--border-hairline)',
                color: hideFuture ? 'var(--accent-brand)' : 'var(--text-secondary)',
              }}
            >
              {hideFuture ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
              <span>{hideFuture ? '藏未来 开' : '藏未来'}</span>
            </button>
          ) : null}

          <button
            type="button"
            aria-pressed={smcOpen}
            onClick={() => setSmcOpen((v) => !v)}
            title="开关 SMC 市场结构 (BOS/CHoCH · 未回补 Order Block · EQH/EQL)"
            className="rounded border px-1.5 py-0.5 font-mono text-[10px] font-medium transition-colors"
            style={{
              backgroundColor: smcOpen ? 'var(--bg-elevated)' : 'transparent',
              borderColor: smcOpen ? 'var(--accent-brand)' : 'var(--border-hairline)',
              color: smcOpen ? 'var(--accent-brand)' : 'var(--text-secondary)',
            }}
          >
            SMC
          </button>

          <ToolButton
            active={drawMode === 'ruler'}
            title="尺子测距（自动保存，点垃圾桶清空）"
            onClick={() => toggleDrawTool('ruler')}
          >
            <Ruler className="h-3.5 w-3.5" />
          </ToolButton>
          <ToolButton
            active={drawMode === 'hline'}
            title="水平线（可上下拖动，点垃圾桶清空）"
            onClick={() => toggleDrawTool('hline')}
          >
            <Minus className="h-3.5 w-3.5" />
          </ToolButton>
          {drawings.length > 0 && onEraseDrawings ? (
            <ToolButton
              active={false}
              title={`清空全部 ${drawings.length} 条画线与尺子`}
              onClick={() => {
                clearTransientRuler();
                setSelectedDrawingId(null);
                setDrawMode('none');
                onEraseDrawings();
              }}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </ToolButton>
          ) : null}
          {onClosePane ? (
            <ToolButton
              active={false}
              title="关闭对比副图"
              onClick={onClosePane}
            >
              <X className="h-3.5 w-3.5" />
            </ToolButton>
          ) : null}
        </div>
      </div>

      {/* Main Chart + Overlay Canvas */}
      <div className="relative min-h-0 flex-1">
        <div ref={containerRef} className="absolute inset-0" />
        <canvas
          ref={overlayCanvasRef}
          className="pointer-events-none absolute inset-0 h-full w-full"
          style={{ zIndex: 4 }}
          aria-hidden
        />
        {activeToolHint ? (
          <div
            className="pointer-events-none absolute top-2 left-3 z-10 rounded border px-2 py-0.5 text-[11px] whitespace-nowrap"
            style={{
              backgroundColor: withAlpha(theme.bgSurface, 0.9),
              borderColor: 'var(--border-hairline)',
              color: 'var(--accent-brand)',
            }}
          >
            {activeToolHint}
          </div>
        ) : null}
        {pinTooltip ? (
          <div
            className="pointer-events-none absolute bottom-2 left-2 z-10 max-w-md rounded border px-2.5 py-1.5 text-[11px]"
            style={{
              backgroundColor: withAlpha(theme.bgElevated, 0.94),
              borderColor: 'var(--border-hairline)',
              color: 'var(--text-primary)',
            }}
          >
            <div
              className="mb-0.5 font-mono text-[10px] tabular-nums"
              style={{ color: 'var(--text-secondary)' }}
            >
              {fmtDateTimeSec(pinTooltip.timeSec)}
            </div>
            {pinTooltip.items.map((line, idx) => (
              <div key={idx} className="truncate font-mono tabular-nums">
                {line}
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function ToolButton({
  active,
  title,
  onClick,
  children,
}: {
  active: boolean;
  title: string;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      title={title}
      aria-pressed={active}
      onClick={onClick}
      className="rounded border p-1 transition-colors"
      style={{
        backgroundColor: active ? 'var(--bg-elevated)' : 'transparent',
        borderColor: active ? 'var(--accent-brand)' : 'transparent',
        color: active ? 'var(--accent-brand)' : 'var(--text-secondary)',
      }}
    >
      {children}
    </button>
  );
}
