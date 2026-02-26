package domain

import "testing"

func newPlayers() []*GamePlayer {
	return []*GamePlayer{
		{UserID: "u1", Username: "A", SeatIndex: 0, Stack: 200},
		{UserID: "u2", Username: "B", SeatIndex: 1, Stack: 200},
	}
}

func TestGame_BlindsPosted(t *testing.T) {
	g, err := NewGame(newPlayers(), 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Heads-up: dealer(0)=SB, seat 1=BB
	if g.SmallBlindPos != 0 {
		t.Fatalf("expected SB at 0, got %d", g.SmallBlindPos)
	}
	if g.BigBlindPos != 1 {
		t.Fatalf("expected BB at 1, got %d", g.BigBlindPos)
	}
	sb := g.Players[0]
	bb := g.Players[1]
	if sb.Stack != 195 { // 200 - 5 (small blind = 10/2)
		t.Fatalf("expected SB stack 195, got %d", sb.Stack)
	}
	if bb.Stack != 190 { // 200 - 10 (big blind)
		t.Fatalf("expected BB stack 190, got %d", bb.Stack)
	}
	if g.Pot != 15 {
		t.Fatalf("expected pot 15, got %d", g.Pot)
	}
	if g.RoundBet != 10 {
		t.Fatalf("expected round bet 10, got %d", g.RoundBet)
	}
	// Preflop action starts with SB (dealer) in heads-up
	if g.Players[g.TurnPos].UserID != "u1" {
		t.Fatalf("expected first action on u1 (SB), got %s", g.Players[g.TurnPos].UserID)
	}
}

func TestGame_FoldEndsHand(t *testing.T) {
	g, err := NewGame(newPlayers(), 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Heads-up preflop: u1 (SB) acts first, fold
	current := g.Players[g.TurnPos].UserID
	if err := g.ApplyAction(current, "fold", 0); err != nil {
		t.Fatal(err)
	}
	if g.Stage != StageFinished {
		t.Fatalf("expected finished stage, got %s", g.Stage)
	}
	if g.Result == nil || len(g.Result.Winners) != 1 {
		t.Fatalf("expected one winner")
	}
}

func TestGame_CallAndCheckToShowdown(t *testing.T) {
	g, err := NewGame(newPlayers(), 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Preflop: u1 (SB) calls to match BB, u2 (BB) checks
	u1 := g.Players[g.TurnPos].UserID
	if err := g.ApplyAction(u1, "call", 0); err != nil {
		t.Fatalf("preflop SB call failed: %v", err)
	}
	u2 := g.Players[g.TurnPos].UserID
	if err := g.ApplyAction(u2, "check", 0); err != nil {
		t.Fatalf("preflop BB check failed: %v", err)
	}
	// After preflop, check through remaining stages
	for g.Stage != StageFinished {
		u := g.Players[g.TurnPos].UserID
		if err := g.ApplyAction(u, "check", 0); err != nil {
			t.Fatalf("check failed at stage=%s err=%v", g.Stage, err)
		}
	}
	if g.Result == nil {
		t.Fatalf("expected showdown result")
	}
}

func TestGame_RevealMaskDefaultsToZero(t *testing.T) {
	g, err := NewGame(newPlayers(), 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range g.Players {
		if p.RevealMask != 0 {
			t.Fatalf("expected default reveal mask 0, got %d", p.RevealMask)
		}
	}
}

func TestGame_SetRevealSelection_OnlyWhenFinished(t *testing.T) {
	g, err := NewGame(newPlayers(), 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.SetRevealSelection("u1", 1); err == nil {
		t.Fatalf("expected reveal before finished to fail")
	}
	current := g.Players[g.TurnPos].UserID
	if err := g.ApplyAction(current, "fold", 0); err != nil {
		t.Fatal(err)
	}
	if g.Stage != StageFinished {
		t.Fatalf("expected finished stage, got %s", g.Stage)
	}
	if err := g.SetRevealSelection("u1", 2); err != nil {
		t.Fatalf("expected reveal at finished to succeed, got %v", err)
	}
	for _, p := range g.Players {
		if p.UserID == "u1" && p.RevealMask != 2 {
			t.Fatalf("expected u1 reveal mask 2, got %d", p.RevealMask)
		}
	}
}

func TestGame_SetRevealSelection_InvalidMaskRejected(t *testing.T) {
	g, err := NewGame(newPlayers(), 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	current := g.Players[g.TurnPos].UserID
	if err := g.ApplyAction(current, "fold", 0); err != nil {
		t.Fatal(err)
	}
	if err := g.SetRevealSelection("u1", -1); err == nil {
		t.Fatalf("expected negative reveal mask to fail")
	}
	if err := g.SetRevealSelection("u1", 4); err == nil {
		t.Fatalf("expected reveal mask > 3 to fail")
	}
}

func TestGame_ShowdownMainPotWithOvercall(t *testing.T) {
	players := []*GamePlayer{
		{UserID: "u1", Username: "A", SeatIndex: 0, Stack: 10000},
		{UserID: "u2", Username: "B", SeatIndex: 1, Stack: 10000},
		{UserID: "u3", Username: "C", SeatIndex: 2, Stack: 6000},
	}
	g, err := NewGame(players, 0, 20, 20)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range g.Players {
		p.Contributed = 0
		p.RoundContrib = 0
		p.Folded = false
		p.AllIn = false
		p.Won = 0
	}
	g.Pot = 0

	u1 := g.Players[0]
	u2 := g.Players[1]
	u3 := g.Players[2]
	u1.Contributed = 6020
	u2.Contributed = 6020
	u3.Contributed = 6000
	g.Pot = 18040

	cards := []Card{
		{Rank: 2, Suit: Hearts}, {Rank: 3, Suit: Hearts}, {Rank: 4, Suit: Hearts},
		{Rank: 5, Suit: Hearts}, {Rank: 9, Suit: Clubs},
	}
	g.CommunityCards = cards
	u3.HoleCards = []Card{{Rank: 14, Suit: Hearts}, {Rank: 13, Suit: Hearts}}
	u1.HoleCards = []Card{{Rank: 14, Suit: Clubs}, {Rank: 13, Suit: Clubs}}
	u2.HoleCards = []Card{{Rank: 12, Suit: Diamonds}, {Rank: 11, Suit: Diamonds}}

	g.finishShowdown()

	if u3.Won != 18000 {
		t.Fatalf("expected short stack winner to win 18000, got %d", u3.Won)
	}
	if u1.Won+u2.Won != 40 {
		t.Fatalf("expected unmatched overcall chips 40 to remain among deep stacks, got %d", u1.Won+u2.Won)
	}
}
