# Phase 3b-J 实验报告模板 — Jev 盲判重验（Stage 2 待填实测值）

- **状态**: **TEMPLATE（Stage 1 先行落盘，Jev 盲判实测数值留空待 Stage 2 闸门开启后由评测器自动填充）**
- **分支**: `feat/kairos-phase3bj-jev-blind-judgment`
- **预注册锚点**: `docs/40-research/experiments/PHASE3BJ_PREREG.md`
- **盲判载荷模板版本**: `kairos.phase3bj.blind_judgment_payload.v1` (`spec_sha256 = e77cd51842062da704d61b42030b310adb0de17c7fc2a4923f50740a4c580f1c`)
- **Stage 2 运行清单元数据**: `endpoint_host = <TBD_STAGE2>` | `shuffle_seed = 20261111` | `submitted_order_sha256 = <TBD_STAGE2>`

> 已知局限先行声明（Pre-Registered Training-Window Overlap Caveat）：Jev 的底层训练数据覆盖范围与知识截止时间不可审计；鉴于本评测窗口为 `2025-11-09` 至 `2026-11-09` 的公开永续合约行情，若 Jev 的训练窗与评测窗重叠，任何正向结果须强制降格为「判断一致性证据（Judgment Consistency Evidence）」，不得声称为「独立样本外证据（Independent Out-of-Sample Evidence）」。

## 1. 步骤 A 数据集重演核验（Stage 1 已锁定）

| 数据表 / 指标 | Phase 2b/3a 锚点值 | Stage 1 重演实测值 | 偏差 | 状态 |
|---|---:|---:|---:|---|
| `fact_perp_bars_1m`（硬锚点 `±2%`） | `758,013` | **`758,013`** | `0.000%` | **PASS (`<= 2%`)** |
| `fact_perp_bars_htf` (`4h` + `1d`) | `53,122` (3a) | **`53,122`** | — | OK |
| `fact_perp_volume_24h` | `197,776` (3a) | **`197,776`** | — | OK |
| `fact_perp_funding_8h` | `6,995` | **`6,995`** | — | OK |
| `fact_perp_oi_5m` | `30,157` (3a) | **`28,926`** | — | OK |
| `采样标的数 / PIT 合约总数` | `27 / 528` | **`27 / 528`** | — | OK |

## 2. 主判决总表（Walk-Forward OOS，`τ = 0.60` 写死，P1 / P2 独立判定）

| 总体 | 全样本 IS+OOS `N` | OOS 基线 `N` (`G`) | OOS 基线 Mean `PolicyNetR` | Jev 放行 `N` (`G_oos`) | OOS 放行率 | Jev 放行 Mean (`95% CI`) | OOS `ΔNetR` (vs 同池基线) | 单侧 `p` | 最终判决 |
|---|---:|---:|---:|---:|---:|---|---:|---:|---|
| **P1 (`S3_POOL`)** | `741` | `713` (`38`) | `-0.2460R` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| **P2 (`S1_SET`)** | `147` | `147` (`29`) | `-0.5080R` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |

## 3. 阈值敏感性附报表（`τ ∈ {0.55, 0.60, 0.65}`；`0.55 / 0.65` 只描述不改判）

| 总体 | 阈值 `τ` | 定位 | OOS 放行数 `N` | OOS 放行率 | `G_oos` | OOS Mean `PolicyNetR` | vs 同池 `ΔNetR` | 单侧 `p` |
|---|---:|---|---:|---:|---:|---:|---:|---:|
| **P1 (`S3_POOL`)** | `0.55` | 描述性附报 | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| **P1 (`S3_POOL`)** | **`0.60`** | **主口径（写死）** | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| **P1 (`S3_POOL`)** | `0.65` | 描述性附报 | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| **P2 (`S1_SET`)** | `0.55` | 描述性附报 | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| **P2 (`S1_SET`)** | **`0.60`** | **主口径（写死）** | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| **P2 (`S1_SET`)** | `0.65` | 描述性附报 | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |

## 4. 次要诊断 A：P1 头对头对照（Jev 放行组 vs Phase 3a S3 规则保留组，OOS）

| 对照组（P1 Walk-Forward OOS） | `N` | `G` | Mean `PolicyNetR` | Cluster SE | 95% CI | 头对头 `ΔNetR (Jev - Rule)` | 单侧 `p` |
|---|---:|---:|---:|---:|---|---:|---:|
| `Jev 盲判放行组 (p >= 0.60)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| `S3 固定规则保留组 (C1∧C2∧C3∧C4)` | `14` | `11` | `+0.0516R` | `0.3693` | `[-0.7712R, +0.8744R]` | — | — |

- **2×2 交叉列联（OOS）**: `Jev ∩ Rule` = *TBD* | `Jev \ Rule` = *TBD* | `Rule \ Jev` = *TBD* | `双拒` = *TBD*

## 5. 次要诊断 B：Jev 概率校准（Brier 分数与 5 档可靠性分箱，OOS）

| 总体 | OOS `N` | 基准 `P(NetR > 0)` | Brier Score | `BS_clim` | Brier Skill Score | `ECE` |
|---|---:|---:|---:|---:|---:|---:|
| **P1 (`S3_POOL`)** | `713` | `23.98%` | *TBD* | *TBD* | *TBD* | *TBD* |
| **P2 (`S1_SET`)** | `147` | `17.69%` | *TBD* | *TBD* | *TBD* | *TBD* |

| 概率分箱 | P1 `n` | P1 `mean(p)` | P1 实际 `P(NetR>0)` | P1 Mean `NetR` | P2 `n` | P2 `mean(p)` | P2 实际 `P(NetR>0)` | P2 Mean `NetR` |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `[0.00, 0.40)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| `[0.40, 0.55)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| `[0.55, 0.60)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| `[0.60, 0.70)` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |
| `[0.70, 1.00]` | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* | *TBD* |

## 6. Stage 2 开工闸门清单（Gate Checklist）

1. 主会话提供真实 Jev HTTP 端点并通过环境变量注入 `JEV_BASE_URL`（绝不写入仓库文件）。
2. 主会话在 Stage 2 开工时单独提供 `JEV_API_KEY` 并仅通过进程环境变量注入（日志/异常/缓存/报告全程脱敏）。
3. 核对 `PHASE3BJ_PREREG.md` 附录 A 载荷模板指纹 `spec_sha256` 未发生任何改动。
4. 运行 `uv run python research.py phase3bj --db ../../data/phase2b.duckdb --work-dir ../../data/phase3bj --out-report ../../docs/40-research/experiments/2026-11-11-phase3bj-jev-blind-judgment.md`（不带 `--mock`，自动创建全新 `live` 缓存并与 Stage 1 `mock` 缓存物理隔离）。
