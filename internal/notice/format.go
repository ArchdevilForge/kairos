package notice

import (
	"fmt"
	"html"
	"strings"

	"github.com/ArchdevilForge/kairos/internal/types"
)

// forbiddenNoticeTerms lists trade-instruction and probability/score words strictly
// banned from Notice copy under §0 and §3 B1.
var forbiddenNoticeTerms = []string{
	"开多",
	"开空",
	"做多",
	"做空",
	"买入",
	"卖出",
	"建议入场",
	"建仓",
	"止盈",
	"抄底",
	"摸顶",
	"胜率",
	"概率",
	"把握",
	"推荐",
	"评分",
	"打分",
	"signal_score",
	"score=",
}

// ContainsForbiddenNoticeTerms checks rendered Notice copy against §0 / §3 B1 red-line vocabulary.
func ContainsForbiddenNoticeTerms(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, term := range forbiddenNoticeTerms {
		if strings.Contains(lower, strings.ToLower(term)) {
			return term, true
		}
	}
	return "", false
}

// FormatNoticeTelegram renders a Notice into Telegram HTML (§3 B1/B3).
// Guarantees:
//   - Scene language only (no buy/sell instruction, no score/probability endorsement).
//   - SeasonRegime rendered ONLY when genuinely computed (omitted when empty).
//   - WINTER notice includes the 0.25x winter defense hint.
//   - Fixed final line: "场景提示，未验证信号，判断在你".
func FormatNoticeTelegram(n types.Notice) string {
	var b strings.Builder

	sym := html.EscapeString(n.Symbol)
	if n.Exchange != "" {
		fmt.Fprintf(&b, "🔭 <b>机会场景通知</b> · <code>%s</code> (%s)\n", sym, html.EscapeString(strings.ToUpper(n.Exchange)))
	} else {
		fmt.Fprintf(&b, "🔭 <b>机会场景通知</b> · <code>%s</code>\n", sym)
	}

	if len(n.Scenes) > 0 {
		for _, sc := range n.Scenes {
			fmt.Fprintf(&b, "• <b>场景</b>: %s\n", html.EscapeString(sc.Summary))
		}
	}

	factLine := html.EscapeString(n.TriggerFacts.FactSummary)
	if n.TriggerFacts.Price > 0 {
		fmt.Fprintf(&b, "• <b>触发事实</b>: %s · 现价 <code>%.6g</code>\n", factLine, n.TriggerFacts.Price)
	} else {
		fmt.Fprintf(&b, "• <b>触发事实</b>: %s\n", factLine)
	}

	// Season tag: ONLY present when genuinely computed (§3 B3).
	if season := types.NormalizeSeasonRegime(n.Env.SeasonRegime); season != "" {
		fmt.Fprintf(&b, "• <b>季节标签</b>: <code>%s</code>\n", html.EscapeString(string(season)))
		hint := n.Env.WinterDefenseHint
		if hint == "" {
			hint = types.WinterDefenseHintFor(season)
		}
		if hint != "" {
			fmt.Fprintf(&b, "• <b>冬季提示</b>: %s\n", html.EscapeString(hint))
		}
	}

	boxLabel := types.NoticeBoxStateLabelZH(n.Env.BoxState)
	if n.Env.BoxWidthPct > 0 {
		fmt.Fprintf(&b, "• <b>箱体状态</b>: %s（宽度 %.1f%%，位置 %.2f）\n",
			html.EscapeString(boxLabel), n.Env.BoxWidthPct, n.Env.BoxPos)
	} else {
		fmt.Fprintf(&b, "• <b>箱体状态</b>: %s\n", html.EscapeString(boxLabel))
	}

	if strings.TrimSpace(n.Env.BTCState) != "" {
		fmt.Fprintf(&b, "• <b>BTC 状态</b>: %s\n", html.EscapeString(n.Env.BTCState))
	}

	if n.NoticeID != "" {
		fmt.Fprintf(&b, "• <b>Notice ID</b>: <code>%s</code>\n", html.EscapeString(n.NoticeID))
	}

	b.WriteString(types.NoticeDisclaimerLine)
	return b.String()
}

// FormatNoticePlain renders a concise plain-text Notice view for kairos-desk.
func FormatNoticePlain(n types.Notice, fb *types.NoticeFeedback) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Notice %s  [%s]  %s\n", n.NoticeID, n.DeliveryStatus, n.Symbol)
	fmt.Fprintf(&b, "  时间: %s\n", n.CreatedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(&b, "  触发事实: %s", n.TriggerFacts.FactSummary)
	if n.TriggerFacts.Price > 0 {
		fmt.Fprintf(&b, " (现价 %.6g)", n.TriggerFacts.Price)
	}
	b.WriteString("\n")
	for _, sc := range n.Scenes {
		fmt.Fprintf(&b, "  场景旗 [%s]: %s\n", sc.Flag, sc.Summary)
	}
	if n.Env.SeasonRegime != "" {
		fmt.Fprintf(&b, "  季节标签: %s\n", n.Env.SeasonRegime)
		if n.Env.WinterDefenseHint != "" {
			fmt.Fprintf(&b, "  冬季提示: %s\n", n.Env.WinterDefenseHint)
		}
	}
	fmt.Fprintf(&b, "  箱体状态: %s", types.NoticeBoxStateLabelZH(n.Env.BoxState))
	if n.Env.BoxWidthPct > 0 {
		fmt.Fprintf(&b, " (宽度 %.1f%%, 位置 %.2f)", n.Env.BoxWidthPct, n.Env.BoxPos)
	}
	b.WriteString("\n")
	if n.Env.BTCState != "" {
		fmt.Fprintf(&b, "  BTC 状态: %s\n", n.Env.BTCState)
	}
	if n.SuppressReason != "" {
		fmt.Fprintf(&b, "  抑制原因: %s (%s)\n", n.SuppressReason, n.SuppressDetail)
	}
	if fb != nil {
		fmt.Fprintf(&b, "  人工反馈: %s (%s)", types.NoticeFeedbackLabelZH(fb.Action), fb.Action)
		if fb.TicketID != "" {
			fmt.Fprintf(&b, " 关联票据=%s", fb.TicketID)
		}
		if fb.Note != "" {
			fmt.Fprintf(&b, " 备注=%s", fb.Note)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("  人工反馈: 待反馈 (kairos-desk feedback --action <看了|做了|忽略> " + n.NoticeID + ")\n")
	}
	fmt.Fprintf(&b, "  %s\n", types.NoticeDisclaimerLine)
	return b.String()
}
