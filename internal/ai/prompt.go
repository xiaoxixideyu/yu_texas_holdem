package ai

import "fmt"

func BuildDecisionPrompt(input DecisionInput) (string, string) {
	system := "你是德州扑克 AI 助手。你必须只输出 JSON，不要输出任何解释、markdown 或代码块。"
	user := fmt.Sprintf(`请基于当前牌局信息给出下一步动作。\n\n要求：\n1) 只能返回 JSON：{"action":"check|call|bet|allin|fold","amount":number}\n2) action 必须在 allowedActions 内\n3) 当 action 不是 bet 时，amount 必须是 0\n4) 当 action 是 bet 时，amount 必须在 [minBet/minRaise, stack] 之间并满足最小约束\n\n当前输入：%s`, mustJSON(input))
	return system, user
}

func BuildSummaryPrompt(input SummaryInput) (string, string) {
	system := "你是德州扑克复盘助手。你必须只输出 JSON，不要输出任何解释、markdown 或代码块。"
	user := fmt.Sprintf(`请根据手局信息输出复盘与对手画像。\n\n要求：\n1) 只返回 JSON：{"handSummary":"...","opponentProfiles":{"userId":{"style":"...","tendencies":["..."],"advice":"..."}}}\n2) handSummary 简洁（不超过120字）\n3) opponentProfiles 仅针对非 AI 玩家，可为空对象\n4) style/advice 必须是字符串，tendencies 必须是字符串数组\n\n当前输入：%s`, mustJSON(input))
	return system, user
}
