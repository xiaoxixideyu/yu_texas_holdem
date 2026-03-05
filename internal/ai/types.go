package ai

import "context"

type Service interface {
	Enabled() bool
	DecideAction(ctx context.Context, input DecisionInput) (Decision, error)
	SummarizeHand(ctx context.Context, input SummaryInput) (Summary, error)
}

type PlayerSnapshot struct {
	UserID       string `json:"userId"`
	Username     string `json:"username"`
	IsAI         bool   `json:"isAi"`
	SeatIndex    int    `json:"seatIndex"`
	Stack        int    `json:"stack"`
	Folded       bool   `json:"folded"`
	AllIn        bool   `json:"allIn"`
	Contributed  int    `json:"contributed"`
	RoundContrib int    `json:"roundContrib"`
	LastAction   string `json:"lastAction"`
	Won          int    `json:"won"`
}

type ActionLog struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Action   string `json:"action"`
	Amount   int    `json:"amount"`
	Stage    string `json:"stage"`
}

type DecisionInput struct {
	RoomID             string                   `json:"roomId"`
	HandID             int64                    `json:"handId"`
	StateVersion       int64                    `json:"stateVersion"`
	AIUserID           string                   `json:"aiUserId"`
	AIUsername         string                   `json:"aiUsername"`
	Stage              string                   `json:"stage"`
	Pot                int                      `json:"pot"`
	RoundBet           int                      `json:"roundBet"`
	OpenBetMin         int                      `json:"openBetMin"`
	BetMin             int                      `json:"betMin"`
	CallAmount         int                      `json:"callAmount"`
	MinBet             int                      `json:"minBet"`
	MinRaise           int                      `json:"minRaise"`
	Stack              int                      `json:"stack"`
	AllowedActions     []string                 `json:"allowedActions"`
	CommunityCards     []string                 `json:"communityCards"`
	HoleCards          []string                 `json:"holeCards"`
	HandCategory       string                   `json:"handCategory"`
	HandCategoryRank   int                      `json:"handCategoryRank"`
	HandRanks          []int                    `json:"handRanks"`
	PreflopTier        string                   `json:"preflopTier"`
	PreflopPosition    string                   `json:"preflopPosition"`
	EffectiveStackBB   float64                  `json:"effectiveStackBb"`
	PreflopFacingRaise bool                     `json:"preflopFacingRaise"`
	MadeHandStrength   string                   `json:"madeHandStrength"`
	DrawFlags          []string                 `json:"drawFlags"`
	Players            []PlayerSnapshot         `json:"players"`
	RecentActionLog    []ActionLog              `json:"recentActionLog"`
	MemorySummaries    []string                 `json:"memorySummaries"`
	Profiles           map[string]Profile       `json:"profiles"`
	OpponentStats      map[string]OpponentStats `json:"opponentStats"`
}

type Decision struct {
	Action string `json:"action"`
	Amount int    `json:"amount"`
}

type Profile struct {
	Style      string   `json:"style"`
	Tendencies []string `json:"tendencies"`
	Advice     string   `json:"advice"`
}

type OpponentStats struct {
	Hands            int     `json:"hands"`
	VPIP             float64 `json:"vpip"`
	PFR              float64 `json:"pfr"`
	AggressionFactor float64 `json:"aggressionFactor"`
	FoldRate         float64 `json:"foldRate"`
	ShowdownRate     float64 `json:"showdownRate"`
	ShowdownWinRate  float64 `json:"showdownWinRate"`
}

type SummaryInput struct {
	RoomID                string                   `json:"roomId"`
	HandID                int64                    `json:"handId"`
	AIUserID              string                   `json:"aiUserId"`
	AIUsername            string                   `json:"aiUsername"`
	ActionLogs            []ActionLog              `json:"actionLogs"`
	Winners               []string                 `json:"winners"`
	Reason                string                   `json:"reason"`
	CommunityCards        []string                 `json:"communityCards"`
	Players               []PlayerSnapshot         `json:"players"`
	ExistingMemory        []string                 `json:"existingMemory"`
	ExistingProfile       map[string]Profile       `json:"existingProfile"`
	ExistingOpponentStats map[string]OpponentStats `json:"existingOpponentStats"`
}

type Summary struct {
	HandSummary      string             `json:"handSummary"`
	OpponentProfiles map[string]Profile `json:"opponentProfiles"`
}
