# pm-bot: Polymarket天气策略CLI Bot

## Goal

构建一个Python CLI工具，扫描Polymarket天气市场的温度桶，计算edge，输出交易推荐到终端。Phase 1纯扫描+推荐，不自动下单。验证策略逻辑后再升级半自动/全自动交易能力。

## Requirements

### Phase 1 (MVP — 纯扫描+推荐)

- **市场发现**：通过 Gamma API 标签+关键词双重验证，找到当日有效温度市场
- **桶解析**：正则解析温度桶标题（支持多种格式），°F→°C 自动转换
- **策略引擎**：实现 3 个策略
  - gopfan2 固定规则：价格 <0.15 买 YES / >0.45 买 NO
  - 价格求和套利：所有桶 YES 价求和，Σ≠1 即为 edge
  - 温度阶梯法：Open-Meteo GFS ensemble → 概率分布 → ±1°C 桶组合 edge
- **天气数据**：Open-Meteo GFS ensemble(31成员) 构建概率分布，HRRR 补充美国城市
- **CLI 命令**：
  - `pm-bot scan` — 一次性扫描输出推荐表格
  - `pm-bot markets` — 列出当前天气市场
  - `pm-bot watch` — TUI 持续监控模式(60s 轮询刷新)
  - `pm-bot explain <id>` — 单市场策略推理详情
  - `pm-bot config` — 显示当前配置
- **输出格式**：默认紧凑 Rich 表格，`--verbose` 展开卡片详情
- **城市过滤**：核心 8 城预设 + `--cities` 覆盖 + `--all` 扫全部
- **阈值**：策略独立默认值 + `--edge` flag 全局覆盖
- **缓存**：TTLCache 内存缓存(市场5min / 价格30s / 预报60min / tag 24h)
- **日志**：默认静默+底部 skipped 提示，`--debug` 开启 structlog

### Phase 2 (半自动 — 扫描+确认下单)

- Polymarket CLOB API 集成（py-clob-client-v2）
- `pm-bot trade --confirm` 一键确认下单
- config.toml 策略参数配置
- WebSocket 实时价格推送
- 窄桶买NO + 机场套利策略

### Phase 3 (全自动 — 24/7 量化)

- Kelly 公式仓位管理
- 多源聚合+Edge 计算
- 完全自动化交易+风控
- Telegram/Discord 通知

## Acceptance Criteria

- [ ] `pm-bot markets` 列出当前 Polymarket 天气温度市场
- [ ] `pm-bot scan --strategy gopfan2` 输出符合规则的桶推荐
- [ ] `pm-bot scan --strategy sum_arb` 检测到价格求和 gap
- [ ] `pm-bot scan --strategy ladder` 用 Open-Meteo 预报计算阶梯 edge
- [ ] `pm-bot scan` 默认跑全部策略，输出合并表格
- [ ] `pm-bot scan -v` 展开详细推理过程
- [ ] `pm-bot scan --cities NYC,HK` 只扫指定城市
- [ ] `pm-bot scan --all` 扫描所有有市场的城市
- [ ] `pm-bot scan --edge 0.05` 覆盖默认阈值
- [ ] `pm-bot watch` 进入 TUI 持续监控，60s 刷新
- [ ] `pm-bot explain <id>` 显示单市场详细推理
- [ ] °F 桶标题正确解析并转换为 °C
- [ ] 缓存生效：连续 scan 不重复请求 API
- [ ] `--debug` 输出结构化请求日志

## Definition of Done

- 所有 Acceptance Criteria 通过
- `uv run pm-bot scan` 端到端可用
- 类型检查通过（mypy 或 pyright）
- 无硬编码 API key，环境变量管理
- 错误处理：API 失败不崩，跳过该市场继续

## Technical Approach

### 项目结构

```
pm_bot/
  __init__.py
  __main__.py        # python -m pm_bot 入口
  core/
    __init__.py
    polymarket.py    # Gamma/CLOB API 客户端
    weather.py       # Open-Meteo API 客户端
    cache.py         # TTLCache 封装
    parser.py        # 温度桶标题解析器
  strategies/
    __init__.py
    base.py          # 策略基类/协议
    gopfan2.py       # gopfan2 固定规则
    sum_arb.py       # 价格求和套利
    ladder.py        # 温度阶梯法
  cli/
    __init__.py
    app.py           # Typer app 定义
    scan.py          # scan 命令
    watch.py         # watch TUI 命令
    markets.py       # markets 命令
    explain.py       # explain 命令
    config_cmd.py    # config 命令
    display.py       # Rich 表格/卡片输出
  models/
    __init__.py
    market.py        # WeatherEvent, TemperatureBucket
    forecast.py      # ForecastResult, ProbabilityDist
    recommendation.py # Recommendation, EdgeResult
```

### 数据模型

```python
@dataclass
class WeatherEvent:
    event_id: str
    city: str
    date: str
    airport_code: str | None
    buckets: list[TemperatureBucket]

@dataclass
class TemperatureBucket:
    market_id: str
    temp_low: float   # °C
    temp_high: float   # °C
    yes_price: float
    no_price: float
    volume: float

@dataclass
class Recommendation:
    strategy: str
    event: WeatherEvent
    bucket: TemperatureBucket
    direction: str      # "YES" or "NO"
    edge: float         # 0-1
    reasoning: str
```

### 策略阈值默认值

```python
STRATEGY_DEFAULTS = {
    "gopfan2":  {"yes_max": 0.15, "no_min": 0.45},
    "sum_arb":  {"gap_min": 0.02},
    "ladder":   {"edge_min": 0.08, "spread": 1},
}
```

### 城市预设

```python
DEFAULT_CITIES = ["NYC", "London", "Hong Kong", "Miami", "Dallas", "Atlanta", "Seoul", "Tokyo"]

CITY_COORDS = {
    "NYC": (40.7128, -74.0060),
    "London": (51.5074, -0.1278),
    "Hong Kong": (22.3193, 114.1694),
    "Miami": (25.7617, -80.1918),
    "Dallas": (32.7767, -96.7970),
    "Atlanta": (33.7490, -84.3880),
    "Seoul": (37.5665, 126.9780),
    "Tokyo": (35.6762, 139.6503),
}
```

### 缓存 TTL

```python
CACHE_TTL = {
    "markets": 300,      # 5 min
    "prices": 30,        # 30s
    "forecast": 3600,    # 60 min
    "tags": 86400,       # 24h
}
```

### 依赖

- typer[all] >= 0.12
- rich >= 13.0
- httpx >= 0.27
- pydantic >= 2.0
- python-dotenv >= 1.0
- structlog >= 24.0
- cachetools >= 5.0
- numpy >= 1.26 (概率分布计算)

## Decision (ADR-lite)

**Context**: 需要从零构建 Polymarket 天气市场扫描工具，19 项核心决策通过 grill-me 逐一确认
**Decision**: 分阶段实现，Python monolith，命令式+TUI 混合 CLI，Phase 1 只做扫描+推荐
**Consequences**: Phase 1 不需要 API key 或钱包，降低了起步门槛和风险；但后续升级交易能力需重构认证层

## Out of Scope

- Phase 2/3 的交易功能（自动下单、Kelly 公式、WebSocket）
- 窄桶买NO、机场套利、多源+Edge 策略（Phase 2+）
- Web UI / 移动端
- 多用户/账户系统
- 回测框架（未来可能加）

## Technical Notes

### API 端点

- Gamma: `https://gamma-api.polymarket.com/events?tag=...`
- CLOB: `https://clob.polymarket.com/markets/{id}`
- Open-Meteo: `https://api.open-meteo.com/v1/forecast` (GFS ensemble: `models=gfs_seamless`)
- HRRR: `https://api.open-meteo.com/v1/forecast?models=gfs_hrrre`

### 关键注意事项

- 天气市场必须设 `negRisk=true`，否则下单被拒（Phase 2 用）
- Wunderground API 已废弃，用 METAR/Visual Crossing 替代
- weather tag_id 需动态发现，不能硬编码
- 桶标题格式不统一，需多种正则兜底
- 美国市场可能用 °F，需自动转换

### Research References

- `research/polymarket-weather-markets-api.md` — Polymarket 3层API架构、V2 SDK、市场结构
- `research/weather-data-apis.md` — Open-Meteo/NOAA/METAR 对比
- `research/existing-polymarket-weather-bots.md` — 9个开源bot对比、polymarketweather.com博客
- `research/polymarket-clob-trading.md` — CLOB交易完整参考（Phase 2用）

### Obsidian 笔记

- `~/Documents/Obsidian Vault/PM天气策略汇总/00-09` — 8个策略详细拆解
