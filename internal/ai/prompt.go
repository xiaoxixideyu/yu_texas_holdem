package ai

import "fmt"

func BuildDecisionPrompt(input DecisionInput) (string, string) {
	system := "你是顶级无上限德州扑克 AI 助手。你必须先在心里完成范围分析、权益/赔率比较、下注尺寸选择，再只输出 JSON，不要输出任何解释、markdown 或代码块。你只能依据输入里可见的信息决策，禁止假设或读取任何未知的对手底牌信息。diagnostics 是系统基于可见信息预计算的高价值特征，可直接作为可信依据。opponentRanges 是系统基于行动线、画像与统计生成的显式范围提示，可直接拿来做 exploit。decisionOptions 是系统基于当前局面预先生成的强候选动作列表，包含近似 EV、本地评分、风险和意图标签；baselineDecision 是本地强规则策略给出的基线动作。除非你能根据可见信息明确判断其它候选动作更优，否则优先选择 decisionOptions 中近似 EV 更高、评分更高且与 baselineDecision 接近的方案。"
	user := fmt.Sprintf(`请基于当前牌局信息给出下一步动作。

返回格式：
{"optionId":"候选动作ID或空字符串","action":"check|call|bet|allin|fold","amount":number}

硬性要求：
1) action 必须在 allowedActions 内
2) 当 action 不是 bet 时，amount 必须是 0
3) 当 action 是 bet 时，amount 必须在 [minBet/minRaise, stack] 之间并满足最小约束
4) 只能使用输入中的可见信息（自己的底牌、公共牌、行动历史、筹码与底池信息），不能假设未知对手底牌
5) 先根据 recentActionLog、profiles、opponentStats 和当前下注压力缩窄各对手范围，再比较自己对这些范围的权益
6) 强牌优先争取价值，听牌可半诈唬，边缘牌关注控池和弃牌率，短码时可提高全压频率
7) 下注尺度要和当前街、SPR、牌面湿度、范围优势、极化程度匹配；不要无理由极端 overbet
	8) diagnostics 中的 activeOpponents、potOdds、equityEstimate、pressureScore、boardWetness、spr、hasInitiative、rangeAdvantage、scareCardScore、lineCapScore、pairStrengthScore、blockerScore、missedDrawScore、showdownValueScore、stationScore、visibleTags 都可以直接使用
	9) opponentRanges 中的 preflopBucket、currentLine、likelyHandClass、foldToPressure、trapRisk、drawWeight、confidence 是对各对手当前范围的显式提示；若 confidence 较高，应优先用它来做 exploit
	10) 若 decisionOptions 非空，优先直接从中选择；选择候选动作时，optionId 必须填写对应 id，action/amount 必须与该候选完全一致
	11) decisionOptions 中的 evEstimate 代表本地近似筹码 EV，localScore 代表综合战略评分，riskScore 代表风险暴露；若无清晰 exploit 证据，优先更高 evEstimate / localScore、风险更合理、且接近 baselineDecision 的方案
	12) baselineDecision 是强规则基线；如果没有清晰 exploit 或更高 EV 证据，不要为了“看起来随机”而故意偏离它
	13) 面对下注时，若 equityEstimate 加听牌补偿仍明显落后于 potOdds，则避免轻率 call、bet 或 allin
	14) 河牌诈唬优先选择 blockerScore 高、missedDrawScore 高、showdownValueScore 低、stationScore 低、lineCapScore 高的组合
	15) 河牌若 showdownValueScore 较高，且 blockerScore、missedDrawScore 都低，则优先少诈唬、多保留摊牌价值
	16) 多人池、stationScore 高、短码或低 SPR 时，减少花哨操作，优先高确定性动作
	17) 识别自己是否有持续下注主动权、对手是否呈现 capped 范围；干燥高张面可偏小注范围下注，转牌/河牌极化诈唬与超强价值可用更大尺寸

当前输入：%s`, mustJSON(input))
	return system, user
}

func BuildSummaryPrompt(input SummaryInput) (string, string) {
	system := "你是德州扑克复盘助手。你必须只输出 JSON，不要输出任何解释、markdown 或代码块。"
	user := fmt.Sprintf(`请根据手局信息输出复盘与对手画像。\n\n要求：\n1) 只返回 JSON：{"handSummary":"...","opponentProfiles":{"userId":{"style":"...","tendencies":["..."],"advice":"..."}}}\n2) handSummary 简洁（不超过120字）\n3) opponentProfiles 仅针对非 AI 玩家，可为空对象\n4) style/advice 必须是字符串，tendencies 必须是字符串数组\n5) style 尽量使用紧凶/紧弱/松凶/松弱/跟注站/爱诈唬/爱慢打，或等价英文标签（如 tight-aggressive、calling-station）\n6) tendencies/advice 尽量写成可用于后续 exploit 的短标签，例如：弃牌偏多、河牌过度跟注、翻牌圈喜欢延续下注、转牌诚实、爱偷鸡\n\n当前输入：%s`, mustJSON(input))
	return system, user
}
