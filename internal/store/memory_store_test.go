package store

import (
	"context"
	"testing"
	"time"

	"texas_yu/internal/ai"
)

type stubAIService struct {
	decisionFn  func(ctx context.Context, input ai.DecisionInput) (ai.Decision, error)
	summaryFn   func(ctx context.Context, input ai.SummaryInput) (ai.Summary, error)
	decideCount int
	sumCount    int
}

func (s *stubAIService) Enabled() bool { return true }

func (s *stubAIService) DecideAction(ctx context.Context, input ai.DecisionInput) (ai.Decision, error) {
	s.decideCount++
	if s.decisionFn != nil {
		return s.decisionFn(ctx, input)
	}
	return ai.Decision{Action: "check", Amount: 0}, nil
}

func (s *stubAIService) SummarizeHand(ctx context.Context, input ai.SummaryInput) (ai.Summary, error) {
	s.sumCount++
	if s.summaryFn != nil {
		return s.summaryFn(ctx, input)
	}
	return ai.Summary{HandSummary: "ok", OpponentProfiles: map[string]ai.Profile{}}, nil
}

func TestStore_RoomLifecycleAndVersionConflict(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")

	room := s.CreateRoom(owner, "r1", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	r, ok := s.GetRoom(room.RoomID)
	if !ok {
		t.Fatal("room not found")
	}
	version := r.StateVersion
	turnUser := r.Game.Players[r.Game.TurnPos].UserID

	if _, err := s.ApplyAction(room.RoomID, turnUser, "a1", "call", 0, version-1); err == nil {
		t.Fatalf("expected version conflict")
	}
	if _, err := s.ApplyAction(room.RoomID, turnUser, "a2", "call", 0, version); err != nil {
		t.Fatalf("expected success action, got err=%v", err)
	}
}

func TestStore_RevealAfterFinished_SucceedsAndBumpsVersion(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")

	room := s.CreateRoom(owner, "r3", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	r, _ := s.GetRoom(room.RoomID)
	turnUser := r.Game.Players[r.Game.TurnPos].UserID
	if _, err := s.ApplyAction(room.RoomID, turnUser, "fold1", "fold", 0, r.StateVersion); err != nil {
		t.Fatal(err)
	}
	r, _ = s.GetRoom(room.RoomID)
	version := r.StateVersion
	if r.Game.Stage != "finished" {
		t.Fatalf("expected finished stage, got %s", r.Game.Stage)
	}
	updated, err := s.ApplyReveal(room.RoomID, owner.UserID, "reveal1", 1, version)
	if err != nil {
		t.Fatalf("expected reveal success, got %v", err)
	}
	if updated.StateVersion != version+1 {
		t.Fatalf("expected version %d, got %d", version+1, updated.StateVersion)
	}
	for _, gp := range updated.Game.Players {
		if gp.UserID == owner.UserID && gp.RevealMask != 1 {
			t.Fatalf("expected owner reveal mask 1, got %d", gp.RevealMask)
		}
	}
}

func TestStore_RevealBeforeFinished_Fails(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")

	room := s.CreateRoom(owner, "r4", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	r, _ := s.GetRoom(room.RoomID)
	if _, err := s.ApplyReveal(room.RoomID, owner.UserID, "reveal2", 1, r.StateVersion); err == nil {
		t.Fatalf("expected reveal before finished to fail")
	}
}

func TestStore_RevealVersionConflict(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")

	room := s.CreateRoom(owner, "r5", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	r, _ := s.GetRoom(room.RoomID)
	turnUser := r.Game.Players[r.Game.TurnPos].UserID
	if _, err := s.ApplyAction(room.RoomID, turnUser, "fold2", "fold", 0, r.StateVersion); err != nil {
		t.Fatal(err)
	}
	r, _ = s.GetRoom(room.RoomID)
	if _, err := s.ApplyReveal(room.RoomID, owner.UserID, "reveal3", 2, r.StateVersion-1); err == nil {
		t.Fatalf("expected reveal version conflict")
	}
}

func TestStore_QuickChatFlowCooldownAndDedup(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "qc", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}

	r, _ := s.GetRoom(room.RoomID)
	beforeVersion := r.StateVersion

	updatedRoom, event, retryAfter, err := s.SendQuickChat(room.RoomID, owner.UserID, "qc-1", "nh")
	if err != nil {
		t.Fatalf("expected send quick chat success, got %v", err)
	}
	if event == nil || event.EventID == 0 {
		t.Fatalf("expected event id assigned")
	}
	if retryAfter != 0 {
		t.Fatalf("expected retryAfter 0, got %d", retryAfter)
	}
	if updatedRoom.StateVersion != beforeVersion {
		t.Fatalf("expected quick chat not to bump state version, got %d want %d", updatedRoom.StateVersion, beforeVersion)
	}

	_, duplicate, _, err := s.SendQuickChat(room.RoomID, owner.UserID, "qc-1", "nh")
	if err != nil {
		t.Fatalf("expected duplicate action id to be idempotent, got %v", err)
	}
	if duplicate != nil {
		t.Fatalf("expected duplicate action id not to create new event")
	}

	if _, _, retryAfter2, err := s.SendQuickChat(room.RoomID, owner.UserID, "qc-2", "gg"); err == nil {
		t.Fatalf("expected cooldown error")
	} else {
		if err.Error() != "quick chat cooldown" {
			t.Fatalf("unexpected error: %v", err)
		}
		if retryAfter2 <= 0 {
			t.Fatalf("expected retryAfter > 0")
		}
	}

	_, _, _, err = s.SendQuickChat(room.RoomID, guest.UserID, "qc-3", "gg")
	if err != nil {
		t.Fatalf("expected another player can send, got %v", err)
	}

	_, events, latestID, _, err := s.ListQuickChats(room.RoomID, 0)
	if err != nil {
		t.Fatalf("list quick chats failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if latestID < events[len(events)-1].EventID {
		t.Fatalf("latest event id should be >= last event id")
	}
}

func TestStore_QuickChatValidation(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	room := s.CreateRoom(owner, "qc2", 10, 10)

	if _, _, _, err := s.SendQuickChat(room.RoomID, owner.UserID, "x-1", ""); err == nil {
		t.Fatalf("expected invalid phrase error")
	}
	if _, _, _, err := s.SendQuickChat(room.RoomID, owner.UserID, "x-2", "free-text"); err == nil {
		t.Fatalf("expected invalid phrase error")
	}
	if _, _, _, err := s.SendQuickChat(room.RoomID, "unknown-user", "x-3", "nh"); err == nil {
		t.Fatalf("expected user not in room error")
	}
}

func TestStore_QuickChatDoesNotCauseActionVersionConflict(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "qc3", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	r, _ := s.GetRoom(room.RoomID)
	expectedVersion := r.StateVersion
	turnUser := r.Game.Players[r.Game.TurnPos].UserID

	if _, _, _, err := s.SendQuickChat(room.RoomID, owner.UserID, "qc-share-ver", "nh"); err != nil {
		t.Fatalf("quick chat failed: %v", err)
	}

	if _, err := s.ApplyAction(room.RoomID, turnUser, "qc-action-after-chat", "call", 0, expectedVersion); err != nil {
		t.Fatalf("expected action success with pre-chat version, got %v", err)
	}
}

func TestStore_LeaveRoomAndNextHand(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")

	room := s.CreateRoom(owner, "r2", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.LeaveRoom(room.RoomID, guest.UserID); err != nil {
		t.Fatalf("leave room failed: %v", err)
	}

	r, ok := s.GetRoom(room.RoomID)
	if !ok {
		t.Fatal("room not found")
	}
	if len(r.Players) != 1 {
		t.Fatalf("expected 1 player left, got %d", len(r.Players))
	}
	if r.Status != RoomWaiting {
		t.Fatalf("expected waiting after only one active player, got %s", r.Status)
	}
	if r.Game == nil || r.Game.Stage != "finished" {
		t.Fatalf("expected finished game after leave")
	}

	newGuest := s.CreateSession("new-guest")
	if _, err := s.JoinRoom(room.RoomID, newGuest); err != nil {
		t.Fatalf("join room failed: %v", err)
	}
	if _, err := s.NextHand(room.RoomID, newGuest.UserID); err == nil {
		t.Fatalf("expected non-owner next hand to fail")
	}
	if _, err := s.NextHand(room.RoomID, owner.UserID); err != nil {
		t.Fatalf("next hand failed: %v", err)
	}

	r, _ = s.GetRoom(room.RoomID)
	if r.Status != RoomPlaying {
		t.Fatalf("expected room playing after next hand, got %s", r.Status)
	}
	if r.Game == nil || r.Game.Stage != "preflop" {
		t.Fatalf("expected preflop after next hand")
	}
}

func TestStore_AddRemoveAIOwnerOnlyAndState(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.AddAI(room.RoomID, guest.UserID, "bot-1"); err == nil {
		t.Fatalf("expected non-owner add ai fail")
	}
	if _, _, err := s.AddAI(room.RoomID, owner.UserID, "bot-1"); err != nil {
		t.Fatalf("owner add ai failed: %v", err)
	}
	if _, _, err := s.AddAI(room.RoomID, owner.UserID, "bot-2"); err != nil {
		t.Fatalf("owner add second ai failed: %v", err)
	}

	r, _ := s.GetRoom(room.RoomID)
	aiCount := 0
	for _, p := range r.Players {
		if p.IsAI {
			aiCount++
		}
	}
	if aiCount != 2 {
		t.Fatalf("expected 2 ai players, got %d", aiCount)
	}

	var aiUserID string
	for _, p := range r.Players {
		if p.IsAI {
			aiUserID = p.UserID
			break
		}
	}
	if aiUserID == "" {
		t.Fatalf("missing ai user id")
	}
	if _, err := s.RemoveAI(room.RoomID, guest.UserID, aiUserID); err == nil {
		t.Fatalf("expected non-owner remove ai fail")
	}
	if _, err := s.RemoveAI(room.RoomID, owner.UserID, aiUserID); err != nil {
		t.Fatalf("owner remove ai failed: %v", err)
	}
}

func TestStore_NoHumansRoomDeletedEvenWithAIs(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, _, err := s.AddAI(room.RoomID, owner.UserID, "bot"); err != nil {
		t.Fatal(err)
	}

	out, err := s.LeaveRoom(room.RoomID, owner.UserID)
	if err != nil {
		t.Fatalf("leave failed: %v", err)
	}
	if out != nil {
		t.Fatalf("expected room deleted")
	}
	if _, ok := s.GetRoom(room.RoomID); ok {
		t.Fatalf("room should not exist")
	}
}

func TestStore_AITurnInputContainsHoleCardsWithoutOpponentsCards(t *testing.T) {
	captured := make(chan ai.DecisionInput, 1)
	stub := &stubAIService{}
	stub.decisionFn = func(_ context.Context, input ai.DecisionInput) (ai.Decision, error) {
		select {
		case captured <- input:
		default:
		}
		if containsAction(input.AllowedActions, "check") {
			return ai.Decision{Action: "check", Amount: 0}, nil
		}
		if containsAction(input.AllowedActions, "call") {
			return ai.Decision{Action: "call", Amount: 0}, nil
		}
		if containsAction(input.AllowedActions, "fold") {
			return ai.Decision{Action: "fold", Amount: 0}, nil
		}
		if containsAction(input.AllowedActions, "allin") {
			return ai.Decision{Action: "allin", Amount: 0}, nil
		}
		return ai.Decision{Action: "check", Amount: 0}, nil
	}

	s := NewMemoryStore(Options{AI: stub})
	owner := s.CreateSession("owner")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, _, err := s.AddAI(room.RoomID, owner.UserID, "bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	r, ok := s.GetRoom(room.RoomID)
	if !ok || r == nil || r.Game == nil {
		t.Fatalf("room/game missing")
	}
	for i := 0; i < 8; i++ {
		r, ok = s.GetRoom(room.RoomID)
		if !ok || r == nil || r.Game == nil {
			t.Fatalf("room/game missing")
		}
		if r.Game.Players[r.Game.TurnPos].IsAI {
			break
		}
		turn := r.Game.Players[r.Game.TurnPos]
		action := "check"
		if r.Game.RoundBet-turn.RoundContrib > 0 {
			action = "call"
		}
		if _, err := s.ApplyAction(room.RoomID, turn.UserID, "advance-to-ai-"+time.Now().Format("150405.000000"), action, 0, r.StateVersion); err != nil {
			t.Fatalf("failed to advance to ai turn: %v", err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case input := <-captured:
			if len(input.HoleCards) != 2 {
				t.Fatalf("expected ai hole cards length 2, got %d", len(input.HoleCards))
			}
			if input.PreflopTier == "" {
				t.Fatalf("expected preflop tier set")
			}
			if input.MadeHandStrength == "" {
				t.Fatalf("expected made hand strength set")
			}
			if len(input.DrawFlags) == 0 {
				t.Fatalf("expected draw flags set")
			}
			r, ok := s.GetRoom(room.RoomID)
			if !ok || r == nil || r.Game == nil {
				t.Fatalf("room/game missing")
			}
			for _, gp := range r.Game.Players {
				if gp.UserID == input.AIUserID {
					continue
				}
				for _, card := range gp.HoleCards {
					cardText := cardToText(card)
					for _, seen := range input.HoleCards {
						if seen == cardText {
							t.Fatalf("opponent hole card leaked into input: %s", cardText)
						}
					}
				}
			}
			return
		default:
			if time.Now().After(deadline) {
				t.Fatalf("did not capture ai decision input in time")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestStore_FallbackDecision_StrongHandBetsWhenAllowed(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		AllowedActions:   []string{"bet", "check", "fold"},
		RoundBet:         10,
		MinBet:           20,
		MinRaise:         30,
		Stack:            120,
		Pot:              100,
		CallAmount:       10,
		MadeHandStrength: "monster",
		DrawFlags:        []string{"none"},
	})
	if decision.Action != "bet" {
		t.Fatalf("expected bet for monster hand, got %s", decision.Action)
	}
	if decision.Amount < 30 {
		t.Fatalf("expected bet amount >= minRaise, got %d", decision.Amount)
	}
	if decision.Amount > 120 {
		t.Fatalf("expected bet amount <= stack, got %d", decision.Amount)
	}
}

func TestStore_FallbackDecision_BetAmountWithinStackAndMin(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		AllowedActions:   []string{"bet", "fold"},
		RoundBet:         0,
		MinBet:           40,
		MinRaise:         0,
		Stack:            45,
		Pot:              20,
		CallAmount:       0,
		MadeHandStrength: "strong",
		DrawFlags:        []string{"none"},
	})
	if decision.Action != "bet" {
		t.Fatalf("expected bet action, got %s", decision.Action)
	}
	if decision.Amount < 40 {
		t.Fatalf("expected bet amount >= minBet, got %d", decision.Amount)
	}
	if decision.Amount > 45 {
		t.Fatalf("expected bet amount <= stack, got %d", decision.Amount)
	}
}

func TestStore_AITurnAutoActionWithFallbackAndSummary(t *testing.T) {
	stub := &stubAIService{}
	stub.decisionFn = func(_ context.Context, input ai.DecisionInput) (ai.Decision, error) {
		return ai.Decision{Action: "invalid_action", Amount: -1}, nil
	}
	stub.summaryFn = func(_ context.Context, input ai.SummaryInput) (ai.Summary, error) {
		profiles := map[string]ai.Profile{}
		for _, p := range input.Players {
			if !p.IsAI {
				profiles[p.UserID] = ai.Profile{Style: "tight", Tendencies: []string{"calls"}, Advice: "pressure"}
			}
		}
		return ai.Summary{HandSummary: "ai summary", OpponentProfiles: profiles}, nil
	}

	s := NewMemoryStore(Options{AI: stub})
	owner := s.CreateSession("owner")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, _, err := s.AddAI(room.RoomID, owner.UserID, "bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	r, _ := s.GetRoom(room.RoomID)
	if r == nil || r.Game == nil {
		t.Fatalf("game missing")
	}
	if r.Game.Players[r.Game.TurnPos].UserID == owner.UserID {
		if _, err := s.ApplyAction(room.RoomID, owner.UserID, "owner-advance", "call", 0, r.StateVersion); err != nil {
			t.Fatalf("owner advance action failed: %v", err)
		}
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		r, ok := s.GetRoom(room.RoomID)
		if !ok || r == nil || r.Game == nil {
			t.Fatalf("room/game missing")
		}
		if r.Game.TurnPos < len(r.Game.Players) && r.Game.Players[r.Game.TurnPos].IsAI {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ai did not get turn in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	r, _ = s.GetRoom(room.RoomID)
	for r != nil && r.Game != nil && r.Game.Stage != "finished" {
		turnUser := r.Game.Players[r.Game.TurnPos].UserID
		_, err := s.ApplyAction(room.RoomID, turnUser, "force-finish-"+turnUser+time.Now().Format("150405.000000"), "fold", 0, r.StateVersion)
		if err != nil {
			break
		}
		r, _ = s.GetRoom(room.RoomID)
	}

	deadline = time.Now().Add(2 * time.Second)
	for {
		r2, ok := s.GetRoom(room.RoomID)
		if !ok || r2 == nil {
			t.Fatalf("room missing")
		}
		found := false
		for _, p := range r2.Players {
			if p.IsAI {
				mem := r2.AIMemory[p.UserID]
				if mem != nil && mem.LastSummarizedHand > 0 {
					found = true
				}
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ai summary not written in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStore_SummaryTriggeredOnLeaveFinish(t *testing.T) {
	stub := &stubAIService{}
	stub.summaryFn = func(_ context.Context, _ ai.SummaryInput) (ai.Summary, error) {
		return ai.Summary{HandSummary: "leave summary", OpponentProfiles: map[string]ai.Profile{}}, nil
	}

	s := NewMemoryStore(Options{AI: stub})
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, _, err := s.AddAI(room.RoomID, owner.UserID, "bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	// First fold guest, then owner leaves to force last-standing finish by leave path.
	r, _ := s.GetRoom(room.RoomID)
	if r == nil || r.Game == nil {
		t.Fatalf("game missing")
	}
	for i := 0; i < 8 && r.Game.Stage != "finished"; i++ {
		turnUser := r.Game.Players[r.Game.TurnPos].UserID
		actionID := "prep-" + turnUser + time.Now().Format("150405.000000")
		action := "fold"
		if turnUser != guest.UserID {
			action = "check"
			for _, gp := range r.Game.Players {
				if gp.UserID == turnUser {
					if r.Game.RoundBet-gp.RoundContrib > 0 {
						action = "call"
					}
					break
				}
			}
		}
		if _, err := s.ApplyAction(room.RoomID, turnUser, actionID, action, 0, r.StateVersion); err != nil {
			break
		}
		r, _ = s.GetRoom(room.RoomID)
		if r == nil || r.Game == nil {
			break
		}
		guestFolded := false
		for _, gp := range r.Game.Players {
			if gp.UserID == guest.UserID {
				guestFolded = gp.Folded
				break
			}
		}
		if guestFolded {
			break
		}
	}

	if _, err := s.LeaveRoom(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		r2, ok := s.GetRoom(room.RoomID)
		if !ok || r2 == nil {
			t.Fatalf("room missing")
		}
		found := false
		for _, p := range r2.Players {
			if p.IsAI {
				mem := r2.AIMemory[p.UserID]
				if mem != nil && mem.LastSummarizedHand > 0 {
					found = true
				}
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("summary not written after leave-finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func containsAction(actions []string, expected string) bool {
	for _, action := range actions {
		if action == expected {
			return true
		}
	}
	return false
}
