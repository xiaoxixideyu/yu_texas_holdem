package domain

import "sort"

type HandValue struct {
	Category int
	Ranks    []int
}

func CompareHandValue(a, b HandValue) int {
	if a.Category != b.Category {
		if a.Category > b.Category {
			return 1
		}
		return -1
	}
	for i := 0; i < len(a.Ranks) && i < len(b.Ranks); i++ {
		if a.Ranks[i] > b.Ranks[i] {
			return 1
		}
		if a.Ranks[i] < b.Ranks[i] {
			return -1
		}
	}
	return 0
}

func BestOfSeven(cards []Card) (HandValue, []Card, string) {
	if len(cards) < 5 {
		return HandValue{}, nil, ""
	}
	best := HandValue{Category: -1}
	var bestCards []Card
	indices := combinations(len(cards), 5)
	for _, idx := range indices {
		hand := []Card{cards[idx[0]], cards[idx[1]], cards[idx[2]], cards[idx[3]], cards[idx[4]]}
		v := EvaluateFive(hand)
		if CompareHandValue(v, best) > 0 {
			best = v
			bestCards = hand
		}
	}
	return best, bestCards, handCategoryName(best.Category)
}

func EvaluateFive(cards []Card) HandValue {
	ranks := make([]int, 0, 5)
	rankCount := map[int]int{}
	suitCount := map[Suit]int{}
	for _, c := range cards {
		ranks = append(ranks, c.Rank)
		rankCount[c.Rank]++
		suitCount[c.Suit]++
	}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i] > ranks[j] })
	isFlush := false
	for _, c := range suitCount {
		if c == 5 {
			isFlush = true
			break
		}
	}
	straightHigh, isStraight := detectStraight(ranks)

	if isFlush && isStraight {
		return HandValue{Category: 8, Ranks: []int{straightHigh}}
	}

	counts := make([][2]int, 0, len(rankCount))
	for r, c := range rankCount {
		counts = append(counts, [2]int{c, r})
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i][0] != counts[j][0] {
			return counts[i][0] > counts[j][0]
		}
		return counts[i][1] > counts[j][1]
	})

	if counts[0][0] == 4 {
		kicker := 0
		for _, cr := range counts {
			if cr[0] == 1 {
				kicker = cr[1]
				break
			}
		}
		return HandValue{Category: 7, Ranks: []int{counts[0][1], kicker}}
	}

	if counts[0][0] == 3 && counts[1][0] == 2 {
		return HandValue{Category: 6, Ranks: []int{counts[0][1], counts[1][1]}}
	}

	if isFlush {
		return HandValue{Category: 5, Ranks: uniqueSortedDesc(ranks)}
	}

	if isStraight {
		return HandValue{Category: 4, Ranks: []int{straightHigh}}
	}

	if counts[0][0] == 3 {
		kickers := make([]int, 0, 2)
		for _, cr := range counts {
			if cr[0] == 1 {
				kickers = append(kickers, cr[1])
			}
		}
		sort.Slice(kickers, func(i, j int) bool { return kickers[i] > kickers[j] })
		return HandValue{Category: 3, Ranks: append([]int{counts[0][1]}, kickers...)}
	}

	if counts[0][0] == 2 && counts[1][0] == 2 {
		highPair := counts[0][1]
		lowPair := counts[1][1]
		kicker := 0
		for _, cr := range counts {
			if cr[0] == 1 {
				kicker = cr[1]
				break
			}
		}
		return HandValue{Category: 2, Ranks: []int{highPair, lowPair, kicker}}
	}

	if counts[0][0] == 2 {
		kickers := make([]int, 0, 3)
		for _, cr := range counts {
			if cr[0] == 1 {
				kickers = append(kickers, cr[1])
			}
		}
		sort.Slice(kickers, func(i, j int) bool { return kickers[i] > kickers[j] })
		return HandValue{Category: 1, Ranks: append([]int{counts[0][1]}, kickers...)}
	}

	return HandValue{Category: 0, Ranks: uniqueSortedDesc(ranks)}
}

func detectStraight(ranks []int) (int, bool) {
	uniqMap := map[int]bool{}
	for _, r := range ranks {
		uniqMap[r] = true
	}
	uniq := make([]int, 0, len(uniqMap))
	for r := range uniqMap {
		uniq = append(uniq, r)
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i] > uniq[j] })
	if len(uniq) < 5 {
		return 0, false
	}
	for i := 0; i <= len(uniq)-5; i++ {
		if uniq[i]-1 == uniq[i+1] && uniq[i+1]-1 == uniq[i+2] && uniq[i+2]-1 == uniq[i+3] && uniq[i+3]-1 == uniq[i+4] {
			return uniq[i], true
		}
	}
	// Wheel straight: A-2-3-4-5
	if uniqMap[14] && uniqMap[5] && uniqMap[4] && uniqMap[3] && uniqMap[2] {
		return 5, true
	}
	return 0, false
}

func uniqueSortedDesc(in []int) []int {
	m := map[int]bool{}
	for _, v := range in {
		m[v] = true
	}
	out := make([]int, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	return out
}

func combinations(n, k int) [][]int {
	res := [][]int{}
	comb := make([]int, k)
	var dfs func(start, idx int)
	dfs = func(start, idx int) {
		if idx == k {
			c := make([]int, k)
			copy(c, comb)
			res = append(res, c)
			return
		}
		for i := start; i <= n-(k-idx); i++ {
			comb[idx] = i
			dfs(i+1, idx+1)
		}
	}
	dfs(0, 0)
	return res
}

func handCategoryName(category int) string {
	switch category {
	case 8:
		return "straight_flush"
	case 7:
		return "four_of_a_kind"
	case 6:
		return "full_house"
	case 5:
		return "flush"
	case 4:
		return "straight"
	case 3:
		return "three_of_a_kind"
	case 2:
		return "two_pair"
	case 1:
		return "one_pair"
	default:
		return "high_card"
	}
}
