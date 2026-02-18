package domain

import "testing"

func newPlayers() []*GamePlayer {
	return []*GamePlayer{
		{UserID: "u1", Username: "A", SeatIndex: 0, Stack: 200},
		{UserID: "u2", Username: "B", SeatIndex: 1, Stack: 200},
	}
}

func TestGame_FoldEndsHand(t *testing.T) {
	g, err := NewGame(newPlayers(), 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
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

func TestGame_AllChecksToShowdown(t *testing.T) {
	g, err := NewGame(newPlayers(), 0, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	// preflop: first player bets, second player calls
	u1 := g.Players[g.TurnPos].UserID
	if err := g.ApplyAction(u1, "bet", 10); err != nil {
		t.Fatalf("preflop bet failed: %v", err)
	}
	u2 := g.Players[g.TurnPos].UserID
	if err := g.ApplyAction(u2, "call", 0); err != nil {
		t.Fatalf("preflop call failed: %v", err)
	}
	// after preflop, check through remaining stages
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
