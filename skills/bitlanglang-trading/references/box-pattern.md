# 箱体识别算法

## 概述

箱体（Box Pattern）是Bit浪浪交易系统中最核心的技术分析工具。本算法用于自动识别K线图中的箱体结构，判断其成熟度，并检测突破信号。

## 箱体定义

### 基本结构
```
上沿（High）─────────────────────
│                              │
│      箱体内部震荡             │
│                              │
下沿（Low）──────────────────────
```

### 关键要素
1. **上沿**：多次触及的高点区域
2. **下沿**：多次触及的低点区域
3. **二次探顶/底**：箱体成熟的标志
4. **收敛**：波动范围逐渐缩小

## 算法参数

### 可调参数
| 参数 | 默认值 | 说明 |
|------|--------|------|
| `minBars` | 10 | 箱体最少K线数 |
| `maxBars` | 100 | 箱体最多K线数 |
| `touchThresholdPct` | 0.3% | 触及上下沿的容差 |
| `convergenceThreshold` | 0.7 | 收敛度阈值（0-1） |
| `minVolumeDeclinePct` | 0.3 | 成交量下降阈值 |

## 识别流程

### 1. 寻找初始箱体范围
```python
# 在前N根K线中找到最高点和最低点
initial_high = max(highs[:min_bars])
initial_low = min(lows[:min_bars])

# 箱体高度必须合理（1-15%）
height_pct = (initial_high - initial_low) / initial_low * 100
if height_pct < 1 or height_pct > 15:
    return None  # 不是有效箱体
```

### 2. 扩展箱体
```python
# 价格在箱体范围内时继续扩展
for i in range(min_bars, max_bars):
    # 价格突破上沿或下沿则停止
    if highs[i] > box_high * 1.003:  # 0.3%容差
        break
    if lows[i] < box_low * 0.997:
        break
    
    # 计算触及次数
    if abs(highs[i] - box_high) / box_high < touch_threshold:
        touch_high += 1
    if abs(lows[i] - box_low) / box_low < touch_threshold:
        touch_low += 1
```

### 3. 检测二次探顶/底
```python
# 二次探顶：至少2次触及上沿
second_test_high = touch_high >= 2

# 二次探底：至少2次触及下沿
second_test_low = touch_low >= 2
```

### 4. 计算收敛度
```python
# 最近5根K线的波动范围
recent_range = max(highs[-5:]) - min(lows[-5:])

# 初始波动范围
initial_range = box_high - box_low

# 收敛度 = 1 - (当前范围/初始范围)
convergence = 1 - (recent_range / initial_range)
```

### 5. 检测成交量下降
```python
# 早期成交量
early_volume = mean(volumes[:min_bars])

# 最近成交量
recent_volume = mean(volumes[-5:])

# 成交量下降
volume_declining = recent_volume < early_volume * 0.7
```

## 箱体状态

### 状态类型
```python
class BoxStatus:
    FORMING = "forming"       # 箱体形成中
    CONVERGING = "converging" # 收敛中，接近突破
    BREAKOUT_UP = "breakout_up"   # 向上突破
    BREAKOUT_DOWN = "breakout_down" # 向下突破
    INVALID = "invalid"       # 箱体无效
```

### 成熟度判断
```python
# 箱体成熟的条件
is_ready = (second_test_high or second_test_low) and convergence > 0.7
```

## 突破检测

### 向上突破条件
```python
if current_price > box_high * 1.005:  # 价格超过上沿0.5%
    if current_volume > avg_volume * 1.5:  # 成交量放大
        status = BREAKOUT_UP
```

### 向下突破条件
```python
if current_price < box_low * 0.995:  # 价格低于下沿0.5%
    if current_volume > avg_volume * 1.5:  # 成交量放大
        status = BREAKOUT_DOWN
```

## 交易信号

### 箱体突破信号
```
触发条件：
1. 箱体状态 = CONVERGING
2. 价格突破上沿
3. 成交量放大1.5倍以上

进场点：突破确认后
止损位：箱体下沿
目标位：箱体高度的1-2倍
```

### 箱底承接信号
```
触发条件：
1. 箱体状态 = FORMING 或 CONVERGING
2. 价格触及下沿
3. 不创新低
4. 出现拐点K线

进场点：拐点确认后
止损位：箱体最下沿
目标位：箱体上沿
```

## 使用示例

### CLI命令
```bash
# 检测BTC/USDT 15分钟图的箱体
pwatch box-detect --symbol BTC/USDT --timeframe 15m --lookback 100

# 输出示例
# Box Pattern Detected: BTC/USDT
# Timeframe: 15m
# High: 68500.00
# Low: 67200.00
# Height: 1300.00 (1.94%)
# Touch High: 3
# Touch Low: 4
# Second Test High: YES
# Second Test Low: YES
# Convergence: 85%
# Volume Declining: YES
# Status: CONVERGING
# Ready for Breakout: YES
# 
# Trading Signal:
# - Wait for breakout above 68500 with volume
# - Stop loss: 67150 (box low - 0.07%)
# - Target 1: 69800 (box height)
# - Target 2: 71100 (2x box height)
```

### Python API
```python
from pwatch.analysis.box_pattern import BoxDetector

detector = BoxDetector({
    "minBars": 10,
    "maxBars": 100,
    "touchThresholdPct": 0.3,
    "convergenceThreshold": 0.7
})

boxes = detector.detect(
    symbol="BTC/USDT",
    timeframe="15m",
    highs=highs,
    lows=lows,
    closes=closes,
    volumes=volumes,
    timestamps=timestamps
)

for box in boxes:
    print(f"Box: {box.low} - {box.high}, Status: {box.status}")
    if box.is_ready:
        print("Ready for breakout!")
```

## 注意事项

### 假突破处理
1. **等待确认**：突破后等待K线收盘确认
2. **成交量验证**：无量突破多为假突破
3. **回踩确认**：突破后回踩不破才是真突破

### 箱体失效条件
1. **时间过长**：超过maxBars根K线
2. **波动过大**：价格大幅突破边界
3. **结构破坏**：跌破下沿后无法收回

### 优化建议
1. **多时间周期**：结合日线和15分钟线
2. **成交量分析**：突破时成交量必须放大
3. **大盘配合**：与大盘趋势共振

## 参考资料

- [Bit浪浪交易系统完整文档](trading-system.md)
- [支撑阻力位算法](support-resistance.md)
- [市场周期判断](cycle-detection.md)
