# Phase 3b-J Stage 2 传输层映射附记（Transport Addendum）

> **状态**：Stage 2 Pre-Execution Transport Anchor（作为 `feat/kairos-phase3bj-stage2-live-jev` 分支首个提交先行落盘；在本文件提交前未发出任何 live 请求）
> **基线提交**：`d0468da`（`main`，含 PR #12 Stage 1 全部产物）
> **上级宪法锚点**：`docs/40-research/experiments/PHASE3BJ_PREREG.md`（一字不改；本附记严格依据其 §A.3 授权仅定义 HTTP 传输层请求/响应字段映射）

---

## 1. 载荷模板指纹核验（Pre-Flight Spec Hash Check）

在编写任何 Stage 2 适配代码或发起任何网络请求前，对 `tools/research/jev_client.py:compute_template_spec_sha256()` 进行实测核验：

| 核验项 | Stage 1 冻结锚点值 | Stage 2 实测值 | 结论 |
|---|---|---|---|
| `TEMPLATE_ID` | `kairos.phase3bj.blind_judgment_payload.v1` | `kairos.phase3bj.blind_judgment_payload.v1` | **一致** |
| `template_spec_sha256` | `e77cd51842062da704d61b42030b310adb0de17c7fc2a4923f50740a4c580f1c` | `e77cd51842062da704d61b42030b310adb0de17c7fc2a4923f50740a4c580f1c` | **PASS（逐字节一致）** |

- `build_jev_payload(snapshot)`、`FROZEN_TARGET_DEFINITION`、`FROZEN_PROMPT_TEMPLATE`、`EXPECTED_CONTEXT_TOP_KEYS` 及全部评测门槛（$\tau = 0.60$、$\Delta\text{NetR} \ge +0.18\text{R}$、单侧 $p < 0.05$、$G_{\text{oos}} \ge 15$、P1/P2 独立判定）保持零改动。

---

## 2. 端点、模型字符串与凭据红线

- **目标端点**：通过进程环境变量 `JEV_BASE_URL` 注入 `https://api.typesafe.ai/v1/systemone`（客户端将 `base_url` 原样作为 HTTP `POST` 目标，不追加路径）。
- **认证头**：通过进程环境变量 `JEV_API_KEY` 注入 `Authorization: Bearer <REDACTED>`；仓库文件、运行清单、判断缓存、测试代码、日志与报告中严禁出现真实密钥。
- **固定请求模型字符串**：`model = "jev-latest"`（写死，严禁在运行中途更换模型别名或改用任何其他模型）。

---

## 3. HTTP 传输层映射全文（`JevClient` `transport` 注入点）

Stage 1 客户端 `JevClient.judge_single_stateless` 将 `build_jev_payload(snapshot)` 序列化为 JSON 字节流传入 `transport(url, body_bytes, headers, timeout_sec)`，并从返回的 JSON 字节流中由 `_extract_probability_from_response` 读取顶层 `"p"`。Stage 2 通过注入 `SystemOneTransportAdapter` 完成与 TypeSafe System One API 的无损协议适配：

### 3.1 请求映射（Request Mapping）

设 `payload` 为 `build_jev_payload(snapshot)` 生成并经 `assert_blind_payload_clean(payload)` 校验通过的字典对象。适配器构造发往 `POST https://api.typesafe.ai/v1/systemone` 的 JSON 请求体如下：

```python
wire_request = {
    "model": "jev-latest",
    "state": payload["prompt"],
    "questions": {
        "netr_gt_0": {
            "type": "noul",
            "instructions": payload["target_definition"],
        }
    },
}
```

**硬约束**：
1. `state` 原样取 `payload["prompt"]`（即 `PHASE3BJ_PREREG.md` §A.3 固定渲染文本，一字不改）；
2. `questions.netr_gt_0.instructions` 原样取 `payload["target_definition"]`（即 `PHASE3BJ_PREREG.md` §A.2 固定目标定义对象，一字不改）；
3. 严禁在适配器中新写、拼接或改写任何判断提示词、准则或上下文字符。

### 3.2 响应映射与异常阻断（Response Mapping & Strict Validation）

TypeSafe System One `HTTP 200` JSON 响应结构为：

```json
{
  "model": "<实际服务构建，如 jev-1.14.0>",
  "answers": {
    "netr_gt_0": {
      "type": "noul",
      "noul": 0.62
    }
  },
  "usage": {
    "input_tokens": 850,
    "output_tokens": 12
  }
}
```

适配器执行以下严格校验与提取（任何一项不满足立即抛出 `ValueError` 阻断，严禁吞错或静默赋默认值）：
1. 顶层必须为 JSON 对象，且包含非空字符串字段 `model`（记录为当次响应的实际服务构建 `service_build`）；
2. `answers` 必须为 JSON 对象，且包含子对象 `answers["netr_gt_0"]`；若存在 `type` 字段则必须等于 `"noul"`；
3. `answers["netr_gt_0"]["noul"]` 必须存在，类型为有限数值 `int | float`（显式拒绝 `bool`、`None`、字符串、`NaN`、`Inf`），且满足闭区间 $0.0 \le p \le 1.0$；
4. 若响应包含 `usage`，其必须为 JSON 对象且 `usage["input_tokens"]`（及 `output_tokens`）为非负整数，累加计入运行清单 `total_input_tokens`；
5. 将提取出的概率值重新封装为 `{"p": <answers.netr_gt_0.noul>}` UTF-8 JSON 字节流返回给 `JevClient`，由既定 `_extract_probability_from_response` 消费并落盘缓存。

---

## 4. 服务构建漂移守则（Service Build Drift Guard）

由于请求指定别名 `"jev-latest"`，上游实际执行推理的服务构建由响应顶层 `model` 字段标识（例如 `"jev-1.14.0"`）。为保证全量盲判来自单一确定模型构建，全程强制执行以下漂移守则：

1. **构建锁定**：当本实验首个 live 响应返回（或从既有 Smoke 阶段正式 live 缓存加载已有 `service_build`）时，立即将该服务构建锁定为基准构建 `locked_build`。
2. **逐票核验**：后续每一次 live 响应在写入缓存前必须校验 `response["model"] == locked_build`。
3. **漂移即停（零混批容忍）**：一旦检测到 `response["model"] != locked_build`，适配器立即标记 `build_drift_detected = true`、记录观测到的构建集合 `service_builds`，并在该票落盘前抛出 `BuildDriftError` 立即中止批量运行；随后在运行清单与交付总结中据实报告，**严禁将不同服务构建的判断结果混入同一批次评测**。

---

## 5. 执行纪律锚点

1. **Smoke 3 张先行**：在正式数据集抽出后，先按同一模板、同一端点对 `P1 OOS`（1 张）、`P2`（1 张）、`P1 IS-only`（1 张）执行 3 张 live 冒烟，验证 $p \in [0, 1]$、`service_build` 记录、`input_tokens` 统计与 JSONL 缓存落盘正常；该 3 张直接计入正式 live 缓存，全量阶段幂等跳过不重打。
2. **全量盲判**：按客户端预登记随机提交序（`shuffle_seed = 20261111`）与退避重试策略完成剩余全部未缓存候选盲判，全程启用构建漂移守则，并将 `service_builds`、`total_input_tokens`、`failed_calls` 完整写入运行清单。
