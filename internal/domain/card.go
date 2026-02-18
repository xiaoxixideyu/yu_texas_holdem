package domain

import (
	"math/rand"
	"time"
)

type Suit int

const (
	Clubs Suit = iota
	Diamonds
	Hearts
	Spades
)

type Card struct {
	Rank int // 2-14 (A=14)
	Suit Suit
}

func NewDeck() []Card {
	deck := make([]Card, 0, 52)
	for s := Clubs; s <= Spades; s++ {
		for r := 2; r <= 14; r++ {
			deck = append(deck, Card{Rank: r, Suit: s})
		}
	}
	return deck
}

func Shuffle(cards []Card) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(cards), func(i, j int) {
		cards[i], cards[j] = cards[j], cards[i]
	})
}
