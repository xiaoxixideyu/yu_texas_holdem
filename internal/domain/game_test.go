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

func TestGame_ThreePlayerBlinds(t *testing.T) {
	players := []*GamePlayer{
		{UserID: "u1", Username: "A", SeatIndex: 0, Stack: 500},
		{UserID: "u2", Username: "B", SeatIndex: 1, Stack: 500},
		{UserID: "u3", Username: "C", SeatIndex: 2, Stack: 500},
	}
	g, err := NewGame(players, 0, 20, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Dealer=0, SB=1, BB=2
	if g.SmallBlindPos != 1 {
		t.Fatalf("expected SB at 1, got %d", g.SmallBlindPos)
	}
	if g.BigBlindPos != 2 {
		t.Fatalf("expected BB at 2, got %d", g.BigBlindPos)
	}
	if g.Players[1].Stack != 490 { // 500 - 10 (SB = 20/2)
		t.Fatalf("expected SB stack 490, got %d", g.Players[1].Stack)
	}
	if g.Players[2].Stack != 480 { // 500 - 20 (BB)
		t.Fatalf("expected BB stack 480, got %d", g.Players[2].Stack)
	}
	if g.Pot != 30 {
		t.Fatalf("expected pot 30, got %d", g.Pot)
	}
	// First to act preflop is seat after BB = seat 0 (u1, the dealer/UTG)
	if g.Players[g.TurnPos].UserID != "u1" {
		t.Fatalf("expected first action on u1 (UTG), got %s", g.Players[g.TurnPos].UserID)
	}
}
