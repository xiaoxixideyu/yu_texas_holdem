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

func TestStore_ToggleAIManaged_BlocksManualThenAllowsAfterCancel(t *testing.T) {
	releaseDecision := make(chan struct{})
	stub := &stubAIService{}
	stub.decisionFn = func(_ context.Context, input ai.DecisionInput) (ai.Decision, error) {
		<-releaseDecision
		if containsAction(input.AllowedActions, "check") {
			return ai.Decision{Action: "check", Amount: 0}, nil
		}
		if containsAction(input.AllowedActions, "call") {
			return ai.Decision{Action: "call", Amount: 0}, nil
		}
		return ai.Decision{Action: "fold", Amount: 0}, nil
	}

	s := NewMemoryStore(Options{AI: stub})
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	r, ok := s.GetRoom(room.RoomID)
	if !ok || r == nil || r.Game == nil {
		t.Fatalf("room/game missing")
	}
	target := r.Game.Players[r.Game.TurnPos]
	targetUserID := target.UserID
	if target.IsAI {
		t.Fatalf("expected human turn player")
	}

	enabledRoom, err := s.SetPlayerAIManaged(room.RoomID, targetUserID, true)
	if err != nil {
		t.Fatalf("enable ai managed failed: %v", err)
	}
	if enabledRoom == nil {
		t.Fatalf("enable ai managed returned nil room")
	}
	if _, err := s.ApplyAction(room.RoomID, targetUserID, "managed-blocked", "fold", 0, enabledRoom.StateVersion); err == nil || err.Error() != "player is ai-managed" {
		t.Fatalf("expected player is ai-managed error, got %v", err)
	}

	disabledRoom, err := s.SetPlayerAIManaged(room.RoomID, targetUserID, false)
	if err != nil {
		t.Fatalf("disable ai managed failed: %v", err)
	}
	if disabledRoom == nil {
		t.Fatalf("disable ai managed returned nil room")
	}

	action := "check"
	diff := 0
	if r2, ok := s.GetRoom(room.RoomID); ok && r2 != nil && r2.Game != nil {
		for _, gp := range r2.Game.Players {
			if gp.UserID == targetUserID {
				diff = r2.Game.RoundBet - gp.RoundContrib
				break
			}
		}
	}
	if diff > 0 {
		action = "call"
	}
	if _, err := s.ApplyAction(room.RoomID, targetUserID, "managed-unblocked", action, 0, disabledRoom.StateVersion); err != nil {
		t.Fatalf("expected manual action after cancel managed to succeed, got %v", err)
	}

	close(releaseDecision)
}

func TestStore_ToggleAIManaged_RequiresEnabledAIService(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPlayerAIManaged(room.RoomID, owner.UserID, true); err == nil || err.Error() != "ai service disabled" {
		t.Fatalf("expected ai service disabled error, got %v", err)
	}
}

func TestStore_ToggleAIManaged_CurrentTurnActsByAI(t *testing.T) {
	decisionCalled := make(chan struct{}, 1)
	stub := &stubAIService{}
	stub.decisionFn = func(_ context.Context, input ai.DecisionInput) (ai.Decision, error) {
		select {
		case decisionCalled <- struct{}{}:
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
		return ai.Decision{Action: "allin", Amount: 0}, nil
	}

	s := NewMemoryStore(Options{AI: stub})
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	r, ok := s.GetRoom(room.RoomID)
	if !ok || r == nil || r.Game == nil {
		t.Fatalf("room/game missing")
	}
	turn := r.Game.Players[r.Game.TurnPos]
	if turn.IsAI {
		t.Fatalf("expected human turn player")
	}
	managedRoom, err := s.SetPlayerAIManaged(room.RoomID, turn.UserID, true)
	if err != nil {
		t.Fatalf("enable ai managed failed: %v", err)
	}
	startVersion := managedRoom.StateVersion

	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case <-decisionCalled:
		default:
		}

		latest, ok := s.GetRoom(room.RoomID)
		if !ok || latest == nil || latest.Game == nil {
			t.Fatalf("room/game missing while waiting ai action")
		}
		acted := false
		for _, gp := range latest.Game.Players {
			if gp.UserID == turn.UserID {
				acted = gp.LastAction != ""
				break
			}
		}
		if latest.StateVersion > startVersion && acted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ai did not act for managed player in time: version=%d startVersion=%d", latest.StateVersion, startVersion)
		}
		time.Sleep(10 * time.Millisecond)
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
			if input.PreflopPosition == "" {
				t.Fatalf("expected preflop position set")
			}
			if input.EffectiveStackBB <= 0 {
				t.Fatalf("expected effective stack bb > 0, got %.2f", input.EffectiveStackBB)
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
		Stage:            "river",
		RoundBet:         20,
		MinBet:           20,
		MinRaise:         40,
		Stack:            45,
		Pot:              120,
		CallAmount:       20,
		MadeHandStrength: "monster",
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

func TestStore_FallbackDecision_MixedBetSizingByStateVersion(t *testing.T) {
	base := ai.DecisionInput{
		RoomID:           "room-mix",
		HandID:           77,
		AIUserID:         "ai-1",
		Stage:            "river",
		AllowedActions:   []string{"bet", "call", "fold"},
		RoundBet:         40,
		MinRaise:         80,
		Stack:            5000,
		Pot:              1000,
		CallAmount:       40,
		MadeHandStrength: "monster",
		DrawFlags:        []string{"none"},
		CommunityCards:   []string{"AH", "KD", "7C", "2S", "2D"},
		HoleCards:        []string{"AS", "AD"},
	}

	input1 := base
	input1.StateVersion = 200
	input2 := base
	input2.StateVersion = 201

	decision1 := fallbackDecision(input1)
	decision2 := fallbackDecision(input2)
	if decision1.Action != "bet" || decision2.Action != "bet" {
		t.Fatalf("expected both decisions to bet, got %s and %s", decision1.Action, decision2.Action)
	}
	if decision1.Amount == decision2.Amount {
		t.Fatalf("expected mixed bet sizing across states, got same amount %d", decision1.Amount)
	}
}

func TestStore_FallbackDecision_PreflopUTGFacingRaiseFoldsTrash(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		RoomID:             "preflop-utg-fold",
		AIUserID:           "ai-1",
		Stage:              "preflop",
		AllowedActions:     []string{"call", "fold"},
		RoundBet:           30,
		OpenBetMin:         10,
		BetMin:             10,
		CallAmount:         30,
		Stack:              1000,
		Pot:                75,
		PreflopTier:        "trash",
		PreflopPosition:    "utg",
		EffectiveStackBB:   100,
		PreflopFacingRaise: true,
		HoleCards:          []string{"7C", "2D"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false},
			{UserID: "p-2", Folded: false},
		},
	})
	if decision.Action != "fold" {
		t.Fatalf("expected utg trash hand to fold vs raise, got %s", decision.Action)
	}
}

func TestStore_FallbackDecision_PreflopButtonOpensStrong(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		RoomID:             "preflop-btn-open",
		AIUserID:           "ai-1",
		Stage:              "preflop",
		AllowedActions:     []string{"bet", "call", "fold"},
		RoundBet:           10,
		OpenBetMin:         10,
		BetMin:             10,
		CallAmount:         10,
		MinRaise:           20,
		Stack:              1000,
		Pot:                15,
		PreflopTier:        "strong",
		PreflopPosition:    "btn",
		EffectiveStackBB:   90,
		PreflopFacingRaise: false,
		HoleCards:          []string{"AS", "QS"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false},
			{UserID: "p-2", Folded: false},
		},
	})
	if decision.Action != "bet" {
		t.Fatalf("expected button strong hand to open bet, got %s", decision.Action)
	}
	if decision.Amount < 20 {
		t.Fatalf("expected open amount >= min raise, got %d", decision.Amount)
	}
}

func TestStore_FallbackDecision_PreflopShortStackJamsPremium(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		RoomID:             "preflop-short-jam",
		AIUserID:           "ai-1",
		Stage:              "preflop",
		AllowedActions:     []string{"bet", "call", "allin", "fold"},
		RoundBet:           30,
		OpenBetMin:         10,
		BetMin:             10,
		CallAmount:         20,
		MinRaise:           30,
		Stack:              90,
		Pot:                75,
		PreflopTier:        "premium",
		PreflopPosition:    "btn",
		EffectiveStackBB:   9,
		PreflopFacingRaise: true,
		HoleCards:          []string{"AS", "AH"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false},
		},
	})
	if decision.Action != "allin" {
		t.Fatalf("expected short-stack premium to jam, got %s", decision.Action)
	}
}

func TestStore_EstimateMonteCarloEquity_VisibleInfoOnly(t *testing.T) {
	input := ai.DecisionInput{
		AIUserID:       "ai-1",
		Stage:          "river",
		HoleCards:      []string{"TH", "3D"},
		CommunityCards: []string{"AH", "KH", "QH", "JH", "2C"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false},
		},
	}
	eq, ok := estimateMonteCarloEquity(input)
	if !ok {
		t.Fatalf("expected monte carlo equity to be available")
	}
	if eq < 0.98 {
		t.Fatalf("expected near-nut equity, got %.4f", eq)
	}
}

func TestStore_GuardAIDecision_RejectsNegativeEVCall(t *testing.T) {
	input := ai.DecisionInput{
		AIUserID:       "ai-1",
		Stage:          "river",
		Pot:            100,
		CallAmount:     120,
		Stack:          500,
		RoundBet:       120,
		AllowedActions: []string{"call", "fold"},
		HoleCards:      []string{"3S", "4C"},
		CommunityCards: []string{"AS", "KD", "7C", "2H", "9D"},
		DrawFlags:      []string{"none"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false},
		},
	}
	decision := guardAIDecision(input, ai.Decision{Action: "call", Amount: 0}, ai.Decision{Action: "fold", Amount: 0})
	if decision.Action != "fold" {
		t.Fatalf("expected guard to reject bad call, got %s", decision.Action)
	}
}

func TestStore_GuardAIDecision_RejectsBadFold(t *testing.T) {
	input := ai.DecisionInput{
		AIUserID:       "ai-1",
		Stage:          "river",
		Pot:            150,
		CallAmount:     10,
		Stack:          800,
		RoundBet:       10,
		AllowedActions: []string{"call", "fold"},
		HoleCards:      []string{"AS", "AD"},
		CommunityCards: []string{"2C", "2D", "9H", "TS", "KD"},
		DrawFlags:      []string{"none"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false},
		},
	}
	decision := guardAIDecision(input, ai.Decision{Action: "fold", Amount: 0}, ai.Decision{Action: "call", Amount: 0})
	if decision.Action != "call" {
		t.Fatalf("expected guard to reject bad fold, got %s", decision.Action)
	}
}

func TestStore_OpponentStatsStrategyAdjustments_FromNumericStats(t *testing.T) {
	foldEqAdj, valueAdj, trapAdj := opponentStatsStrategyAdjustments(map[string]ai.OpponentStats{
		"villain-1": {
			Hands:            30,
			VPIP:             0.48,
			PFR:              0.12,
			AggressionFactor: 1.2,
			FoldRate:         0.12,
			ShowdownRate:     0.40,
			ShowdownWinRate:  0.42,
		},
		"villain-2": {
			Hands:            26,
			VPIP:             0.19,
			PFR:              0.31,
			AggressionFactor: 2.9,
			FoldRate:         0.41,
			ShowdownRate:     0.24,
			ShowdownWinRate:  0.56,
		},
	})
	if valueAdj <= 0 {
		t.Fatalf("expected positive value adjustment, got %.4f", valueAdj)
	}
	if trapAdj <= 0 {
		t.Fatalf("expected positive trap adjustment, got %.4f", trapAdj)
	}
	if foldEqAdj == 0 {
		t.Fatalf("expected non-zero fold equity adjustment")
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

	r3, ok := s.GetRoom(room.RoomID)
	if !ok || r3 == nil {
		t.Fatalf("room missing")
	}
	hasStats := false
	for _, p := range r3.Players {
		if !p.IsAI {
			continue
		}
		mem := r3.AIMemory[p.UserID]
		if mem == nil || len(mem.OpponentStats) == 0 {
			continue
		}
		if ownerStats := mem.OpponentStats[owner.UserID]; ownerStats != nil && ownerStats.Hands > 0 {
			hasStats = true
			break
		}
	}
	if !hasStats {
		t.Fatalf("expected opponent stats to be recorded for ai memory")
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

func TestStore_SpectatorJoinIdempotentAndPlayerNoDup(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	spectator := s.CreateSession("spectator")
	player := s.CreateSession("player")

	room := s.CreateRoom(owner, "spec", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, player); err != nil {
		t.Fatal(err)
	}

	r1, err := s.SpectateRoom(room.RoomID, spectator)
	if err != nil {
		t.Fatalf("spectate failed: %v", err)
	}
	if len(r1.Spectators) != 1 {
		t.Fatalf("expected 1 spectator, got %d", len(r1.Spectators))
	}
	v1 := r1.StateVersion

	r2, err := s.SpectateRoom(room.RoomID, spectator)
	if err != nil {
		t.Fatalf("spectate idempotent failed: %v", err)
	}
	if len(r2.Spectators) != 1 {
		t.Fatalf("expected 1 spectator after idempotent call, got %d", len(r2.Spectators))
	}
	if r2.StateVersion != v1 {
		t.Fatalf("expected idempotent spectate not bump version, got %d want %d", r2.StateVersion, v1)
	}

	r3, err := s.SpectateRoom(room.RoomID, player)
	if err != nil {
		t.Fatalf("player spectate should no-op: %v", err)
	}
	if len(r3.Spectators) != 1 {
		t.Fatalf("expected spectator list unchanged for player, got %d", len(r3.Spectators))
	}
}

func TestStore_SpectatorReadOnlyOperationsDenied(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	spectator := s.CreateSession("spectator")

	room := s.CreateRoom(owner, "spec-ro", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SpectateRoom(room.RoomID, spectator); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	r, _ := s.GetRoom(room.RoomID)

	if _, err := s.ApplyAction(room.RoomID, spectator.UserID, "spec-act", "check", 0, r.StateVersion); err == nil || err.Error() != "spectator is read-only" {
		t.Fatalf("expected spectator action denied, got %v", err)
	}
	if _, err := s.ApplyReveal(room.RoomID, spectator.UserID, "spec-reveal", 1, r.StateVersion); err == nil || err.Error() != "spectator is read-only" {
		t.Fatalf("expected spectator reveal denied, got %v", err)
	}
	if _, err := s.NextHand(room.RoomID, spectator.UserID); err == nil || err.Error() != "spectator is read-only" {
		t.Fatalf("expected spectator next hand denied, got %v", err)
	}
	if _, _, err := s.AddAI(room.RoomID, spectator.UserID, "bot"); err == nil || err.Error() != "spectator is read-only" {
		t.Fatalf("expected spectator add ai denied, got %v", err)
	}
	if _, err := s.RemoveAI(room.RoomID, spectator.UserID, "ai-not-found"); err == nil || err.Error() != "spectator is read-only" {
		t.Fatalf("expected spectator remove ai denied, got %v", err)
	}
	if _, _, _, err := s.SendQuickChat(room.RoomID, spectator.UserID, "spec-chat", "nh"); err == nil || err.Error() != "spectator is read-only" {
		t.Fatalf("expected spectator send quick chat denied, got %v", err)
	}
}

func TestStore_SpectatorLeaveOnlyRemovesSpectator(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	spectator := s.CreateSession("spectator")

	room := s.CreateRoom(owner, "spec-leave", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SpectateRoom(room.RoomID, spectator); err != nil {
		t.Fatal(err)
	}

	rBefore, _ := s.GetRoom(room.RoomID)
	if len(rBefore.Players) != 2 || len(rBefore.Spectators) != 1 {
		t.Fatalf("unexpected setup players=%d spectators=%d", len(rBefore.Players), len(rBefore.Spectators))
	}
	versionBefore := rBefore.StateVersion

	rAfter, err := s.LeaveRoom(room.RoomID, spectator.UserID)
	if err != nil {
		t.Fatalf("spectator leave failed: %v", err)
	}
	if rAfter == nil {
		t.Fatalf("room should still exist after spectator leave")
	}
	if len(rAfter.Players) != 2 {
		t.Fatalf("expected players unchanged, got %d", len(rAfter.Players))
	}
	if len(rAfter.Spectators) != 0 {
		t.Fatalf("expected spectator removed, got %d", len(rAfter.Spectators))
	}
	if rAfter.StateVersion != versionBefore+1 {
		t.Fatalf("expected version bump by spectator leave")
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
