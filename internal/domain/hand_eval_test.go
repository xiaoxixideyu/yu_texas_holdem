package domain

import "testing"

func TestEvaluateFive_RankingOrder(t *testing.T) {
	straightFlush := []Card{{10, Hearts}, {11, Hearts}, {12, Hearts}, {13, Hearts}, {14, Hearts}}
	fourKind := []Card{{9, Clubs}, {9, Diamonds}, {9, Hearts}, {9, Spades}, {2, Clubs}}

	a := EvaluateFive(straightFlush)
	b := EvaluateFive(fourKind)
	if CompareHandValue(a, b) <= 0 {
		t.Fatalf("expected straight flush > four of a kind")
	}
}

func TestBestOfSeven_SelectsBestHand(t *testing.T) {
	cards := []Card{
		{14, Spades}, {13, Spades}, // hole
		{12, Spades}, {11, Spades}, {10, Spades}, {2, Clubs}, {3, Diamonds}, // board
	}
	v, _, name := BestOfSeven(cards)
	if v.Category != 8 || name != "straight_flush" {
		t.Fatalf("expected straight_flush, got category=%d name=%s", v.Category, name)
	}
}
