# Research: Kelly Criterion, Risk Management & Multi-Source Aggregation for Weather Market Trading

- **Query**: Kelly criterion position sizing for prediction markets, automated risk management, multi-source data aggregation, 24/7 daemon patterns, existing open-source prediction market bots
- **Scope**: External + Internal (project code)
- **Date**: 2026-05-04

---

## 1. Kelly Criterion for Prediction Markets

### 1.1 Binary Kelly Formula (Standard Gambling)

The Kelly criterion maximizes the long-term geometric growth rate of bankroll. For a **binary bet** where you either win (gain `b` times your wager) or lose your entire wager:

```
f* = p - (1-p)/b = p - q/b
```

Where:
- `f*` = fraction of bankroll to wager
- `p` = true probability of winning
- `q = 1-p` = probability of losing
- `b` = net odds received (e.g., for a $0.15 YES share paying $1: b = 1/0.15 - 1 = 5.67)

### 1.2 Kelly for Prediction Markets (Binary Outcomes)

In prediction markets, buying YES at price `P_yes` means:
- If YES wins: you receive $1 per share (profit = $1 - P_yes)
- If NO wins: you receive $0 (loss = P_yes)

**Edge calculation:**
```
edge = p_true - P_yes
```

**Kelly fraction for buying YES:**
```
f* = (p_true * (1 - P_yes) - (1 - p_true) * P_yes) / (1 - P_yes)
   = (p_true - P_yes) / (1 - P_yes)
```

**Kelly fraction for buying NO:**
```
f* = ((1 - p_true) * P_no - p_true * (1 - P_no)) / P_no
   = ((1 - p_true) - P_no) / P_no
```

Simplified: `f* = edge / payout_if_correct`

### 1.3 Multi-Outcome Kelly (Temperature Buckets)

For a weather market with N mutually exclusive temperature buckets, the multi-outcome Kelly generalizes:

1. Calculate expected revenue rate for each outcome: `er_i = p_i * (Q_i + 1)` where `Q_i` is the payoff odds
2. Sort outcomes by expected revenue rate (descending)
3. Find the optimal set S* of outcomes to bet on (iteratively add outcomes until marginal revenue < threshold)
4. For each outcome in S*: `f_k = p_k - (1 - sum_S(p_j)) / (1 + sum_S(Q_j))` (Whitaker formula)

**Practical simplification for weather markets:** Since temperature buckets are mutually exclusive (exactly one bucket resolves YES), treat each bucket independently with binary Kelly, then cap total exposure across all buckets.

### 1.4 Fractional Kelly (Half-Kelly, Quarter-Kelly)

**Why full Kelly is too aggressive:**
- Full Kelly maximizes geometric growth but assumes **perfectly known probabilities** — which never exists in practice
- Full Kelly can produce drawdowns of 50%+ with probability >33% (Thorp, 2006)
- With estimation error in `p`, full Kelly can actually *overbet* relative to true optimal, leading to catastrophic losses
- The behavioral cost of large drawdowns (panic, errors) far exceeds the theoretical growth rate reduction

**Fractional Kelly in practice:**
| Fraction | Growth Rate vs Full Kelly | Max Drawdown (approx) | Used By |
|----------|--------------------------|----------------------|---------|
| Full Kelly | 100% | 50%+ likely | Theoretical only |
| 3/4 Kelly | ~89% | ~40% | Aggressive quant funds |
| 1/2 Kelly | ~75% | ~25% | Most professional bettors |
| 1/4 Kelly | ~50% | ~13% | weatherbot.fi, conservative bots |
| 1/6 Kelly | ~38% | ~8% | alteregoeth/weatherbot (0.15x) |

**Documented weather bot usage (from project research):**
- alteregoeth/weatherbot: Fractional Kelly, 0.15x multiplier (very conservative)
- MoonsatProtocol: Kelly criterion with entry threshold 0.15
- suislanchez: Trades when edge > 8%, implied fractional Kelly
- weatherbot.fi: Quarter-Kelly (0.25x) with hard caps
- polymarketweather.com recommendation: 0.15-0.25x fractional Kelly with $50-$100 hard caps per position

### 1.5 Computing Edge and Kelly from YES/NO Prices

**Step-by-step calculation for a temperature bucket:**

```python
def kelly_fraction(p_true: float, yes_price: float, direction: str = "YES", kelly_multiplier: float = 0.25) -> float:
    """Compute fractional Kelly position size.
    
    Args:
        p_true: Model's estimated true probability (0-1)
        yes_price: Current YES share price (0-1)
        direction: "YES" or "NO"
        kelly_multiplier: Fraction of full Kelly to use (0.15-0.25 typical)
    
    Returns:
        Fraction of bankroll to risk on this trade
    """
    if direction == "YES":
        edge = p_true - yes_price
        payout_if_correct = 1.0 - yes_price  # profit per $1 if YES wins
        if edge <= 0:
            return 0.0
        full_kelly = edge / payout_if_correct
    else:  # NO
        no_price = 1.0 - yes_price
        edge = (1.0 - p_true) - no_price
        payout_if_correct = no_price  # profit per $1 if NO wins
        if edge <= 0:
            return 0.0
        full_kelly = edge / payout_if_correct
    
    return full_kelly * kelly_multiplier

def position_size(bankroll: float, p_true: float, yes_price: float, direction: str, kelly_mult: float = 0.25, max_position: float = 50.0) -> float:
    """Compute dollar position size with hard cap."""
    frac = kelly_fraction(p_true, yes_price, direction, kelly_mult)
    size = bankroll * frac
    return min(size, max_position)
```

**Concrete example:**
- Bankroll: $1,000
- GFS ensemble: 28/31 members above 70°F → p_true = 0.90
- YES price for "≥70°F" bucket: $0.82
- Edge: 0.90 - 0.82 = 0.08 (8%)
- Full Kelly: 0.08 / (1 - 0.82) = 0.08 / 0.18 = 44.4% of bankroll
- Quarter Kelly: 44.4% × 0.25 = 11.1% → $111
- With $50 hard cap: min($111, $50) = **$50**

### 1.6 Bankroll Management

**Key principles from existing bots and literature:**

| Control | Value | Source |
|---------|-------|--------|
| Kelly fraction | 0.15-0.25x | polymarketweather.com, weatherbot.fi |
| Hard cap per position | $50-$100 | polymarketweather.com |
| Max total exposure | 30-50% of bankroll | Professional betting |
| Max exposure per city/date | 10% of bankroll | Risk diversification |
| Max concurrent positions | 20-50 | alteregoeth/weatherbot |
| Minimum edge threshold | 8% | polymarketweather.com |
| Spread filter | Skip if spread > $0.03 | alteregoeth/weatherbot |

---

## 2. Automated Risk Management

### 2.1 Position Limits

**Max concurrent positions:**
- alteregoeth/weatherbot: trades up to 20 cities, limits per-city exposure
- weatherbot.fi: 67+ cities but limits total exposure
- Recommended: `max_concurrent_positions = 30`, `max_per_city_date = 2 positions`

**Max exposure per city/date:**
```python
MAX_EXPOSURE_PER_CITY_DATE = 0.10  # 10% of bankroll
MAX_TOTAL_EXPOSURE = 0.40  # 40% of bankroll across all positions
```

### 2.2 Drawdown Circuit Breakers

**From alteregoeth/weatherbot:**
- 20% stop-loss per position
- Trailing stop: moves to breakeven when position gains 20%
- Monitors stops every 10 minutes

**Proposed drawdown circuit breaker system:**
```python
DRAWDOWN_CONFIG = {
    "level_1": {  # Warning
        "threshold": -0.10,  # 10% drawdown from peak
        "action": "reduce_new_sizes",  # Half all new position sizes
        "duration_hours": 4,
    },
    "level_2": {  # Caution
        "threshold": -0.20,  # 20% drawdown from peak
        "action": "pause_new_trades",  # No new positions, manage existing only
        "duration_hours": 8,
    },
    "level_3": {  # Emergency
        "threshold": -0.30,  # 30% drawdown from peak
        "action": "close_all",  # Close all positions immediately
        "duration_hours": 24,
    },
}
```

**Rolling window drawdown:**
```python
def check_drawdown_circuit_breaker(trade_log: list, window_hours: int = 24, threshold: float = -0.15) -> str | None:
    """Check if recent PnL triggers a circuit breaker."""
    recent = [t for t in trade_log if t.timestamp > now - timedelta(hours=window_hours)]
    recent_pnl = sum(t.realized_pnl for t in recent)
    drawdown_pct = recent_pnl / current_bankroll
    if drawdown_pct <= -0.30:
        return "close_all"
    elif drawdown_pct <= -0.20:
        return "pause_new_trades"
    elif drawdown_pct <= -0.10:
        return "reduce_new_sizes"
    return None
```

### 2.3 Time-Based Risk

**Near-resolution position management:**
- MoonsatProtocol: `MIN_HOURS_TO_RESOLUTION=2` — won't enter positions <2 hours to resolution
- As resolution approaches, forecast uncertainty drops but market prices converge, reducing edge
- **Last-day trading risk:** Resolution sources (Wunderground) may differ from forecast models, creating basis risk

**Proposed time rules:**
```python
TIME_RISK_CONFIG = {
    "min_hours_to_resolution": 3,      # Don't enter if <3h to resolution
    "close_positions_before_resolution_hours": 1,  # Auto-close 1h before
    "avoid_last_day_entry": True,       # Don't enter on resolution day
    "reduce_position_near_resolution": True,  # Scale down position as resolution nears
}
```

**Position scaling near resolution:**
```python
def time_decay_factor(hours_to_resolution: float, max_hours: float = 48.0) -> float:
    """Scale position size based on time to resolution."""
    if hours_to_resolution < 3:
        return 0.0  # Don't enter
    elif hours_to_resolution < 12:
        return 0.5  # Half size
    elif hours_to_resolution < 24:
        return 0.75  # 75% size
    else:
        return 1.0  # Full size
```

### 2.4 Slippage Modeling

**How existing bots handle slippage:**
- alteregoeth/weatherbot: Skip markets with spread > $0.03
- weather-edge (santox422): Liquidity gating — orderbook depth checks before execution
- General approach: use mid price for edge calculation, then adjust for actual execution price

**Slippage model:**
```python
def estimate_fill_price(side: str, price: float, size: float, orderbook: dict) -> float | None:
    """Estimate actual fill price given orderbook depth.
    
    Returns None if insufficient liquidity.
    """
    slippage_bps = 0  # basis points
    remaining = size
    book = orderbook["bids"] if side == "sell" else orderbook["asks"]
    
    for level in book:
        if remaining <= 0:
            break
        fill_qty = min(remaining, level["size"])
        slippage_bps += fill_qty * abs(level["price"] - price)
        remaining -= fill_qty
    
    if remaining > 0:
        return None  # Insufficient liquidity
    
    if side == "buy":
        return price + slippage_bps / size
    else:
        return price - slippage_bps / size

SLIPPAGE_CONFIG = {
    "max_spread": 0.03,       # Skip if bid-ask spread > $0.03
    "max_slippage_pct": 0.02, # Skip if estimated slippage > 2%
    "min_liquidity": 50.0,    # Skip if orderbook depth < $50
}
```

### 2.5 Order Management

**Auto-cancel stale orders:**
```python
STALE_ORDER_CONFIG = {
    "max_age_minutes": 30,       # Cancel orders older than 30 min
    "requote_if_edge_exists": True,  # Re-submit at better price if edge still present
    "cancel_on_edge_reversal": True,  # Cancel if market price moved against us
}
```

**Exit management (from weatherbot.fi 5-layer system):**
1. **Profit target**: Close at +50% gain
2. **Edge convergence**: Close when market price converges to our forecast (edge → 0)
3. **Trailing stop**: Lock in profits, trail by 20%
4. **Stop loss**: Close at -20% loss
5. **Time decay**: Auto-close 1h before resolution

---

## 3. Multi-Source Data Aggregation

### 3.1 Available Weather Data Sources Beyond Open-Meteo

| Source | API | Auth | Coverage | Resolution | Update Frequency | Free Tier |
|--------|-----|------|----------|------------|-----------------|-----------|
| **Open-Meteo** | REST | None | Global | 2-25km | 1-12h | Yes, generous |
| **NOAA/NWS API** | REST (api.weather.gov) | User-Agent header | US only | ~2.5km | Hourly | Yes, rate-limited |
| **Aviation Weather (METAR)** | REST/FTP | None | Airport stations | Point | Hourly/semi-hourly | Yes |
| **Visual Crossing** | REST | API key | Global | Varies | Hourly | Free tier (1K calls/day) |
| **AccuWeather** | REST | API key | Global | Varies | Hourly | Limited free tier |
| **WeatherAPI.com** | REST | API key | Global | Varies | Hourly | Free tier |
| **Pirate Weather** | REST | API key | Global | Dark Sky compatible | Hourly | Free tier |

**Open-Meteo multi-model access (all free, no auth):**
| Model | Origin | Resolution | Forecast Length | Members (Ensemble) |
|-------|--------|-----------|----------------|-------------------|
| ECMWF IFS HRES | EU | 9km | 15 days | — |
| ECMWF IFS Ensemble | EU | 25km | 15 days | 51 |
| GFS Global | US | 11-25km | 16 days | — |
| GFS Ensemble | US | 25km | 10 days | 31 |
| HRRR | US | 3km | 48h | — |
| ICON-EU-EPS | DE | 13km | 5 days | 40 |
| ICON-D2-EPS | DE | 2km | 2 days | 20 |
| UKMO MOGREPS-G | UK | 20km | 8 days | 18 |

**NOAA/NWS API details:**
- Endpoint: `https://api.weather.gov`
- Format: GeoJSON / JSON-LD
- Grid: ~2.5km × 2.5km per WFO
- Provides: 12h forecast periods, hourly forecasts, observations
- Rate limit: Generous but undisclosed; ~5-second cooldown if exceeded
- Authentication: User-Agent header required
- Key endpoints:
  - `/points/{lat},{lon}` → grid coordinates, forecast URLs
  - `/gridpoints/{wfo}/{x},{y}/forecast` → 12h period forecast
  - `/gridpoints/{wfo}/{x},{y}/forecast/hourly` → hourly forecast
  - `/stations/{id}/observations` → METAR observations

### 3.2 Weighting Multiple Forecasts

**Inverse-Error Weighting:**
```python
def inverse_error_weight(forecasts: list[ForecastResult]) -> ForecastResult:
    """Weight forecasts inversely by their historical RMSE.
    
    Weight w_i = 1 / RMSE_i^2
    Normalized: w_i = w_i / sum(w)
    """
    weights = [1.0 / (f.rmse ** 2) for f in forecasts]
    total_w = sum(weights)
    norm_weights = [w / total_w for w in weights]
    
    # Weighted average of mean temperatures
    combined_mean = sum(w * f.mean for w, f in zip(norm_weights, forecasts))
    combined_std = math.sqrt(sum(w * f.std**2 for w, f in zip(norm_weights, forecasts)))
    
    return ForecastResult(mean=combined_mean, std=combined_std, weights=norm_weights)
```

**Bayesian Model Averaging (BMA):**
```python
def bma_weight(forecasts: list[ForecastResult], observations: list[float]) -> ForecastResult:
    """Bayesian Model Averaging — weight models by posterior probability.
    
    BMA weight for model k: w_k = p(M_k | D) ∝ p(D | M_k) * p(M_k)
    
    Where p(D | M_k) is the marginal likelihood (how well model k 
    explains observed data), and p(M_k) is the prior.
    
    In practice, use training period to estimate weights:
    - Fit a normal distribution to each model's errors vs observations
    - Weight by log-likelihood on recent observation window
    """
    # Simplified: use recent accuracy as posterior proxy
    weights = []
    for f in forecasts:
        log_likelihood = sum(norm.logpdf(obs, f.mean, f.std) for obs in observations[-30:])
        weights.append(math.exp(log_likelihood))
    
    total_w = sum(weights)
    norm_weights = [w / total_w for w in weights]
    
    combined_mean = sum(w * f.mean for w, f in zip(norm_weights, forecasts))
    # BMA variance includes between-model uncertainty
    combined_var = sum(w * (f.std**2 + (f.mean - combined_mean)**2) 
                       for w, f in zip(norm_weights, forecasts))
    
    return ForecastResult(mean=combined_mean, std=math.sqrt(combined_var), weights=norm_weights)
```

**Ensemble counting (simplified, from suislanchez bot):**
```python
def ensemble_count_probability(members: list[float], low: float, high: float, spread_mult: float = 1.15) -> float:
    """Count ensemble members falling in bucket.
    
    Apply 1.15x spread multiplier for ensemble underdispersion
    (documented in polymarketweather.com).
    """
    count = sum(1 for m in members if low <= m <= high)
    raw_prob = count / len(members)
    # Apply underdispersion correction
    adjusted_prob = raw_prob * spread_mult
    return min(adjusted_prob, 1.0)  # Cap at 100%
```

### 3.3 Multi-Source Edge Calculation

**When multiple sources agree → higher confidence:**
```python
def compute_multi_source_edge(sources: list[ForecastResult], yes_price: float, low: float, high: float) -> tuple[float, float]:
    """Compute edge with confidence based on source agreement.
    
    Returns: (edge, confidence_multiplier)
    """
    probs = []
    for s in sources:
        # Probability that temperature falls in [low, high]
        p = norm.cdf(high, s.mean, s.std) - norm.cdf(low, s.mean, s.std)
        probs.append(p)
    
    # Check agreement: low standard deviation of probs = high agreement
    prob_std = statistics.stdev(probs) if len(probs) > 1 else 0
    
    # BMA-weighted probability
    combined_prob = bma_weight(sources, low, high)  # returns probability
    
    # Confidence multiplier: scale down edge when sources disagree
    # If all sources agree within 5%, full confidence (1.0)
    # If sources disagree by 20%, half confidence (0.5)
    confidence = max(0.3, 1.0 - prob_std * 5)
    
    edge = (combined_prob - yes_price) * confidence
    return edge, confidence
```

### 3.4 Source Disagreement as Risk Signal

**Key insight:** When forecast models disagree significantly, the uncertainty is genuine and should reduce position size.

**Disagreement detection:**
```python
def detect_source_disagreement(sources: list[ForecastResult], threshold_c: float = 2.0) -> bool:
    """Flag when models disagree by more than threshold_c °C."""
    means = [s.mean for s in sources]
    spread = max(means) - min(means)
    return spread > threshold_c

# Action: if disagreement detected, reduce kelly_multiplier by 50%
# and widen the probability distribution (increase std by disagreement spread)
```

**Documented patterns from existing bots:**
- weather-edge (santox422): "Multi-source confirmation required" before entering
- weatherbot.fi: 4-model ensemble (GFS, ECMWF, UKMO, NWS) with Bayesian edge detection
- alteregoeth/weatherbot: Self-calibration — learns per-city accuracy over time
- polymarketweather.com: "Ensemble underdispersion" requires 1.15x spread multiplier

---

## 4. 24/7 Daemon Patterns

### 4.1 Python Daemon/Scheduler Approaches

**Option A: APScheduler 4.x (Recommended)**
- AsyncScheduler for asyncio-native code
- Persistent data stores (SQLite, PostgreSQL via SQLAlchemy)
- Triggers: Interval, Cron, Date, CalendarInterval, Or, And
- Event brokers for distributed coordination
- Built-in misfire handling, job coalescing

```python
from apscheduler import AsyncScheduler
from apscheduler.triggers.interval import IntervalTrigger
from apscheduler.triggers.cron import CronTrigger

async def main():
    async with AsyncScheduler() as scheduler:
        # Full market scan every hour
        scheduler.add_schedule(
            scan_all_markets,
            IntervalTrigger(hours=1),
            id="market_scan",
            conflict_policy="replace",
        )
        
        # Position monitoring every 10 minutes
        scheduler.add_schedule(
            monitor_positions,
            IntervalTrigger(minutes=10),
            id="position_monitor",
        )
        
        # Align with ECMWF model runs (00/12 UTC)
        scheduler.add_schedule(
            ecmwf_update_scan,
            CronTrigger(hour="0,12", minute="30"),  # 30 min after model run
            id="ecmwf_scan",
        )
        
        await scheduler.run_until_stopped()

asyncio.run(main())
```

**Option B: Pure asyncio Loop**
```python
import asyncio
import signal

class TradingDaemon:
    def __init__(self):
        self.running = True
        self.positions = {}
        
    async def run(self):
        loop = asyncio.get_event_loop()
        
        # Register graceful shutdown
        for sig in (signal.SIGTERM, signal.SIGINT):
            loop.add_signal_handler(sig, self.graceful_shutdown)
        
        # Schedule recurring tasks
        tasks = [
            asyncio.create_task(self.market_scan_loop(interval=3600)),
            asyncio.create_task(self.position_monitor_loop(interval=600)),
            asyncio.create_task(self.ecmwf_watcher()),
            asyncio.create_task(self.health_check_loop(interval=300)),
        ]
        
        await asyncio.gather(*tasks)
    
    async def market_scan_loop(self, interval: int):
        while self.running:
            try:
                await self.scan_all_markets()
            except Exception as e:
                log.error("market_scan_failed", error=str(e))
            await asyncio.sleep(interval)
    
    def graceful_shutdown(self):
        log.info("shutdown_initiated")
        self.running = False
        # Cancel all open orders
        asyncio.create_task(self.cancel_all_orders())
        # Close all positions (optional based on config)
        # asyncio.create_task(self.close_all_positions())
```

**Option C: Cron + State File**
- Simplest approach: run via cron, persist state in SQLite
- Each run: load state → scan → trade → save state → exit
- No long-running process to manage
- Downside: no real-time position monitoring between runs

### 4.2 Health Checks and Self-Monitoring

```python
HEALTH_CHECK_CONFIG = {
    "heartbeat_interval_seconds": 300,  # 5 min
    "max_scan_duration_seconds": 120,   # Alert if scan takes >2 min
    "max_order_age_seconds": 1800,      # Alert if order unfilled >30 min
    "stale_data_threshold_seconds": 3600,  # Alert if forecast data >1h old
}

class HealthMonitor:
    def __init__(self):
        self.last_scan_time = None
        self.last_trade_time = None
        self.errors_since_last_scan = 0
        
    async def check(self) -> dict:
        """Return health status."""
        issues = []
        now = time.time()
        
        if self.last_scan_time and (now - self.last_scan_time) > 7200:
            issues.append("scan_overdue")
        
        if self.errors_since_last_scan > 5:
            issues.append("excessive_errors")
        
        # Check API connectivity
        for api_name, check_fn in self.api_checks.items():
            try:
                await check_fn()
            except Exception:
                issues.append(f"{api_name}_unreachable")
        
        return {
            "status": "unhealthy" if issues else "healthy",
            "issues": issues,
            "uptime": now - self.start_time,
            "positions_open": len(self.positions),
            "bankroll": self.bankroll,
            "last_scan": self.last_scan_time,
        }
```

### 4.3 Graceful Shutdown

```python
import signal

class GracefulShutdown:
    def __init__(self, trader):
        self.trader = trader
        self.shutting_down = False
        
    def setup(self):
        loop = asyncio.get_event_loop()
        for sig in (signal.SIGTERM, signal.SIGINT):
            loop.add_signal_handler(sig, self.handle_signal)
    
    def handle_signal(self):
        if self.shutting_down:
            return  # Already handling
        self.shutting_down = True
        log.info("graceful_shutdown_signal_received")
        asyncio.create_task(self._shutdown())
    
    async def _shutdown(self):
        """Cancel all orders, optionally close positions, save state."""
        # 1. Cancel all open orders
        await self.trader.cancel_all_orders()
        log.info("all_orders_cancelled")
        
        # 2. Optionally close positions (configurable)
        if self.trader.config.close_on_shutdown:
            await self.trader.close_all_positions()
            log.info("all_positions_closed")
        
        # 3. Save final state to SQLite
        await self.trader.save_state()
        log.info("state_saved")
        
        # 4. Stop the event loop
        asyncio.get_event_loop().stop()
```

### 4.4 State Persistence (SQLite)

```sql
-- Trade log
CREATE TABLE trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    market_id TEXT NOT NULL,
    condition_id TEXT NOT NULL,
    direction TEXT NOT NULL,  -- YES/NO
    entry_price REAL NOT NULL,
    quantity REAL NOT NULL,
    cost REAL NOT NULL,
    strategy TEXT NOT NULL,
    edge REAL NOT NULL,
    kelly_fraction REAL NOT NULL,
    forecast_source TEXT NOT NULL,
    forecast_prob REAL NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',  -- open/closed/expired
    exit_price REAL,
    exit_timestamp TEXT,
    realized_pnl REAL,
    stop_loss_price REAL,
    take_profit_price REAL
);

-- Position tracking
CREATE TABLE positions (
    market_id TEXT PRIMARY KEY,
    condition_id TEXT NOT NULL,
    direction TEXT NOT NULL,
    entry_price REAL NOT NULL,
    quantity REAL NOT NULL,
    current_value REAL,
    unrealized_pnl REAL,
    last_updated TEXT NOT NULL
);

-- Forecast snapshots (for calibration)
CREATE TABLE forecasts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    market_id TEXT NOT NULL,
    source TEXT NOT NULL,  -- ecmwf, gfs, hrrr, nws, etc.
    predicted_temp_c REAL,
    predicted_prob REAL,
    actual_temp_c REAL,  -- Filled after resolution
    error_c REAL  -- Filled after resolution
);

-- Bankroll history
CREATE TABLE bankroll (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    balance REAL NOT NULL,
    total_exposure REAL NOT NULL,
    unrealized_pnl REAL NOT NULL,
    drawdown_from_peak REAL NOT NULL
);

-- Daemon state (for crash recovery)
CREATE TABLE daemon_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

---

## 5. Existing Open-Source Prediction Market Bots

### 5.1 Summary Table

| Bot | Language | Kelly | Risk Controls | Forecast Sources | Live Trading |
|-----|----------|-------|---------------|-----------------|-------------|
| alteregoeth/weatherbot | Python | ✓ Fractional (0.15x) | Stop-loss, trailing stop, slippage filter | ECMWF+HRRR+METAR | Yes |
| MoonsatProtocol | TypeScript | ✓ | Entry/exit thresholds, min resolution time | NWS | Yes |
| solship | TypeScript | ✓ | Same as MoonsatProtocol | NWS | Yes |
| suislanchez | TypeScript/Python | ✓ | Edge >8% threshold | GFS ensemble | Yes (Kalshi+PM) |
| weather-edge | Python/TS | ✓ + Black-Litterman | Multi-source confirm, liquidity gating | 5-stage pipeline | Partial |
| openclaw/simmer | Python | Position caps | Min edge, hedge unwinds, sizing caps | Simmer API | Planned |
| polymarket-tmax-lab | Python | Edge computation | Research-first, live disabled | Open-Meteo | Disabled |
| weatherbot.fi | Node.js | ✓ Quarter-Kelly | 5-layer exit, hard caps | GFS+ECMWF+UKMO+NWS | Yes (commercial) |

### 5.2 Key Risk Controls Implemented Across Bots

1. **Kelly-based position sizing** — Every serious bot uses some form of fractional Kelly
2. **Hard caps on position size** — $50-$100 max per position is the industry standard
3. **Minimum edge threshold** — 8% is the consensus; 5% is too low for weather markets
4. **Stop-loss management** — 20% stop-loss with trailing stop at breakeven
5. **Slippage/spread filter** — Skip markets with bid-ask spread > $0.03
6. **Time-to-resolution filter** — Don't enter within 2-3 hours of resolution
7. **Ensemble underdispersion correction** — Apply 1.15x spread multiplier to raw ensemble counts
8. **Self-calibration** — Track forecast accuracy per city, adjust probabilities

### 5.3 Lessons from Bot Designs

**What works:**
- Multi-model ensembles consistently outperform single models
- Fractional Kelly (0.15-0.25x) with hard caps is the sweet spot for weather markets
- ECMWF 12 UTC forecast latency is the #1 documented edge (prices move after model updates)
- Airport station vs city-center temperature difference creates systematic mispricing
- The "volume game" — edge only shows up reliably at hundreds/thousands of positions

**What fails:**
- Full Kelly leads to catastrophic drawdowns with estimation error
- Single-model forecasts are too overconfident (underdispersion)
- Last-day trading introduces resolution basis risk (Wunderground ≠ forecast model)
- Thin liquidity markets make position sizing meaningless (can't fill orders)

---

## Related Project Code

| File | Description |
|------|-------------|
| `pm_bot/strategies/base.py` | Strategy base class + Gopfan2Strategy, SumArbStrategy, LadderStrategy |
| `pm_bot/strategies/airport_arb.py` | Airport station arbitrage strategy |
| `pm_bot/strategies/narrow_no.py` | Narrow bucket NO-buying strategy |
| `pm_bot/models/market.py` | WeatherEvent, TemperatureBucket, Recommendation, ForecastResult |
| `.trellis/tasks/00-bootstrap-guidelines/research/existing-polymarket-weather-bots.md` | Detailed comparison of 9 open-source weather bots |
| `.trellis/tasks/05-04-pm-bot/prd.md` | Phase 2 PRD with project structure and data models |

## Not Found

- **No BMA implementation found in any open-source weather bot** — most use simple ensemble counting or single-model approaches
- **No formal drawdown circuit breaker system** in any open-source bot — existing bots use simple stop-loss
- **No order management/requote system** in open-source bots — most submit limit orders and forget
- **Polymarket CLOB WebSocket API docs** — not publicly documented; weatherbot.fi uses it but details are proprietary
- **No open-source Python APScheduler + Polymarket integration** — each bot rolls its own scheduling
