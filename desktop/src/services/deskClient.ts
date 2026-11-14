import type {
  AccountSyncResponse,
  BootstrapPayload,
  ChartDrawingItem,
  CreateDrawingRequest,
  MarketBarsResponse,
  ReplayStateResponse,
  StatsPayload,
  Timeframe,
  UpdateDrawingRequest,
  UpdateSettingsRequest,
  WsEvent,
} from '../types/kairos';

export class DeskApiError extends Error {
  status: number;
  code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.name = 'DeskApiError';
    this.status = status;
    this.code = code;
  }
}

export interface DeskClientOptions {
  baseUrl?: string;
  wsUrl?: string;
  token?: string;
}

export class DeskClient {
  private baseUrl: string;
  private wsUrl: string;
  private token: string;

  private ws: WebSocket | null = null;
  private wsListeners = new Set<(evt: WsEvent) => void>();
  private connListeners = new Set<(connected: boolean) => void>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempt = 0;
  private closedByCaller = false;
  private wsConnected = false;
  private activeSubs: Array<{ symbol: string; tfs: Timeframe[] }> = [];

  constructor(opts: DeskClientOptions = {}) {
    this.baseUrl = (opts.baseUrl ?? '').replace(/\/+$/, '');
    this.token = opts.token ?? '';
    this.wsUrl = opts.wsUrl ?? '';
  }

  static async createFromEnvironment(): Promise<DeskClient> {
    if (typeof window !== 'undefined' && window.kairosDesktop) {
      const cfg = await window.kairosDesktop.getConfig();
      if (cfg && cfg.url) {
        return new DeskClient({
          baseUrl: cfg.url,
          wsUrl: cfg.wsUrl,
          token: cfg.token,
        });
      }
    }

    let token = '';
    let baseUrl = '';
    let wsUrl = '';
    if (typeof window !== 'undefined') {
      const params = new URLSearchParams(window.location.search);
      token =
        params.get('token') ??
        window.localStorage.getItem('kairos.desk.token') ??
        '';
      baseUrl =
        params.get('api') ??
        window.localStorage.getItem('kairos.desk.apiUrl') ??
        '';
      wsUrl =
        params.get('ws') ??
        window.localStorage.getItem('kairos.desk.wsUrl') ??
        '';
    }
    return new DeskClient({ baseUrl, wsUrl, token });
  }

  updateCredentials(opts: DeskClientOptions): void {
    if (opts.baseUrl !== undefined) {
      this.baseUrl = opts.baseUrl.replace(/\/+$/, '');
    }
    if (opts.wsUrl !== undefined) {
      this.wsUrl = opts.wsUrl;
    }
    if (opts.token !== undefined) {
      this.token = opts.token;
    }
  }

  getToken(): string {
    return this.token;
  }

  isWsConnected(): boolean {
    return this.wsConnected;
  }

  private buildHttpUrl(
    path: string,
    params?: Record<string, string | number | boolean | undefined | null>,
  ): string {
    const cleanPath = path.startsWith('/') ? path : `/${path}`;
    const full = `${this.baseUrl}${cleanPath}`;
    if (!params) return full;

    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v === undefined || v === null || v === '') continue;
      qs.set(k, String(v));
    }
    const query = qs.toString();
    return query ? `${full}?${query}` : full;
  }

  private resolveWsUrl(): string {
    let target = this.wsUrl;
    if (!target) {
      if (this.baseUrl) {
        target = `${this.baseUrl.replace(/^http/i, 'ws')}/api/v1/ws`;
      } else if (typeof window !== 'undefined') {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        target = `${proto}//${window.location.host}/api/v1/ws`;
      }
    }
    if (!target) return '';
    if (!this.token) return target;
    const sep = target.includes('?') ? '&' : '?';
    return `${target}${sep}token=${encodeURIComponent(this.token)}`;
  }

  private async request<T>(
    path: string,
    init: RequestInit & {
      params?: Record<string, string | number | boolean | undefined | null>;
      skipAuth?: boolean;
      timeoutMs?: number;
    } = {},
  ): Promise<T> {
    const {
      params,
      skipAuth,
      timeoutMs = 20_000,
      headers: initHeaders,
      ...rest
    } = init;
    const headers = new Headers(initHeaders);
    if (!skipAuth && this.token) {
      headers.set('Authorization', `Bearer ${this.token}`);
    }
    if (rest.body !== undefined && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json');
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const res = await fetch(this.buildHttpUrl(path, params), {
        ...rest,
        headers,
        signal: rest.signal ?? controller.signal,
      });
      if (!res.ok) {
        let message = `HTTP ${res.status}`;
        let code: string | undefined;
        try {
          const body = (await res.json()) as {
            error?: string;
            detail?: string;
            message?: string;
            code?: string;
          };
          message = body.error || body.detail || body.message || message;
          code = body.code;
        } catch {
          // Response was not JSON; keep HTTP status fallback.
        }
        throw new DeskApiError(message, res.status, code);
      }
      if (res.status === 204) {
        return undefined as T;
      }
      const text = await res.text();
      if (!text) {
        return undefined as T;
      }
      return JSON.parse(text) as T;
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') {
        throw new DeskApiError('请求超时，请稍后重试', 408, 'REQUEST_TIMEOUT');
      }
      throw err;
    } finally {
      clearTimeout(timer);
    }
  }

  async health(): Promise<{ ok: boolean; ts: number }> {
    return this.request<{ ok: boolean; ts: number }>('/api/v1/health', {
      skipAuth: true,
    });
  }

  async bootstrap(): Promise<BootstrapPayload> {
    const data = await this.request<BootstrapPayload>('/api/v1/bootstrap');
    return {
      ...data,
      status: {
        ...data.status,
        symbols: data.status?.symbols ?? [],
        available_symbols: data.status?.available_symbols ?? data.status?.symbols ?? [],
        gate_health: {
          locked_24h_symbols: data.status?.gate_health?.locked_24h_symbols ?? [],
          active_cooldowns: data.status?.gate_health?.active_cooldowns ?? [],
          consecutive_stops_24h:
            data.status?.gate_health?.consecutive_stops_24h ?? 0,
        },
      },
      tickets: (data.tickets ?? []).map((b) => ({
        ...b,
        annotations: b.annotations ?? [],
        fills: b.fills ?? [],
        ticket: {
          ...b.ticket,
          reasons: b.ticket?.reasons ?? [],
          warnings: b.ticket?.warnings ?? [],
          invalidations: b.ticket?.invalidations ?? [],
        },
      })),
      unplanned_trades: data.unplanned_trades ?? [],
      open_positions: data.open_positions ?? [],
      notices: data.notices ?? [],
      anomalies: data.anomalies ?? [],
    };
  }

  async getMarketBars(params: {
    symbol: string;
    tf: Timeframe;
    limit?: number;
    beforeTs?: number;
  }): Promise<MarketBarsResponse> {
    const res = await this.request<MarketBarsResponse>('/api/v1/market/bars', {
      params: {
        symbol: params.symbol,
        tf: params.tf,
        limit: params.limit,
        before_ts: params.beforeTs,
      },
    });
    return {
      ...res,
      bars: res.bars ?? [],
      metrics: res.metrics ?? [],
    };
  }

  async getReplayState(params: {
    symbol: string;
    cursorTs: number;
    tfs?: Timeframe[];
    limit?: number;
  }): Promise<ReplayStateResponse> {
    const res = await this.request<ReplayStateResponse>('/api/v1/replay/state', {
      params: {
        symbol: params.symbol,
        cursor_ts: params.cursorTs,
        tfs: params.tfs?.join(','),
        limit: params.limit,
      },
    });
    return {
      ...res,
      bars_by_tf: res.bars_by_tf ?? {},
      drawings: res.drawings ?? [],
      anomalies: res.anomalies ?? [],
      notices: res.notices ?? [],
      tickets: res.tickets ?? [],
      fills: res.fills ?? [],
    };
  }

  async getDrawings(symbol: string): Promise<ChartDrawingItem[]> {
    const res = await this.request<
      | ChartDrawingItem[]
      | { drawings?: ChartDrawingItem[]; data?: ChartDrawingItem[] }
    >('/api/v1/drawings', { params: { symbol } });
    if (Array.isArray(res)) return res;
    if (Array.isArray(res.drawings)) return res.drawings;
    if (Array.isArray(res.data)) return res.data;
    return [];
  }

  async createDrawing(req: CreateDrawingRequest): Promise<ChartDrawingItem> {
    return this.request<ChartDrawingItem>('/api/v1/drawings', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async updateDrawing(id: number, req: UpdateDrawingRequest): Promise<ChartDrawingItem> {
    return this.request<ChartDrawingItem>(`/api/v1/drawings/${id}`, {
      method: 'PUT',
      body: JSON.stringify(req),
    });
  }

  async deleteDrawing(id: number): Promise<void> {
    await this.request<void>(`/api/v1/drawings/${id}`, {
      method: 'DELETE',
    });
  }

  async deleteDrawingsForSymbol(symbol: string): Promise<void> {
    await this.request<void>('/api/v1/drawings', {
      method: 'DELETE',
      params: { symbol },
    });
  }

  async syncAccount(): Promise<AccountSyncResponse> {
    return this.request<AccountSyncResponse>('/api/v1/account/sync', {
      method: 'POST',
      timeoutMs: 45_000,
    });
  }

  async getStats(): Promise<StatsPayload> {
    const res = await this.request<StatsPayload>('/api/v1/stats');
    return {
      ...res,
      equity_curve: res.equity_curve ?? [],
      r_distribution: {
        ...res.r_distribution,
        buckets: res.r_distribution?.buckets ?? [],
      },
      gate_compliance: {
        ...res.gate_compliance,
        rejections_by_code: res.gate_compliance?.rejections_by_code ?? {},
      },
      hourly_utc8: res.hourly_utc8 ?? [],
      tag_breakdown: {
        setup_tags: res.tag_breakdown?.setup_tags ?? [],
        error_tags: res.tag_breakdown?.error_tags ?? [],
        grades: res.tag_breakdown?.grades ?? [],
        emotions: res.tag_breakdown?.emotions ?? [],
      },
    };
  }

  async updateSettings(
    req: UpdateSettingsRequest,
  ): Promise<{ ok: boolean }> {
    return this.request<{ ok: boolean }>('/api/v1/settings', {
      method: 'PUT',
      body: JSON.stringify(req),
    });
  }

  async wake(): Promise<{ ok: boolean }> {
    return this.request<{ ok: boolean }>('/api/v1/system/wake', {
      method: 'POST',
      timeoutMs: 45_000,
    });
  }

  connectWs(): void {
    this.closedByCaller = false;
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    const url = this.resolveWsUrl();
    if (!url || typeof WebSocket === 'undefined') return;

    try {
      const socket = new WebSocket(url);
      this.ws = socket;

      socket.onopen = () => {
        this.reconnectAttempt = 0;
        this.setConnected(true);
        if (this.activeSubs.length > 0) {
          this.sendSubscribeMany(this.activeSubs);
        }
      };

      socket.onmessage = (msg) => {
        if (typeof msg.data !== 'string') return;
        try {
          const parsed = JSON.parse(msg.data) as WsEvent;
          for (const cb of this.wsListeners) {
            cb(parsed);
          }
        } catch {
          // Ignore malformed frame.
        }
      };

      socket.onclose = () => {
        if (this.ws === socket) {
          this.ws = null;
        }
        this.setConnected(false);
        if (!this.closedByCaller) {
          this.scheduleReconnect();
        }
      };

      socket.onerror = () => {
        socket.close();
      };
    } catch {
      this.setConnected(false);
      if (!this.closedByCaller) {
        this.scheduleReconnect();
      }
    }
  }

  disconnectWs(): void {
    this.closedByCaller = true;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.setConnected(false);
  }

  subscribe(symbol: string, tfs: Timeframe[]): void {
    this.subscribeMany([{ symbol, tfs }]);
  }

  subscribeMany(subs: Array<{ symbol: string; tfs: Timeframe[] }>): void {
    this.activeSubs = subs.map((s) => ({ symbol: s.symbol, tfs: [...s.tfs] }));
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.sendSubscribeMany(this.activeSubs);
    } else if (!this.closedByCaller && !this.ws) {
      this.connectWs();
    }
  }

  onWsEvent(listener: (evt: WsEvent) => void): () => void {
    this.wsListeners.add(listener);
    return () => {
      this.wsListeners.delete(listener);
    };
  }

  onConnectionChange(listener: (connected: boolean) => void): () => void {
    this.connListeners.add(listener);
    listener(this.wsConnected);
    return () => {
      this.connListeners.delete(listener);
    };
  }

  private setConnected(next: boolean): void {
    if (this.wsConnected === next) return;
    this.wsConnected = next;
    for (const cb of this.connListeners) {
      cb(next);
    }
  }

  private sendSubscribeMany(subs: Array<{ symbol: string; tfs: Timeframe[] }>): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(
      JSON.stringify({
        type: 'subscribe',
        subs,
      }),
    );
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer !== null) return;
    const delayMs = Math.min(10_000, 1_000 * Math.pow(2, this.reconnectAttempt));
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (!this.closedByCaller) {
        this.connectWs();
      }
    }, delayMs);
  }
}
