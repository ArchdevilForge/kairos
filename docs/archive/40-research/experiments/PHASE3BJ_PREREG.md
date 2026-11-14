# Phase 3b-J 预注册（Pre-Registration）— Jev 盲判重验（Stage 1 脚手架与预注册锚点）

> **状态**：Pre-Registered Audit Anchor（作为 `feat/kairos-phase3bj-jev-blind-judgment` 分支首个工作提交先行落盘；在任何评测代码与结果计算之前冻结）
> **分支**：`feat/kairos-phase3bj-jev-blind-judgment`
> **基线提交**：`eabef72`（`main`，含 Phase 2c/2d/3a 全部产物，PR #11 已合并）
> **前置权威锚点**：
> - `docs/40-research/experiments/PHASE3A_PREREG.md`（Phase 3a 预注册，Rev 2）
> - `docs/40-research/experiments/2026-11-11-phase3a-bitlanglang-summary.md`（Phase 3a 总结论报告）
> - `docs/40-research/experiments/2026-11-11-phase3a-s3-point2-constraints.md`（S3 点位 2 约束集报告）
> - `docs/40-research/experiments/2026-11-11-phase3a-s1-point1-breakout.md`（S1 点位 1 突破报告）
> - `docs/40-research/experiments/2026-11-11-phase2c-winter-defense-advisory.md`（Phase 2c/2d 因果四季分类器口径）
> - `tools/research/PHASE2B_PREREG.md`（Phase 2b 12 个月 40 切片 27 标的 PIT 数据集锚点）
> **范围红线**：仅限 `tools/research/` 与 `docs/40-research/`，零触碰 `internal/` 及任何生产链路代码。

---

## 1. 背景与唯一待验假设

### 1.1 背景
Phase 3a（PR #11）将 Bit浪浪体系的点位、过滤与排序构件全部按因果操作化验证：**S1 = `KILLED`**、**S2 / S3 / F / L = `FAIL`（或 `FAIL (underpowered)`）**、**T = `UNTESTABLE`** —— 固定机械规则无法在该主观体系上跨越样本外全摩擦门槛。仓库所有者判断潜在边（Edge）位于**概率判断层**而非固定阈值规则层，决定以自有概率判断模型 **Jev**（原生任务：估计一条信息带来的概率跃升，单次推理约 `~80ms`）作为候选票据盲判评审器，在同一批历史候选票上重验。

### 1.2 唯一待验假设（Single Pre-Registered Hypothesis）
**在同一批 Phase 3a 历史候选票上，Jev 在严格盲判协议（仅见 `<= entry_ts` 事前上下文、剥离构造来源与顺序信息、零未来/结果泄漏）下给出的放行子集（`p >= 0.60`），能否在 Walk-Forward OOS 上跨越与 Phase 3a 完全同一道统计门槛，而固定规则未能跨越。**

- 主总体 **P1** 与次总体 **P2** **分别独立判定**。
- **严禁合并 P1 与 P2 样本凑样本量、凑簇数或凑显著性**。
- **严禁挑子时间窗、挑子币种、事后修改阈值 $\tau$ 或用任何其他模型（Gemini / 通用 LLM / 规则变体）冒充 Jev**。

---

## 2. 数据集重演锚点与结果标尺（100% 沿用 Phase 2b / Phase 3a）

### 2.1 步骤 0 数据集重演硬门槛
- 复用 `tools/research/p0_experiment.py:ingest_phase2b_sample_to_duckdb` 与 `tools/research/phase3a_experiment.py:verify_phase3a_dataset_anchor` 按 `PHASE2B_PREREG.md` 原参数重演同一数据集：
  - **时间跨度**：`2025-11-09T00:00:00Z`（`1762646400`）至 `2026-11-09T23:59:00Z`（`1794268740`），连续 `366` 个日历日。
  - **切片与标的**：`40` 个等距连续 `1m` 切片（前 39 个各 `15h = 900` 根 `1m`，间距 `~9.15 天 >> 24h`；第 40 个为末尾 `24h`）+ 全窗口连续 `4h` / `1d` HTF K 线（`fact_perp_bars_htf`）+ 因果滚动 24h 成交额（`fact_perp_volume_24h`）+ `8h` 资金费率（`fact_perp_funding_8h`）+ `5m` OI（`fact_perp_oi_5m`），覆盖 `PHASE2B_27_SWAP_INST_IDS` 共 `27` 个 PIT 永续合约。
  - **硬锚点停机纪律**：重演落库后 `fact_perp_bars_1m` 行数须落在 **`758,013 ± 2%`**（即闭区间 **`[742,853, 773,173]`**）之内；一旦超差立即停机报告，严禁带病往下跑。

### 2.2 标准结果模型（Kairos `PolicyNetR` 引擎）
与 Phase 1 P0、Phase 2b、Phase 3a 保持 100% 同一引擎（`tools/research/policies.py:simulate_holding_policy`，参数 `policy_kind="wave"`, `k_be=0.8`, `max_hold_minutes=1440`）：
- 1m K 线内同时触及止损与保本/结构目标位时，严格按 **`stop-first`（最坏路径优先）** 结算；
- 含双边手续费缓冲的保本止损在 `+0.8R` 确认分钟收盘后，于**下一分钟开盘**生效；
- 15m / 1h / 4h 波段抬高低点（HL）移动止盈仅使用 `close_ts <= bar.open_ts` 的已收盘 K 线；
- 全额扣除双边 Taker `5 bps`、基础滑点（BTC/ETH 主流 `1.5 bps` / 山寨 `5.0 bps`）、平方根市场冲击滑点 `0.5 * (ATR_1m / P) * sqrt(V / ADV_5m)`（单笔名义本金 `$10,000`）及持仓跨越的 `8h` 资金费率。

### 2.3 波次聚类与 Walk-Forward OOS 切分（与 Phase 3a 逐行一致）
- **波次聚类与方差估计**：统一使用 `tools/research/clustering.py` 的 `< 24h`（`86,400s`）合并规则生成 `wave_cluster_id`，并用 `cluster_robust_mean` / `cluster_robust_diff` 计算 `CR1` 聚类稳健标准误、自由度 `df = G - 1` 的双侧 95% Student-$t$ 置信区间与单侧 `student_t_one_sided_p`。
- **Walk-Forward OOS 主口径切分**（完全复刻 `tools/research/phase3a_experiment.py`）：
  - **P1（S3 池）**：沿用 `phase3a_experiment.py:1550-1551`（`all_cids = sorted({a.ticket.signal.wave_cluster_id for a in annotations})`，首个波次簇 `all_cids[0]` 为预热簇，`oos_cids = set(all_cids[1:])` 为 Walk-Forward OOS 评测集）。
  - **P2（S1 集）**：沿用 `phase3a_experiment.py:1159-1214`（在 `baseline_tickets + s1_raw` 统一 `<24h` 波次聚类时间轴上取 `all_cids = sorted(...)`，首个波次簇 `all_cids[0]` 为预热簇，`oos_cids = set(all_cids[1:])` 为 Walk-Forward OOS 评测集）。
  - IS 全样本指标仅作诊断透明展示；**所有 PASS / FAIL / KILLED 判决唯一绑定上述 Walk-Forward OOS 口径**。

---

## 3. 候选总体定义（逐条指回 Phase 3a，零新造定义）与调用预算规则

### 3.1 两个独立候选总体
1. **主总体 P1 = S3 的全候选池（`source_pool = "S3_POOL"`）**：
   - **权威溯源**：`docs/40-research/experiments/PHASE3A_PREREG.md` §2.3 & §3.3；`docs/40-research/experiments/2026-11-11-phase3a-s3-point2-constraints.md` 第 20–25 行（`全票流 ALL_PULLBACK`）；代码入口 `tools/research/phase3a_experiment.py:evaluate_s3_point2_constraints`（第 `1366` 行 `[t for t in baseline_tickets if t.signal.direction == "UP"]`，其中 `baseline_tickets` 来自 `h1_experiment.py:run_h1_experiment(..., min_quote_vol_24h=30_000_000.0)`）。
   - **基准规模锚点**：Phase 3a 实测全样本 IS+OOS 共 `741` 张（`G = 39`），Walk-Forward OOS 段共 `713` 张（`G = 38`，OOS 基线均值 `-0.2460R`）。
   - **同池现成规则对照组**：Phase 3a S3 约束集保留组 `RETAINED`（满足 `C1 ∧ C2 ∧ C3 ∧ C4` 的子集，Phase 3a OOS 为 `14` 张、`G_oos = 11`、均值 `+0.0516R`、对全池 `ΔNetR = +0.2975R`、`p = 0.1844` -> `FAIL (underpowered, G_oos=11)`）。
2. **次总体 P2 = S1 的全部候选票（`source_pool = "S1_SET"`）**：
   - **权威溯源**：`docs/40-research/experiments/PHASE3A_PREREG.md` §3.1；`docs/40-research/experiments/2026-11-11-phase3a-s1-point1-breakout.md` 第 24–25 行；代码入口 `tools/research/phase3a_experiment.py:detect_s1_point1_candidates`（`min_quote_vol_24h=10_000_000.0`）。
   - **基准规模锚点**：Phase 3a 实测全样本 IS+OOS 共 `147` 张（`G = 29`），Walk-Forward OOS 段共 `147` 张（`G_oos = 29`，OOS 全候选均值 `-0.5080R`，Phase 3a 判决 `KILLED`）。

### 3.2 总调用量上限与超限确定性取舍规则
- **硬上限**：Stage 2 对真实 Jev 端点的非缓存 HTTP 调用总数 **`MAX_TOTAL_JEV_CALLS = 5,000`** 次。
- 在标准 Phase 2b/3a 数据集下，`P1 + P2` 全样本（IS + OOS）合计约 `741 + 147 = 888` 张候选票，远低于 `5,000` 次上限，正常应全量覆盖。
- **预先写死的超限取舍规则（严禁人工挑票）**：若待调用未缓存候选总数超过 `5,000`（或通过 CLI 显式指定更低配额上限时），在随机打乱提交顺序**之前**，严格按以下 4 级确定性优先级截断至上限：
  1. **优先级 1**：`P1`（`S3_POOL`）位于 Walk-Forward **OOS** 窗口内的全部候选，内部按 `(entry_ts ASC, symbol ASC, candidate_id ASC)` 排序；
  2. **优先级 2**：`P2`（`S1_SET`）位于 Walk-Forward **OOS** 窗口内的全部候选，内部按 `(entry_ts ASC, symbol ASC, candidate_id ASC)` 排序；
  3. **优先级 3**：`P1`（`S3_POOL`）位于预热簇 **IS-only**（非 OOS）窗口内的剩余候选，按 `(entry_ts ASC, symbol ASC, candidate_id ASC)` 排序；
  4. **优先级 4**：`P2`（`S1_SET`）位于预热簇 **IS-only**（非 OOS）窗口内的剩余候选，按 `(entry_ts ASC, symbol ASC, candidate_id ASC)` 排序。
  截断集合确定后，再使用预登记随机种子打乱提交给 Jev 的请求顺序（见 §6.3）。

---

## 4. 预注册判决门槛、阈值 $\tau$ 与次要诊断（写死，一字不改）

### 4.1 Jev 输出定义与放行阈值 $\tau$
- **Jev 输出概率定义**：
  $$p_i = \mathbb{P}\!\left(\text{PolicyNetR}_i > 0 \;\middle|\; \mathcal{I}_{i,\, t \le \text{entry\_ts}}\right) \in [0, 1]$$
  即候选票 $i$ 在 §2.2 所述 kairos 标准全摩擦结果模型（`wave_k_be=0.8`）下实现净收益 `PolicyNetR > 0` 的事前概率估计。
- **主口径接受阈值（写死）**：**$\tau = 0.60$** —— 当且仅当 Jev 返回 $p_i \ge 0.60$ 时该票归入 **Jev 盲判放行子集（`JEV_ACCEPTED`）**；$p_i < 0.60$ 归入 **Jev 盲判拒绝子集（`JEV_REJECTED`）**。
- **敏感性附报阈值（只描述、不改判）**：同时附报 $\tau \in \{0.55,\; 0.65\}$ 下的放行率、均值与 $\Delta\text{NetR}$；**严禁根据 $\tau = 0.55$ 或 $\tau = 0.65$ 的表现修改主判决结论**。

### 4.2 主判决条件（P1 与 P2 分别独立套用，与 Phase 3a 完全同一道门槛）
对任一总体 $k \in \{\text{P1}, \text{P2}\}$：设 $B_k$ 为该总体在 Walk-Forward OOS 窗口内的**全候选基线集合**，$A_k(\tau) = \{i \in B_k : p_i \ge \tau\}$ 为 $\tau = 0.60$ 下的 **Jev 盲判放行子集**。采用 `cluster_robust_diff(A_k(0.60), B_k)` 计算聚类稳健均值差：

- **`PASS`**：当且仅当以下三条在 $\tau = 0.60$ 的 Walk-Forward OOS 上**同时满足**：
  1. **效应量门槛**：$\Delta\text{NetR}_{\text{oos}} = \text{Mean}(A_k) - \text{Mean}(B_k) \ge \mathbf{+0.18\text{R}}$；
  2. **统计显著性门槛**：按 `wave_cluster_id` 聚类的 `CR1` 单侧检验 $p < \mathbf{0.05}$；
  3. **独立波次簇功效门槛**：放行子集覆盖的样本外独立波次簇数 $G_{\text{oos}}(A_k) \ge \mathbf{15}$。
- **`FAIL (underpowered, G_oos=...)`**：未达 `PASS` 且 $G_{\text{oos}}(A_k) < 15$（含放行数为 0 的退化情形，据实报告 $N_{\text{oos}}(A_k)$ 与 $G_{\text{oos}}(A_k)$）。
- **`KILLED`**：未达 `PASS`、$G_{\text{oos}}(A_k) \ge 15$，且放行子集 $A_k(0.60)$ 自身 OOS `PolicyNetR` 的 95% 聚类稳健置信区间上界 $< 0.0000\text{R}$（与 `phase3a_experiment.py:1302-1307` 口径完全对齐）。
- **`FAIL`**：其余一切未达 `PASS` 的情形。

| 评测总体 | 待检集合（主口径 $\tau=0.60$） | 主口径对照基线 | PASS 联合硬门槛（Walk-Forward OOS） |
|---|---|---|---|
| **P1（S3 全候选池）** | `P1_JEV_ACCEPTED_OOS`（$p \ge 0.60$） | `P1_ALL_OOS`（S3 OOS 全候选基线） | $\Delta\text{NetR} \ge +0.18\text{R}$ **且** 单侧 $p < 0.05$ **且** $G_{\text{oos}} \ge 15$ |
| **P2（S1 全候选票）** | `P2_JEV_ACCEPTED_OOS`（$p \ge 0.60$） | `P2_ALL_OOS`（S1 OOS 全候选基线） | $\Delta\text{NetR} \ge +0.18\text{R}$ **且** 单侧 $p < 0.05$ **且** $G_{\text{oos}} \ge 15$ |

### 4.3 次要诊断指标（只描述、不改判）
以下指标全部写入评测报表，但仅用于机制诊断，不论数值优劣均不得推翻 §4.2 的主判决：
1. **Jev 放行组 vs 规则保留组头对头对照（仅 P1）**：
   - 在 P1 的 Walk-Forward OOS 上，用 `cluster_robust_diff` 直接对比 `P1_JEV_ACCEPTED_OOS`（$p \ge 0.60$）与 Phase 3a 固定规则保留组 `P1_S3_RULE_RETAINED_OOS`（`C1 ∧ C2 ∧ C3 ∧ C4`）：报告双方 $N$、$G$、Mean `PolicyNetR`、95% CI、头对头 $\Delta\text{NetR}$、Cluster SE 与单侧 $p$；
   - 附列 $2 \times 2$ 交叉列联表（`Jev ∩ Rule`、`Jev \ Rule`、`Rule \ Jev`、`Neither` 四格各自的票数与均值 `PolicyNetR`）。
2. **Jev 概率校准诊断（P1 与 P2 分别在 OOS 与全样本上报告）**：
   - 定义二元真实结果指示变量 $y_i = \mathbb{I}(\text{PolicyNetR}_i > 0) \in \{0, 1\}$（严格对应 $p_i = \mathbb{P}(\text{NetR} > 0)$）；
   - **Brier 分数（Brier Score）**：$\text{BS} = \frac{1}{N}\sum_{i=1}^{N}(p_i - y_i)^2$，同时报告样本基准胜率 $\bar{y}$ 对应的气候学无技巧基准 $\text{BS}_{\text{clim}} = \bar{y}(1 - \bar{y})$ 及 Brier Skill Score $\text{BSS} = 1 - \text{BS}/\text{BS}_{\text{clim}}$；
   - **可靠性分箱（Reliability Bins）与 ECE**：固定 5 个概率分箱 $[0.00, 0.40)$、$[0.40, 0.55)$、$[0.55, 0.60)$、$[0.60, 0.70)$、$[0.70, 1.00]$，逐箱报告票数 $n_b$、平均预测概率 $\bar{p}_b$、实际正净值率 $\bar{y}_b = \mathbb{P}(\text{NetR} > 0)$、箱内平均 `PolicyNetR`，以及加权期望校准误差 $\text{ECE} = \sum_b \frac{n_b}{N}|\bar{p}_b - \bar{y}_b|$。
3. **放行率与附报参照**：
   - 报告 P1 / P2 在 OOS 与 IS 下的放行率 $|A_k(\tau)| / |B_k|$（主口径 $\tau = 0.60$，附报 $\tau \in \{0.55, 0.65\}$）；
   - 在 P1 / P2 报表中附列 Jev 拒绝子集 `JEV_REJECTED_OOS`（$p < 0.60$）均值与 95% CI，以及 P2 放行子集对比 Phase 3a 基线多头票流（`Baseline_Long_OOS`）的参考列。

---

## 5. 已知局限先行声明（Pre-Declared Training-Window Overlap Caveat）

> **强制声明（必须原样保留于预注册与 Stage 2 最终实验报告）**：
> Jev 系仓库所有者自有的外部概率判断模型，其底层训练语料与知识截止时间（Training Cutoff）在本研究层**不可审计**。鉴于本实验评测窗口为 `2025-11-09` 至 `2026-11-09` 的公开永续合约历史行情，**若 Jev 的训练数据窗口与本评测窗口存在任何重叠（或包含同期加密市场价格/叙事记忆），则即使统计指标跨越 §4.2 门槛，研究结论也必须强制降格表述为「判断一致性证据（Judgment Consistency Evidence）」，而不得声称为「独立样本外预测证据（Independent Out-of-Sample Evidence）」**。

---

## 6. 盲判协议（§D 全文规范）

### 6.1 事前上下文快照纪律（Ex-Ante Context Snapshot Discipline）
1. **零前视硬约束**：每张候选票在进场时刻 `entry_ts`（触发 5m K 线收盘秒级时间戳）生成一行 JSON 快照。所有特征必须仅使用收盘时间 `<= entry_ts` 的历史数据计算，记录最大特征时间戳 `max_feature_ts` 并逐特征通过 `clustering.py:validate_ex_ante_grouping(..., entry_ts=float(entry_ts))` 校验。
2. **严禁出现的字段（连字段名都不许出现在快照 JSON 中）**：
   - **进场后结果与路径字段**：`net_r`、`net_r_wave`、`net_r_bracket`、`net_r_30m`、`gross_r`、`gross_r_wave`、`realized_r`、`mechanical_r`、`pnl`、`mfe`、`mae`、`exit_ts`、`exit_price`、`exit_reason`、`closed_at`、`path_end_unix`、`hold_seconds`、`hold_seconds_wave`、`holding_duration`、`stop_hit`、`stop_hit_first`、`target_hit_first`、`future_bars`、`win`、`outcome` 等一切依赖 `t > entry_ts` 的量；
   - **构件判决与规则 Pass/Fail 标志**：`verdict`、`retained`、`rejected`、`is_filtered`、`pass`、`passed`、`fail`、`failed`、`c1_ok`、`c2_ok`、`c3_ok`、`c4_ok`、`c1_undertake_no_breakout`、`c2_div_count_le_2`、`c3_htf_main_uptrend`、`c4_overhead_space`、`is_box_mid_chop`、`is_box_internal_rebound`、`is_box_counter_short`、`is_top2_langlang`、`is_top2_current`、`setup_code`、`sub_template`、`wave_cluster_id`、`is_oos` 等一切暴露规则筛选结论或样本切分归属的字段。
3. **跨总体特征同构性**：P1（`S3_POOL`）与 P2（`S1_SET`）候选快照采用**完全相同的字段集合与计算函数**（统一输出：交易几何参数、标的年龄与流动性档位、`CycleMap` 三层状态、箱体连续观测状态、分歧连续观测状态、BTC 状态、Phase 2c 因果四季状态、同板块与全截面共振计数）。快照中唯一区分来源的字段是顶层分组键 `source_pool`（取值 `"S3_POOL"` 或 `"S1_SET"`），且该字段在构造 Jev 请求载荷时被强制剥离。
4. **不透明候选标识（Opaque `candidate_id`）**：
   - 定义 `candidate_id = "cand_" + sha256(f"{source_pool}|{symbol}|{side}|{entry_ts}|{entry_price:.8f}|{stop_price:.8f}").hexdigest()[:16]`；
   - 不含任何自增序号（如 `0001`、`0002`），不含任何来源前缀（如 `s1_`、`s3_`、`mp_`），且 `candidate_id` 仅用于本地 JSONL 缓存对齐与评测合并，**不进入呈现给 Jev 的判断内容**。
5. **同板块共振计数的静态无前视分类口径**：
   - 鉴于 Phase 3a 温度计审计已确认 DuckDB 无动态叙事板块表，对 `PHASE2B_27_SWAP_INST_IDS` 预登记唯一静态、仅依赖币种代码的板块分桶映射 `STATIC_SECTOR_BUCKET_BY_COIN`：
     - `MAJORS_PAYMENT`：`BTC`, `ETH`, `XRP`, `LTC`, `BCH`
     - `ALT_SMART_L1`：`SOL`, `SUI`, `AVAX`, `ADA`, `DOT`, `NEAR`
     - `DEFI_INFRA`：`LINK`, `UNI`, `AAVE`, `LIT`
     - `MEME`：`DOGE`
     - `EMERGING_2025_2026`：`PIEVERSE`, `ZAMA`, `EDGE`, `UP`, `GRAM`, `CAP`, `VVV`, `AKE`, `PONS`, `DGAI`, `CT`
   - 在时刻 `entry_ts`，仅对当时满足 PIT 上市未退市且存在完整收盘 `5m` K 线的其他合约（`peer != symbol`），统计同分桶内当根 `5m` 同向涨幅 `> +0.06%`（与 MarketPulse `r5 > 0.06%` 噪声阈值一致）的共振标的数 `same_sector_resonance_count_5m`（附分桶活跃同伴数 `same_sector_active_peers`）以及全 27 标的池共振标的数 `cross_section_resonance_count_5m`。

### 6.2 盲判载荷剥离规则（Blind Payload Stripping Rules）
由 `tools/research/jev_client.py:build_jev_payload(snapshot)` 将单行事前快照转换为发送给 Jev 的判断载荷，强制执行：
1. **剥离分组与索引元数据**：剔除 `source_pool`、`candidate_id`、`max_feature_ts` 及任何文件行号/批次序号；
2. **零提示词偏向**：固定说明目标事件为 `PolicyNetR > 0` 的客观结算规则与事前上下文观测值，不包含任何暗示该票「已通过某条点位规则」或「来自回撤池/突破池」的措辞；
3. **递归安全断言**：在发出任何请求前对载荷做键名黑名单递归扫描，若出现 `source_pool`、`candidate_id`、`FORBIDDEN_EX_POST_COLUMNS` 或任何规则判决键名立即抛出异常阻断调用。

### 6.3 客户端提交、随机序、缓存与密钥脱敏规范（`tools/research/jev_client.py`）
1. **环境变量只读配置**：仅从进程环境变量读取 `JEV_BASE_URL` 与 `JEV_API_KEY`；仓库代码、配置文件、运行清单、缓存 JSONL、评测报告、测试样例与异常堆栈/日志中**绝不许出现真实 API key 或完整 `Authorization` 请求头**（内置脱敏过滤器自动将任何疑似 Bearer token / key 值替换为 `[REDACTED]`）。
2. **无状态单票调用与随机提交序**：
   - 每张候选票独立发起一次无会话历史（stateless）HTTP POST 请求，严禁批量拼单或跨票共享上下文窗口；
   - 待提交候选列表按确定性伪随机数生成器 `random.Random(shuffle_seed)` 打乱顺序后逐票提交，`shuffle_seed`（缺省固定为 `20261111`）连同载荷模板 SHA-256 指纹一并写入运行清单（`manifest.json`）。
3. **超时、重试与礼貌限速**：单次请求缺省超时 `10.0s`，网络瞬断或 `HTTP 429 / 5xx` 指数退避重试最多 `3` 次，相邻请求间强制最小礼貌间隔（缺省 `min_interval_sec = 0.12s`）。
4. **按 `candidate_id` 幂等落盘缓存**：每完成一票判断立即以追加模式（JSONL flush）将 `{candidate_id, p_netr_gt_0, mode, payload_sha256, latency_ms, cached_at_utc}` 落盘；重跑时自动加载既有缓存跳过已判 `candidate_id`，不重复计费/调用。
5. **Mock 模式与 Stage 2 闸门**：
   - Stage 1 仅运行 `--mock`（按 `HMAC-SHA256("kairos-phase3bj-mock-v1", candidate_id)` 确定性映射至 $[0.25, 0.85]$ 伪概率，`mode = "mock"`），用于端到端验证「快照生成 → 盲判载荷 → 客户端缓存 → 统计评测与报表渲染」全链路；
   - **Stage 2 闸门铁律**：在主会话正式交付真实 `JEV_BASE_URL` 与 `JEV_API_KEY` 之前，严禁使用任何其他模型或规则变体生成伪造的 `"live"` 判断缓存；评测器若检测到缓存含 `"mock"` 记录，必须在报告顶部醒目标注 **`DRY-RUN (MOCK)`**，严禁写入正式实验结论报告。

---

## 附录 A：Jev 盲判请求载荷模板（全文写死；Stage 2 不许改模板，改模板 = 新预注册）

### A.1 模板版本与指纹约定
- **模板标识符（`TEMPLATE_ID`）**：`kairos.phase3bj.blind_judgment_payload.v1`
- `build_jev_payload(snapshot)` 输出如下固定结构的 JSON 对象（所有浮点数按固定小数位四舍五入，JSON 序列化使用 `sort_keys=False` 固定字段顺序；其中 `prompt` 字段由同一 `context` 字典按下方 §A.3 固定文本模板确定性渲染，供文本输入或结构化 JSON 输入端点无损消费，Stage 2 严禁改动任何措辞或字段）：

### A.2 结构化请求载荷 Schema（精确固定字段集）
```json
{
  "schema_version": "kairos.phase3bj.blind_judgment_payload.v1",
  "task": "estimate_probability_net_r_gt_zero",
  "target_definition": {
    "event": "PolicyNetR > 0",
    "output_field": "p",
    "output_range": [0.0, 1.0],
    "settlement_model": "kairos wave_k_be=0.8 full-friction 1m stop-first holding policy: max_hold=1440m; +0.8R fee-inclusive breakeven at next 1m open; causal 15m/1h/4h higher-low structure trail; bilateral 5bps taker fee + 1.5bps(major)/5.0bps(alt) base slippage + 0.5*(ATR_1m/P)*sqrt($10000/ADV_5m) impact slippage + realized 8h funding"
  },
  "context": {
    "symbol": "<PERP_SYMBOL e.g. SOL/USDT:USDT>",
    "side": "<LONG|SHORT>",
    "entry_ts": 1762690500,
    "entry_iso_utc": "2025-11-09T12:15:00Z",
    "trade_geometry": {
      "entry_price": 0.0,
      "stop_price": 0.0,
      "stop_distance_pct": 0.0,
      "overhead_room_r": 0.0,
      "atr_1m_pct": 0.0,
      "adv_5m_usd": 0.0
    },
    "symbol_profile": {
      "listing_age_days": 0.0,
      "age_tier": "<NEW_LT_7D|CIXIN_7D_90D|ESTABLISHED_90D_365D|MATURE_GT_365D>",
      "quote_vol_24h_usd": 0.0,
      "liquidity_tier": "<TIER_1_ULTRA_GT_500M|TIER_2_HIGH_100M_500M|TIER_3_CORE_30M_100M|TIER_4_THIN_10M_30M|TIER_5_ILLIQUID_LT_10M>"
    },
    "cycle_map": {
      "primary_direction": "<up|down|neutral>",
      "alignment": "<full_alignment|trend_pullback|counter_trend|mixed|no_trade>",
      "trade_class": "<aligned_long|aligned_short|counter_trend_long|counter_trend_short|no_trade>",
      "context_1d": {"direction": "<up|down|neutral>", "phase": "<spring|summer|autumn|winter>", "confidence": 0.0},
      "context_4h": {"direction": "<up|down|neutral>", "phase": "<spring|summer|autumn|winter>", "confidence": 0.0},
      "setup_15m": {"direction": "<up|down|neutral>", "phase": "<spring|summer|autumn|winter>", "confidence": 0.0},
      "trigger_5m": {"direction": "<up|down|neutral>", "phase": "<spring|summer|autumn|winter>", "confidence": 0.0}
    },
    "box_state": {
      "box_5d_width_pct": 0.0,
      "pos_in_5d_box": 0.0,
      "er_4h_48h": 0.0,
      "htf_platform_width_pct": 0.0,
      "upper_probe_count": 0,
      "lower_probe_count": 0,
      "atr_4h_convergence_ratio": 0.0,
      "atr_5m_convergence_ratio": 0.0
    },
    "divergence_state": {
      "pullback_depth_5m_pct": 0.0,
      "rebound_position_5m": 0.0,
      "prior_completed_minor_divs_in_4h_leg": 0,
      "dd_from_14d_4h_high_pct": 0.0,
      "bars_4h_since_14d_high": 0,
      "vol_5m_surge_ratio": 0.0
    },
    "btc_state": {
      "btc_1d_vs_sma50_pct": 0.0,
      "btc_4h_vs_sma18_pct": 0.0,
      "btc_ret_1h_pct": 0.0,
      "btc_ret_48h_pct": 0.0,
      "btc_dist_to_4h_high_pct": 0.0,
      "btc_sync_mode": "<RESONANCE|LAGGING_CATCHUP|NONE>",
      "hours_since_major_4h_launch": null,
      "btc_pullback_resilience_score": 0
    },
    "season_state": {
      "season_regime": "<SPRING|SUMMER|AUTUMN|WINTER>",
      "breadth_fast": 0.0,
      "breadth_slow": 0.0,
      "breadth_momentum": 0.0,
      "volume_ratio": 0.0,
      "crowding_pct": 0.0
    },
    "resonance_state": {
      "sector_bucket": "<MAJORS_PAYMENT|ALT_SMART_L1|DEFI_INFRA|MEME|EMERGING_2025_2026>",
      "same_sector_active_peers": 0,
      "same_sector_resonance_count_5m": 0,
      "cross_section_resonance_count_5m": 0
    }
  },
  "prompt": "<rendered strictly by §A.3 canonical template>"
}
```

### A.3 `prompt` 字段固定文本渲染模板（一字不改）
```text
TASK: Estimate p = P(PolicyNetR > 0 | ex-ante context at entry_ts) in [0.0, 1.0].
SETTLEMENT_MODEL: kairos wave_k_be=0.8 full-friction 1m stop-first holding policy (max_hold=1440m; +0.8R fee-inclusive breakeven at next 1m open; causal 15m/1h/4h HL structure trail; bilateral 5bps taker + 1.5/5.0bps base slippage + sqrt market impact on $10,000 notional + 8h funding).
EX_ANTE_TICKET_CONTEXT_JSON:
{canonical_context_json}
```
其中 `{canonical_context_json}` 为 `json.dumps(payload["context"], ensure_ascii=False, separators=(",", ":"))`。Stage 2 接入真实 `JEV_BASE_URL` 时，仅允许做 HTTP 传输层字段映射（即把上述完整 `payload` JSON POST 至端点并从响应 JSON 中提取概率数值 $p \in [0, 1]$），**严禁增删或改写 `target_definition`、`context` 或 `prompt` 模板中的任何字符**。
