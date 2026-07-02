package alert

import (
	"fmt"
	"html"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/types"
)

var actionRank = map[string]int{
	"no_trade":        0,
	"watch":           1,
	"prepare":         2,
	"trade_candidate": 3,
}

// SelectSetups returns scanner setups at or above minState, sorted by rank.
func SelectSetups(scan *types.SignalEnvelope, minState string, limit int) []map[string]any {
	if scan == nil || scan.Data == nil {
		return nil
	}
	minimum, ok := actionRank[minState]
	if !ok {
		minimum = actionRank["prepare"]
	}

	setups := extractSetupMaps(scan.Data)
	selected := make([]map[string]any, 0, len(setups))
	for _, setup := range setups {
		state, _ := setup["action_state"].(string)
		if actionRank[state] >= minimum {
			selected = append(selected, setup)
		}
	}

	sortSetupsByRank(selected)
	if limit < 1 {
		limit = 1
	}
	if len(selected) > limit {
		selected = selected[:limit]
	}
	return selected
}

func extractSetupMaps(data map[string]any) []map[string]any {
	if raw, ok := data["setups"].([]map[string]any); ok && len(raw) > 0 {
		return raw
	}
	if raw, ok := data["qualified_setups"].([]map[string]any); ok && len(raw) > 0 {
		return raw
	}
	if raw, ok := data["setups"].([]any); ok && len(raw) > 0 {
		return toSetupMaps(raw)
	}
	if raw, ok := data["qualified_setups"].([]any); ok {
		return toSetupMaps(raw)
	}
	return nil
}

func toSetupMaps(raw []any) []map[string]any {
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func sortSetupsByRank(setups []map[string]any) {
	for i := 0; i < len(setups); i++ {
		for j := i + 1; j < len(setups); j++ {
			a, _ := setups[i]["action_state"].(string)
			b, _ := setups[j]["action_state"].(string)
			if actionRank[b] > actionRank[a] {
				setups[i], setups[j] = setups[j], setups[i]
			}
		}
	}
}

// FormatAlert builds one compact Telegram HTML alert from setup maps.
func FormatAlert(setups []map[string]any) string {
	parts := []string{"<b>Kairos 机会筛选</b> | <b>非指令</b> 仅供人工判断"}
	for _, setup := range setups {
		risk, _ := setup["risk"].(map[string]any)
		if risk == nil {
			risk = map[string]any{}
		}
		reasons := stringList(setup["reasons"])
		warnings := stringList(setup["warnings"])
		direction := html.EscapeString(directionZH(fmt.Sprint(setup["direction"])))
		symbol := html.EscapeString(displaySymbol(fmt.Sprint(setup["symbol"])))
		state := html.EscapeString(stateZH(fmt.Sprint(setup["action_state"])))
		setupType := html.EscapeString(setupTypeZH(fmt.Sprint(setup["setup_type"])))
		matched, missing := strategyPoints(reasons, warnings)
		parts = append(parts,
			"",
			fmt.Sprintf("<b>[%s] %s %s</b> | %s | %v/%v", state, symbol, direction, setupType, setup["setup_score"], setup["threshold"]),
			fmt.Sprintf("<b>匹配</b>: %s", joinOrDash(matched, "、")),
			fmt.Sprintf("<b>缺口</b>: %s", joinOrDash(missing, "、")),
			fmt.Sprintf("<b>位</b>: 入 %s | 损 %s | 目 %s", fmtList(risk["entry_zone"], " - "), formatAnyFloat(risk["structural_stop"]), fmtList(risk["targets"], " / ")),
			fmt.Sprintf("<b>RR/上限</b>: %s | %s%% / %sx", formatAnyFloat(risk["risk_reward"]), formatAnyFloat(risk["max_position_pct"]), formatAnyFloat(risk["max_leverage"])),
		)
	}
	return strings.Join(parts, "\n")
}

func stringList(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		if s, ok := v.([]string); ok {
			return s
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func fmtList(value any, sep string) string {
	raw, ok := value.([]any)
	if !ok {
		if nums, ok := value.([]float64); ok {
			parts := make([]string, 0, len(nums))
			for _, n := range nums {
				parts = append(parts, formatFloat(n))
			}
			if len(parts) > 3 {
				parts = parts[:3]
			}
			if len(parts) == 0 {
				return "-"
			}
			return strings.Join(parts, sep)
		}
		return "-"
	}
	parts := make([]string, 0, len(raw))
	for _, item := range raw {
		switch v := item.(type) {
		case float64:
			parts = append(parts, formatFloat(v))
		case float32:
			parts = append(parts, formatFloat(float64(v)))
		default:
			parts = append(parts, fmt.Sprint(item))
		}
	}
	if len(parts) > 3 {
		parts = parts[:3]
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, sep)
}

func joinOrDash(items []string, sep string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, sep)
}

func displaySymbol(symbol string) string {
	s := strings.ReplaceAll(symbol, "/USDT:USDT", "")
	return strings.ReplaceAll(s, "/USDT", "")
}

func strategyPoints(reasons, warnings []string) (matched, missing []string) {
	reasonText := strings.Join(reasons, "\n")
	warningText := strings.Join(warnings, "\n")

	if strings.Contains(reasonText, "1d trend supports") {
		matched = append(matched, "日线顺势")
	} else if strings.Contains(warningText, "1d trend conflicts") {
		missing = append(missing, "日线逆势")
	}
	if strings.Contains(reasonText, "4h ") && strings.Contains(reasonText, "structure is usable") {
		matched = append(matched, "4H结构")
	} else if strings.Contains(warningText, "4h structure is not usable") {
		missing = append(missing, "4H结构不足")
	}
	if strings.Contains(reasonText, "BTC resonance supports direction") || strings.Contains(reasonText, "BTC setup has no separate BTC resonance requirement") {
		matched = append(matched, "BTC共振")
	} else if strings.Contains(warningText, "BTC resonance conflicts") {
		missing = append(missing, "BTC不共振")
	} else if strings.Contains(warningText, "BTC resonance is neutral") {
		missing = append(missing, "BTC中性")
	}
	if strings.Contains(reasonText, "15m trigger is active") {
		matched = append(matched, "15m触发")
	} else if strings.Contains(reasonText, "15m price is near trigger") {
		matched = append(matched, "接近触发")
	}
	if strings.Contains(reasonText, "15m volume confirms move") {
		matched = append(matched, "量能确认")
	} else if strings.Contains(warningText, "15m volume confirmation missing") {
		missing = append(missing, "缺量能确认")
	}
	if strings.Contains(reasonText, "risk/reward") && strings.Contains(reasonText, "meets requirement") {
		matched = append(matched, "盈亏比达标")
	} else if strings.Contains(warningText, "risk/reward") && strings.Contains(warningText, "below requirement") {
		missing = append(missing, "盈亏比不足")
	}
	if strings.Contains(reasonText, "cycle component=") {
		matched = append(matched, "周期支持")
	} else if strings.Contains(warningText, "cycle does not support") {
		missing = append(missing, "周期不支持")
	}
	if len(matched) > 5 {
		matched = matched[:5]
	}
	if len(missing) > 4 {
		missing = missing[:4]
	}
	return matched, missing
}

func directionZH(direction string) string {
	switch direction {
	case "long", "LONG":
		return "做多"
	case "short", "SHORT":
		return "做空"
	default:
		return direction
	}
}

func stateZH(state string) string {
	switch state {
	case "no_trade":
		return "不交易"
	case "watch":
		return "观察"
	case "prepare":
		return "准备"
	case "trade_candidate":
		return "交易候选"
	default:
		return state
	}
}

func setupTypeZH(setupType string) string {
	switch setupType {
	case "box_breakout":
		return "箱体突破"
	case "box_breakdown":
		return "箱体跌破"
	case "range_breakout":
		return "区间突破"
	case "range_breakdown":
		return "区间跌破"
	case "box_support":
		return "箱体支撑"
	default:
		return setupType
	}
}

func formatFloat(v float64) string {
	s := fmt.Sprintf("%.10f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func formatAnyFloat(v any) string {
	switch n := v.(type) {
	case float64:
		return formatFloat(n)
	case float32:
		return formatFloat(float64(n))
	case int:
		return formatFloat(float64(n))
	case int64:
		return formatFloat(float64(n))
	default:
		return fmt.Sprint(v)
	}
}
