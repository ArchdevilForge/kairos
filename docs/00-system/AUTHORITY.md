# Authority Graph

只有一个权威版本链。任何文档不得自称 authoritative，除非它在这条链上。

```text
NORTH_STAR  (docs/00-system/NORTH_STAR.md)
    ↓
KAIROS_DOCTRINE  (docs/10-doctrine/KAIROS_DOCTRINE.md — canonical freeze)
    ↓
Market / Cycle Models  (docs/20-models/)
    ↓
Playbooks  (docs/30-playbooks/)
    ↓
Runtime Contracts  (schemas/ + internal/ 代码)
    ↓
Research Results  (docs/40-research/ — 证据, 不是规则)
```

## 裁决规则

1. **冲突时谁赢**：`KAIROS_DOCTRINE` > `20-models` > `30-playbooks` > 代码。若代码和 doctrine 不一致，以 doctrine 为准，直到 PR 同时更新两者。
2. **Source 层不是规则**：`docs/10-doctrine/SOURCE_*.md`（如 Bit浪浪交易哲学）是交易思想的**来源**，未经 `Source → Hypothesis → Finding → Canonical` 生命周期验证前，不进入任何权威层。
3. **archive 不参与裁决**：`docs/archive/` 只读历史，一律不标 authoritative。
4. **Research 是证据不是规则**：`docs/40-research/` 的 findings 只记录"测了什么、结果如何"，不写"应该怎么做"。
5. **新增权威文档的入口**：先在 `docs/INDEX.md` 登记，再写内容。

## 知识生命周期

```text
Source (10-doctrine/SOURCE_*) → Hypothesis (40-research/hypotheses/)
→ Experiment (40-research/experiments/) → Finding (40-research/findings/)
→ Canonical (KAIROS_DOCTRINE / playbook)
```
