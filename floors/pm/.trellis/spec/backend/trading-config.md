---
scope: backend
---

# PM-Bot Backtest Configuration

> Polymarket 天气市场交易策略、踩坑记录、真实可行性分析。

---

## ⚠️ 核心教训

### 教训1：actual_temps 键类型不匹配 (2026-05-16)

`fetch_actual_temps` 返回 string 键 `'city|date'`，但 `BacktestEngine` 用 tuple `(city, date)` 查找，导致所有交易解析为亏损（0% 胜率）。

```python
# ❌ 错误 (engine.py:369)
actual = self._actual_temps.get((ev.city, ev.target_date))

# ✅ 正确
actual = self._actual_temps.get(f"{ev.city}|{ev.target_date}")
```

**根因**: `fetch_actual_temps` (real_data.py:1313) 用 f-string 生成键：
```python
data[key] = temp  # key = f'{city}|{date_iso}'
```

**症状**: 所有策略 0% 胜率，Sharpe 深度负值。诊断方法：打印 `_actual_temps` 键样例 vs 查找键。

### 教训2：危险 Kelly 参数 (2026-05-14)

STRATEGY_DEFAULTS 曾包含 kelly=0.80, max_single_pct=0.60，导致单笔可下 60% bankroll。

```python
# ❌ 自杀参数（已修复）
"forecast_arb": {"kelly_fraction": 0.80, "max_single_pct": 0.60}

# ✅ 安全参数（当前）
"forecast_arb": {"kelly_fraction": 0.25, "max_single_pct": 0.02}
```

**规则**: 未校准模型的 kelly_fraction 不得超过 0.30。max_single_pct 不得超过 0.05。

### 教训3：P&L 公式方向错误

NO 方向 P&L 公式曾写反，回测显示 +57,000% 回报。

```python
# ❌ 错误
raw_pnl = size * effective_price if not hit else -size * (1.0 - effective_price)
# ✅ 正确
raw_pnl = size * (1.0 - effective_price) if not hit else -size * effective_price
```

### 教训4：用 mid price 成交

Polymarket 订单簿实际是 bid=$0.010/ask=$0.990，mid=$0.500。只有尾部桶 (mid < $0.15) 的价格是准确的。中部桶回测结果完全不可信。

### 教训5：中部桶交易全部负EV

买 YES 付 $0.990 ask，赢了赚 $0.010，输了亏 $0.990。风险回报比 99:1。

### 教训6：edge 定义必须一致

所有策略 edge = `model_prob - market_price`。曾有策略用 `edge = 1.0 - yes_price`，导致 Kelly 重建 p_true = 1.0。

### 教训7：CLOB 历史价格对已结算市场返回空数据

CLOB `/prices-history` 对已结算天气市场返回 0 数据点。回测必须在结算前获取价格，或使用 SQLite 缓存。

```python
# 已结算市场 → 0 数据点
history = await get_price_history(condition_id)  # []

# 解决: enrich_events_with_clob_prices 在结算前缓存
# 或使用 Open-Meteo historical-forecast-api + archive-api 重建
```

### 教训8：bucket_probability_numpy 温度单位处理

Fahrenheit 路径正确使用范围比较，Celsius 路径对 range bucket 使用精确匹配（bug）。

```python
# ❌ Celsius 路径 (weather.py:118) — 精确匹配
count = float(np.sum(truncated == temp_low_c))

# ✅ Fahrenheit 路径 (weather.py:105-110) — 范围比较
count = float(np.sum((truncated_f >= bucket_low_f) & (truncated_f <= bucket_high_f)))
```

**症状**: Celsius range bucket 概率始终为 0.000，导致策略无法正确识别 mispricing。

---

## 当前策略 (4 个)

### forecast_arb — 模型 vs 市场定价偏差

```python
# strategies/forecast_arb.py ForecastArbStrategy.run()
model_prob = bucket_probability_numpy(forecast, b.temp_low_c, b.temp_high_c)
mispricing = model_prob - b.yes_price
if mispricing >= 0.15 and b.yes_price <= 0.30:
    # quarter Kelly sizing, max $2 per position, max 3 recs per event
```

### emos_forecast_arb — EMOS 校准的 forecast_arb

与 forecast_arb 相同，但使用 EMOS 校准概率替代原始集合计数。

```python
# strategies/emos_strategies.py EMOSForecastArbStrategy.run()
model_prob = bucket_probability_emos(forecast, b.temp_low_c, b.temp_high_c, calibrator)
mispricing = model_prob - b.yes_price
if mispricing >= 0.15 and b.yes_price <= 0.30:
    # quarter Kelly sizing, max $2 per position
```

### barbell — ColdMath 风格尾部+中央组合

```python
# strategies/barbell.py BarbellStrategy.run()
# 尾部桶 (80%): price < $0.15, model_prob >= 0.18, edge >= 0.08
# 中央桶 (20%): edge >= 0.20, 支持 YES 和 NO 方向
```

### adaptive_barbell — 动态调整的 barbell

根据市场条件动态调整尾部/中央比例（0.60-0.90）。

### 已删除策略 (2026-05-16 清理)

| 策略 | 删除日期 | 原因 |
|------|----------|------|
| gopfan2 | 2026-05-16 | -2.9% 回报，5% 胜率，159 笔交易全亏 |
| emos_gopfan2 | 2026-05-16 | -1.1% 回报，7.6% 胜率，纯尾部买入不赚钱 |
| smart_wallet | 2026-05-16 | 0 笔交易，无基础设施 |
| adaptive_smart_wallet | 2026-05-16 | 0 笔交易，无基础设施 |

### 早期删除策略

| 策略 | 删除日期 | 原因 |
|------|----------|------|
| neg_risk_field_fade | 2026-05-07 | live fill rate <1% |
| neg_risk_sum | 2026-05-07 | live fill rate <1% |
| truncation_edge | 2026-05-07 | 中部桶负EV |
| ensemble_spread | 2026-05-07 | 总P&L为负 |
| resolution_div | 2026-05-07 | 中部桶负EV |
| laddering | 2026-05-14 | 无 NegRisk，kelly=0.60 |
| tail_no_barbell | 2026-05-14 | 3% edge 太低，kelly=0.60 |
| resolution_delay | 2026-05-14 | 最高风险，kelly=0.80 |
| near_certain_bond | 2026-05-14 | edge 太薄，kelly=0.50 |
| smart_wallet bot | 2026-05-14 | 无交易执行，从未实盘 |

### 已删除模块 (2026-05-16 cleanup)

| 模块 | 行数 | 删除原因 |
|------|------|----------|
| strategies/smart_wallet.py | ~200 | 复制交易策略，无基础设施 |
| core/smart_wallet.py | ~300 | 钱包追踪器，从未实盘 |
| backtest/smart_wallet_backtest.py | ~150 | 专用回测，已删除 |
| cli/wallet_cmd.py | ~100 | 钱包 CLI 命令 |

### 早期删除模块 (2026-05-14 cleanup)

| 模块 | 行数 | 删除原因 |
|------|------|----------|
| core/aggregation.py | 180 | BMA 加权共识概率，无调用方 |
| core/station_bias.py | 140 | 站点偏差学习，未集成到交易流程 |
| core/city_variance.py | 375 | 城市方差过滤，过度设计 |
| core/sources/ | 180 | NWS/METAR 重复实现 |
| backtest/monte_carlo.py | 364 | 假随机数模拟，非真实回测 |
| backtest/snowball_metrics.py | 265 | 心理安慰指标（$100→$10K 里程碑） |
| run_snowball.py | 190 | 演示脚本，合成数据 |
| models/forecast.py | 45 | ConsensusForecast，未使用 |
| shared/ | 88 | 与 core/polymarket.py 重复 |
| tests/ | 10,465 | 47 个文件，0 通过 |

---

## 关键参数

| 参数 | 当前值 | 说明 |
|------|--------|------|
| kelly_fraction | 0.25 | Quarter Kelly |
| max_single_pct | 0.02 | 单笔最大 2% bankroll |
| max_position_usd | $2 | 单笔上限 |
| max_total_pct | 70% | 总暴露（30% 现金储备） |
| yes_max (gopfan2) | 0.15 | 只交易 ≤$0.15 的尾部桶 |
| min_edge (gopfan2) | 0.08 | 最低 8% edge |
| min_mispricing (forecast_arb) | 0.15 | 最低 15% mispricing |
| max_market_price (forecast_arb) | 0.30 | 最高市场价 |
| WARMUP_DAYS | 30 | Station bias EMA warmup (module deleted) |
| GEFS members | 31 | GFS 集合预报成员数 |

---

## 真实回测数据接入 (2026-05-14 研究)

### 数据源架构

| 数据 | 来源 | API | 认证 |
|------|------|-----|------|
| 市场发现 | Gamma API | `gamma-api.polymarket.com/events` | 无需 |
| 历史价格 | CLOB API | `clob.polymarket.com/prices-history` | 无需 |
| 实时订单簿 | CLOB API | `clob.polymarket.com/book` | 无需 |
| 天气预报 (当前) | Open-Meteo | `api.open-meteo.com/v1/forecast` | 无需 |
| 天气预报 (历史) | Open-Meteo | `historical-forecast-api.open-meteo.com` | 无需 |
| 天气预报 (集合) | Open-Meteo | `ensemble-api.open-meteo.com/v1/ensemble` | 无需 |
| 天气观测 (真实) | Open-Meteo | `archive-api.open-meteo.com/v1/archive` | 无需 |

### Polymarket CLOB 历史价格 API

```python
# GET /prices-history
# 参数:
#   market: condition_id (string, required)
#   interval: "1h" | "6h" | "1d" | "1w" | "max"
#   startTs: Unix timestamp (seconds)
#   endTs: Unix timestamp (seconds)
#   fidelity: 数据点数量
# 返回: {"history": [{"t": 1697875200, "p": 0.18}, ...]}
# 无需认证

import httpx

async def get_price_history(condition_id: str, interval: str = "1h") -> list[dict]:
    url = "https://clob.polymarket.com/prices-history"
    params = {"market": condition_id, "interval": interval}
    async with httpx.AsyncClient() as client:
        resp = await client.get(url, params=params)
        return resp.json()["history"]
```

### 批量历史价格 API

```python
# POST /batch-prices-history
# 一次获取多个市场的历史价格
# body: {"markets": ["condition_id_1", "condition_id_2"], "interval": "1d"}
```

### Open-Meteo 历史预报 API (关键!)

这是回测的核心：获取**过去某一天发出的预报**，而非事后观测。

```python
# 历史预报 API — 获取过去某天模型实际发出的预报
# 与 archive API 不同：archive 是观测值，historical-forecast 是预报值
# 来源: open-meteo.com/en/docs/historical-forecast-api
# 数据从 2022 年至今

# 获取 2025-06-15 发出的 NYC 温度预报（含多个 lead time）
url = "https://historical-forecast-api.open-meteo.com/v1/forecast"
params = {
    "latitude": 40.7772,
    "longitude": -73.8726,
    "hourly": "temperature_2m",
    "start_date": "2025-06-15",
    "end_date": "2025-06-16",
    "models": "gfs_seamless",  # 或 ecmwf_ifs025, icon_seamless
    "timezone": "America/New_York",
}
```

### Open-Meteo 历史观测 API (真实值)

```python
# Archive API — 获取实际观测值（用于验证预报准确性）
# 来源: archive-api.open-meteo.com/v1/archive
# 数据从 1940 年至今（2017+ 使用 9km 分辨率）

url = "https://archive-api.open-meteo.com/v1/archive"
params = {
    "latitude": 40.7772,
    "longitude": -73.8726,
    "start_date": "2025-06-15",
    "end_date": "2025-06-15",
    "daily": "temperature_2m_max",  # 日最高温
    "timezone": "America/New_York",
    "models": "era5",  # ERA5 再分析（最准确的观测值）
}
```

### 集合预报 API (概率校准用)

```python
# Ensemble API — 获取 31 个集合成员（用于概率校准）
# 注意: 历史 ensemble 数据只保留 ~24h（免费版）
# 付费版 open-meteo.com/pricing 保留更长历史

url = "https://ensemble-api.open-meteo.com/v1/ensemble"
params = {
    "latitude": 40.7772,
    "longitude": -73.8726,
    "hourly": "temperature_2m",
    "models": "gfs_seamless",  # 31 members
    "past_days": 1,  # 免费版最多 ~1 天历史
    "forecast_days": 7,
}
```

### Gamma API 市场发现

```python
# 获取天气类市场的 condition_id（用于查历史价格）
# 来源: gamma-api.polymarket.com

async def discover_weather_markets() -> list[dict]:
    url = "https://gamma-api.polymarket.com/markets"
    params = {
        "tag": "weather",  # 或 "temperature"
        "active": True,
        "closed": False,
        "limit": 100,
    }
    async with httpx.AsyncClient() as client:
        resp = await client.get(url, params=params)
        return resp.json()
```

### 回测数据流水线 (Best Practice)

参考 polymarket-tmax-lab 的架构：

```
1. 市场发现 → Gamma API 获取天气市场列表
2. 规则解析 → 从 market description 提取城市、日期、温度单位、bucket 定义
3. 历史价格 → CLOB /prices-history 获取每个 bucket 的价格时间序列
4. 历史预报 → Open-Meteo historical-forecast-api 获取过去某天的预报
5. 历史观测 → Open-Meteo archive-api 获取真实温度（验证用）
6. 概率计算 → 用历史预报计算 bucket 概率
7. Edge 计算 → model_prob - market_price
8. 模拟交易 → 按 edge 阈值下单，用真实价格结算
9. P&L 计算 → bucket_hit * (1 - price) - (1 - bucket_hit) * price
```

### ⚠️ 关键陷阱：避免 Look-Ahead Bias

```python
# ❌ 错误：用 resolution-day 的预报计算 edge
forecast = get_forecast(date="2025-06-15")  # 6/15 的预报
price = get_price(date="2025-06-14")  # 6/14 的价格
# → 你在用未来信息交易过去

# ✅ 正确：用 trade-date 的预报计算 edge
forecast = get_forecast(date="2025-06-14")  # 6/14 的预报
price = get_price(date="2025-06-14")  # 6/14 的价格
resolution = get_observation(date="2025-06-15")  # 6/15 的真实温度
```

### EMOS 概率校准 (推荐)

原始 ensemble 欠散 20-40%，需要 EMOS 校准：

```python
# EMOS (Ensemble Model Output Statistics) 校准
# 参考: Gneiting et al. 2005
# 原理: 用历史 (预报, 观测) 对训练线性校准

# 校准后: μ_calibrated = a + b * ensemble_mean
#          σ_calibrated = c + d * ensemble_std
# 其中 a, b, c, d 通过最小化 CRPS 训练

# Python 实现参考:
# - github.com/btrotta-bom/rainforests-paper-code (BOM)
# - github.com/YoungseokOh/polymarket-tmax-lab (pmtmax)
# - IMPROVER library: improver.readthedocs.io/en/latest/
```

### 开源参考实现

| 项目 | 用途 | 链接 |
|------|------|------|
| polymarket-tmax-lab | 完整回测框架（DuckDB+Parquet） | github.com/YoungseokOh/polymarket-tmax-lab |
| PolyWeather | 生产级天气交易栈（含 EMOS） | github.com/yangyuan-zhen/PolyWeather |
| polymarket-backtester | 通用 Polymarket 回测器 | github.com/geckopunk1337/polymarket-backtester |
| IMPROVER | EMOS 校准库 | improver.readthedocs.io |
| rainforests-paper-code | EMOS benchmark 代码 | github.com/btrotta-bom/rainforests-paper-code |

---

## Station Bias 修正系统

EMA (指数移动平均) 追踪每个站点的预报偏差：

```python
bias_c = alpha * error + (1.0 - alpha) * bias_c  # alpha=0.15
```

warmup 期间（30 天）使用 ERA5 冷偏差先验值：

| 站点 | 先验偏差 (°C) |
|------|--------------|
| New York | 0.7 |
| London | 0.8 |
| Tokyo | 0.8 |
| Seoul | 0.9 |
| Shanghai/Beijing | 0.9 |
| Hong Kong | 0.5 |
| Miami | 0.6 |
| Dallas | 1.1 |
| Paris | 0.7 |

---

## 概率模型

### 当前：原始集合计数

```python
# bucket_probability_numpy: 成员落在桶内的比例
prob = sum(1 for m in members if low <= m < high) / len(members)
```

**问题**: GEFS ensemble 欠散 20-40%，概率估计不准确。

### 推荐：EMOS 校准

```python
# 用历史 (预报, 观测) 对训练校准参数
mu_cal = a + b * ensemble_mean  # 偏差校正
sigma_cal = c + d * ensemble_std  # 散布校正
prob = gaussian_cdf(high, mu_cal, sigma_cal) - gaussian_cdf(low, mu_cal, sigma_cal)
```

---

## 回测引擎

### 当前模式

1. **run()** — 合成回测（synthetic_only=True，无真实数据）
2. **run_real()** — 真实 CLOB 价格回测（未实现）

### 费用模型

| 费用 | 值 | 说明 |
|------|-----|------|
| Taker fee | 50bps (capped 1.25%) | v2 规则 |
| Maker fee | 0% | 限价单免费 |
| Slippage | 1% | 默认 |
| Tail price penalty | 5% | $0.01-$0.15 |

### Fill Model

```python
fill_prob_at_best = 0.50
fill_prob_inside = 0.25
fill_prob_tail = 0.10
```

---

## City Variance Filtering

追踪每个城市的预报误差统计（MAE、std、bias），跳过高波动城市。

| Tier | MAE 阈值 | 典型城市 |
|------|----------|----------|
| low  | < 2.0°C | Miami, LA, SF, HK, Jeddah, Lagos, Jakarta |
| medium | 2.0-3.5°C | NYC, London, Tokyo, Seoul, Paris |
| high | > 3.5°C | Chicago |

---

## Staged Entry

时间衰减仓位缩放：>48h skip, 48-24h 30%, 24-8h 60%, <8h full。

---

## 策略配置参数传递

`STRATEGY_DEFAULTS` 在 `get_all_strategies()` 创建策略实例时传入构造函数：

```python
_all_strategies = {
    "forecast_arb": ForecastArbStrategy(**STRATEGY_DEFAULTS.get("forecast_arb", {})),
    "emos_forecast_arb": EMOSForecastArbStrategy(**STRATEGY_DEFAULTS.get("emos_forecast_arb", {})),
    "barbell": BarbellStrategy(**STRATEGY_DEFAULTS.get("barbell", {})),
    "adaptive_barbell": AdaptiveBarbellStrategy(**STRATEGY_DEFAULTS.get("adaptive_barbell", {})),
}
```

**反模式**: 不要在 run() kwargs 注入 config，构造函数参数必须在构造时传入。

---

## 真实回测结果 (2026-05-16)

90 天、15 城市、$2000 bankroll、seed 42、live 模式：

| 策略 | 回报 | Sharpe | 胜率 | 交易数 |
|------|------|--------|------|--------|
| barbell | +0.3% | 0.40 | 4.1% | 148 |
| adaptive_barbell | +0.3% | 0.63 | 2.7% | 148 |
| forecast_arb | -0.6% | -1.02 | 1.2% | 81 |
| emos_forecast_arb | -0.5% | -0.86 | 1.2% | 81 |

**结论**: 策略边际盈利或亏损。真实优势可能需要更好的预报模型或市场微结构优势。

### 外部参考

| 项目 | 胜率 | 利润 | 来源 |
|------|------|------|------|
| AadiXD200/polymarket-weather-bot | 82.5% (52W/11L) | +$806 | GitHub |
| PrintMoneyLab bot | 23% (38W/125L) | +$7 | Blog |
| gopfan2 (wallet) | — | $343K+ | Leaderboard |
| ColdMath (wallet) | — | $120K+ | Leaderboard |
| Hans323 (single trade) | — | $1.11M | Leaderboard |

---

## Common Mistakes

### 1. Dict 键类型不匹配

**症状**: 查找始终返回 None，数据明明存在却找不到。

**原因**: Python dict 键类型敏感，`('city', 'date')` ≠ `'city|date'`。

**修复**: 检查数据生产者和消费者的键格式是否一致。

**预防**: 使用 TypedDict 或 dataclass 定义键类型，避免隐式格式约定。

### 2. 跨模块数据格式不一致

**症状**: 模块 A 输出的数据格式与模块 B 期望的格式不匹配。

**原因**: 没有共享的数据契约，各模块独立定义格式。

**修复**: 在 models/ 中定义共享数据结构，所有模块使用同一类型。

**预防**: 使用类型注解 + mypy 检查，避免 dict[str, Any] 传递。

---

## 未来改进方向

1. **接入真实回测数据** — CLOB /prices-history + Open-Meteo historical-forecast-api
2. **EMOS 概率校准** — 原始 ensemble 欠散，需校准后才能准确计算 edge
3. **Out-of-sample 验证** — 拆分 train/test，验证策略泛化
4. **Reliability diagram** — 检查概率校准质量
5. **gopfan2 NO 方向** — gopfan2 也买 NO（>0.45），当前代码缺失
