---
id: H-001
status: proposed
source: newsliquid 教程(营销文) + 2026-08-09 单机币研究
created: 2026-08-09
---

# H-001: 合约启动三维度确认后 4h 延续

## 原始观点(Source)
newsliquid:"OI 异动 + 主力占比OI>50% + 主力盈利并持有 → 已启动 meme 币还能跟"

## 可验证表述
OI 突增(h1>10%) + 大户持仓多空>55% + 账户/持仓分歧(单机指纹) 之后 4h,
正 excess return 是否显著高于非事件基线?

## 可观测数据
- 数据源: binance fapi (openInterestHist/topLongShortPositionRatio/topLongShortAccountRatio)
- 事件: cmd/kairos-oiscan launch_confirmed
- 对照: kairos-calibrate 非事件 directional baseline(60s snapshot, 剔除事件窗口)

## 测试设计
- 模式: shadow(先只记录)
- 判定: lift_4h > 1 且样本 > 30 才考虑 promote

## 结果
- 样本数: 0(尚未开始 shadow 收集)
- 结论: pending
