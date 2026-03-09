package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"texas_yu/internal/ai"
	"texas_yu/internal/domain"
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

func TestStore_StartGame_SkipsPlayersWithZeroStack(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest1 := s.CreateSession("guest-1")
	guest2 := s.CreateSession("guest-2")

	room := s.CreateRoom(owner, "skip-zero", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinRoom(room.RoomID, guest2); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.rooms[room.RoomID].Players[0].Stack = 0
	s.mu.Unlock()

	updated, err := s.StartGame(room.RoomID, owner.UserID)
	if err != nil {
		t.Fatalf("start game failed: %v", err)
	}
	if updated.Game == nil {
		t.Fatalf("expected game initialized")
	}
	if len(updated.Game.Players) != 2 {
		t.Fatalf("expected 2 active players, got %d", len(updated.Game.Players))
	}
	for _, gp := range updated.Game.Players {
		if gp.UserID == owner.UserID {
			t.Fatalf("expected zero-stack owner to be excluded from game players")
		}
	}
	if updated.Game.Players[updated.Game.DealerPos].UserID != guest1.UserID {
		t.Fatalf("expected dealer to skip zero-stack seat and land on guest1")
	}
}

func TestStore_StartGame_RequiresAtLeastTwoPlayersWithChips(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")

	room := s.CreateRoom(owner, "need-chips", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	internal := s.rooms[room.RoomID]
	internal.Players[0].Stack = 0
	internal.Players[1].Stack = 0
	s.mu.Unlock()

	if _, err := s.StartGame(room.RoomID, owner.UserID); err == nil {
		t.Fatalf("expected start game to fail when all players are out of chips")
	}
}

func TestStore_NextHand_SkipsZeroStackPlayersAndRotatesDealer(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest1 := s.CreateSession("guest-1")
	guest2 := s.CreateSession("guest-2")

	room := s.CreateRoom(owner, "next-skip-zero", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinRoom(room.RoomID, guest2); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.rooms[room.RoomID].Players[0].Stack = 0
	s.mu.Unlock()

	started, err := s.StartGame(room.RoomID, owner.UserID)
	if err != nil {
		t.Fatalf("start game failed: %v", err)
	}
	firstDealer := started.Game.Players[started.Game.DealerPos].UserID
	if firstDealer != guest1.UserID {
		t.Fatalf("expected first dealer guest1, got %s", firstDealer)
	}

	firstTurnUser := started.Game.Players[started.Game.TurnPos].UserID
	finished, err := s.ApplyAction(room.RoomID, firstTurnUser, "finish-hand-by-fold", "fold", 0, started.StateVersion)
	if err != nil {
		t.Fatalf("finish first hand failed: %v", err)
	}
	if finished.Game == nil || finished.Game.Stage != "finished" {
		t.Fatalf("expected first hand finished before next hand")
	}

	next, err := s.NextHand(room.RoomID, owner.UserID)
	if err != nil {
		t.Fatalf("next hand failed: %v", err)
	}
	if next.Game == nil {
		t.Fatalf("expected next hand game initialized")
	}
	if len(next.Game.Players) != 2 {
		t.Fatalf("expected 2 active players in next hand, got %d", len(next.Game.Players))
	}
	for _, gp := range next.Game.Players {
		if gp.UserID == owner.UserID {
			t.Fatalf("expected zero-stack owner to stay excluded in next hand")
		}
	}
	secondDealer := next.Game.Players[next.Game.DealerPos].UserID
	if secondDealer != guest2.UserID {
		t.Fatalf("expected second dealer guest2 after rotation, got %s", secondDealer)
	}
}

func TestStore_StartAndNextHand_SkipMultipleZeroStackSeats(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest1 := s.CreateSession("guest-1")
	guest2 := s.CreateSession("guest-2")
	guest3 := s.CreateSession("guest-3")

	room := s.CreateRoom(owner, "multi-zero-seats", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinRoom(room.RoomID, guest2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinRoom(room.RoomID, guest3); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.rooms[room.RoomID].Players[0].Stack = 0
	s.rooms[room.RoomID].Players[1].Stack = 0
	s.mu.Unlock()

	first, err := s.StartGame(room.RoomID, owner.UserID)
	if err != nil {
		t.Fatalf("start game failed: %v", err)
	}
	if first.Game == nil {
		t.Fatalf("expected game initialized")
	}
	if len(first.Game.Players) != 2 {
		t.Fatalf("expected 2 active players, got %d", len(first.Game.Players))
	}
	firstDealer := first.Game.Players[first.Game.DealerPos].UserID
	if firstDealer != guest2.UserID {
		t.Fatalf("expected dealer to skip seat0/seat1 and land on guest2, got %s", firstDealer)
	}

	firstTurnUser := first.Game.Players[first.Game.TurnPos].UserID
	afterFirst, err := s.ApplyAction(room.RoomID, firstTurnUser, "multi-zero-finish-1", "fold", 0, first.StateVersion)
	if err != nil {
		t.Fatalf("finish first hand failed: %v", err)
	}
	if afterFirst.Game == nil || afterFirst.Game.Stage != "finished" {
		t.Fatalf("expected first hand finished")
	}

	second, err := s.NextHand(room.RoomID, owner.UserID)
	if err != nil {
		t.Fatalf("next hand failed: %v", err)
	}
	secondDealer := second.Game.Players[second.Game.DealerPos].UserID
	if secondDealer != guest3.UserID {
		t.Fatalf("expected dealer rotate to guest3, got %s", secondDealer)
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

func TestStore_ToggleAIManaged_AllowsOfflineLocalStrategy(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "room", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	managed, err := s.SetPlayerAIManaged(room.RoomID, owner.UserID, true)
	if err != nil {
		t.Fatalf("expected offline local strategy to be available, got %v", err)
	}
	if managed == nil {
		t.Fatalf("expected room after enabling offline local strategy")
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

func TestStore_AIManagedPlayerGetsSummaryAndProfiles(t *testing.T) {
	summaryCalled := make(chan ai.SummaryInput, 1)
	stub := &stubAIService{}
	stub.decisionFn = func(_ context.Context, input ai.DecisionInput) (ai.Decision, error) {
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
	stub.summaryFn = func(_ context.Context, input ai.SummaryInput) (ai.Summary, error) {
		select {
		case summaryCalled <- input:
		default:
		}
		profiles := map[string]ai.Profile{}
		for _, p := range input.Players {
			if p.UserID == input.AIUserID || p.IsAI {
				continue
			}
			profiles[p.UserID] = ai.Profile{Style: "tight", Tendencies: []string{"calls"}, Advice: "pressure"}
		}
		return ai.Summary{HandSummary: "managed summary", OpponentProfiles: profiles}, nil
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
	managedUserID := r.Game.Players[r.Game.TurnPos].UserID
	opponentUserID := owner.UserID
	if opponentUserID == managedUserID {
		opponentUserID = guest.UserID
	}
	managedRoom, err := s.SetPlayerAIManaged(room.RoomID, managedUserID, true)
	if err != nil {
		t.Fatalf("enable ai managed failed: %v", err)
	}
	startVersion := managedRoom.StateVersion

	deadline := time.Now().Add(2 * time.Second)
	for {
		latest, ok := s.GetRoom(room.RoomID)
		if !ok || latest == nil || latest.Game == nil {
			t.Fatalf("room/game missing while waiting ai action")
		}
		acted := false
		for _, gp := range latest.Game.Players {
			if gp.UserID == managedUserID {
				acted = gp.LastAction != ""
				break
			}
		}
		if latest.StateVersion > startVersion && acted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ai did not act for managed player in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	deadline = time.Now().Add(2 * time.Second)
	for {
		latest, ok := s.GetRoom(room.RoomID)
		if !ok || latest == nil || latest.Game == nil {
			t.Fatalf("room/game missing while finishing hand")
		}
		if latest.Game.Stage == "finished" {
			break
		}
		turn := latest.Game.Players[latest.Game.TurnPos]
		if turn.UserID == managedUserID {
			if time.Now().After(deadline) {
				t.Fatalf("managed hand did not finish in time")
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if _, err := s.ApplyAction(room.RoomID, turn.UserID, "finish-"+turn.UserID+time.Now().Format("150405.000000"), "fold", 0, latest.StateVersion); err != nil {
			t.Fatalf("force finish failed: %v", err)
		}
	}

	deadline = time.Now().Add(2 * time.Second)
	for {
		latest, ok := s.GetRoom(room.RoomID)
		if !ok || latest == nil {
			t.Fatalf("room missing")
		}
		mem := latest.AIMemory[managedUserID]
		if mem != nil && mem.LastSummarizedHand > 0 {
			if len(mem.HandSummaries) == 0 || mem.HandSummaries[len(mem.HandSummaries)-1] != "managed summary" {
				t.Fatalf("expected managed summary written, got %+v", mem.HandSummaries)
			}
			if mem.OpponentProfiles[opponentUserID] == nil {
				t.Fatalf("expected opponent profile recorded for managed player")
			}
			if mem.OpponentStats[opponentUserID] == nil || mem.OpponentStats[opponentUserID].Hands == 0 {
				t.Fatalf("expected opponent stats recorded for managed player")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed summary not written in time")
		}
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case input := <-summaryCalled:
		if input.AIUserID != managedUserID {
			t.Fatalf("expected summary for managed user %s, got %s", managedUserID, input.AIUserID)
		}
	default:
		t.Fatalf("expected summary callback for managed player")
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

func TestStore_ProfileStrategyAdjustments_RecognizesChineseDescriptors(t *testing.T) {
	foldEqAdj, valueAdj, trapAdj := profileStrategyAdjustments(map[string]ai.Profile{
		"villain-1": {
			Style:      "跟注站",
			Tendencies: []string{"爱跟到底"},
			Advice:     "多做价值下注",
		},
		"villain-2": {
			Style:      "紧凶",
			Tendencies: []string{"翻牌圈爱加注", "会慢打强牌"},
			Advice:     "别轻易偷鸡",
		},
		"villain-3": {
			Style:      "保守被动",
			Tendencies: []string{"弃牌偏多"},
			Advice:     "可以持续施压",
		},
	})
	if foldEqAdj <= 0 {
		t.Fatalf("expected positive fold-equity adjustment from Chinese profiles, got %.4f", foldEqAdj)
	}
	if valueAdj <= 0 {
		t.Fatalf("expected positive value adjustment from Chinese profiles, got %.4f", valueAdj)
	}
	if trapAdj <= 0 {
		t.Fatalf("expected positive trap adjustment from Chinese profiles, got %.4f", trapAdj)
	}
}

func TestStore_EstimateMonteCarloEquity_UsesVisibleRangeSignals(t *testing.T) {
	base := ai.DecisionInput{
		AIUserID:       "ai-1",
		Stage:          "flop",
		Pot:            95,
		RoundBet:       60,
		CallAmount:     60,
		HoleCards:      []string{"AH", "QH"},
		CommunityCards: []string{"AS", "7D", "2C"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "villain", Folded: false, LastAction: "bet", RoundContrib: 60},
		},
		RecentActionLog: []ai.ActionLog{
			{UserID: "villain", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "ai-1", Action: "call", Amount: 30, Stage: "preflop"},
			{UserID: "villain", Action: "bet", Amount: 60, Stage: "flop"},
		},
	}
	tight := base
	tight.OpponentStats = map[string]ai.OpponentStats{
		"villain": {
			Hands:            30,
			VPIP:             0.18,
			PFR:              0.14,
			AggressionFactor: 2.6,
			FoldRate:         0.24,
			ShowdownRate:     0.31,
			ShowdownWinRate:  0.59,
		},
	}
	loose := base
	loose.OpponentStats = map[string]ai.OpponentStats{
		"villain": {
			Hands:            30,
			VPIP:             0.49,
			PFR:              0.12,
			AggressionFactor: 1.1,
			FoldRate:         0.13,
			ShowdownRate:     0.39,
			ShowdownWinRate:  0.43,
		},
	}
	eqTight, ok := estimateMonteCarloEquity(tight)
	if !ok {
		t.Fatalf("expected tight equity estimate to be available")
	}
	eqLoose, ok := estimateMonteCarloEquity(loose)
	if !ok {
		t.Fatalf("expected loose equity estimate to be available")
	}
	if eqTight >= eqLoose {
		t.Fatalf("expected tighter aggressive line to lower hero equity, got tight=%.4f loose=%.4f", eqTight, eqLoose)
	}
}

func TestStore_FallbackDecision_PreflopFoldsMarginalHandVsTightRaise(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		RoomID:             "preflop-tight-open",
		AIUserID:           "ai-1",
		Stage:              "preflop",
		AllowedActions:     []string{"call", "fold"},
		RoundBet:           40,
		OpenBetMin:         10,
		BetMin:             10,
		CallAmount:         40,
		Stack:              1000,
		Pot:                55,
		PreflopTier:        "speculative",
		PreflopPosition:    "co",
		EffectiveStackBB:   100,
		PreflopFacingRaise: true,
		HoleCards:          []string{"KH", "TD"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "villain", Folded: false, LastAction: "bet", RoundContrib: 40},
			{UserID: "p-2", Folded: false},
		},
		RecentActionLog: []ai.ActionLog{
			{UserID: "villain", Action: "bet", Amount: 40, Stage: "preflop"},
		},
		OpponentStats: map[string]ai.OpponentStats{
			"villain": {
				Hands:            42,
				VPIP:             0.16,
				PFR:              0.13,
				AggressionFactor: 2.4,
				FoldRate:         0.29,
				ShowdownRate:     0.23,
				ShowdownWinRate:  0.57,
			},
		},
	})
	if decision.Action != "fold" {
		t.Fatalf("expected marginal offsuit broadway to fold vs tight raise, got %s", decision.Action)
	}
}

func TestStore_ProfileRangeBias_AdjustsTightVsLoose(t *testing.T) {
	tight, loose, aggressive, trap := profileRangeBias(ai.Profile{Style: "紧凶", Tendencies: []string{"爱加注", "会慢打"}, Advice: "价值下注为主"})
	if tight <= 0 || aggressive <= 0 || trap <= 0 {
		t.Fatalf("expected tight/aggressive/trap bias to be positive, got tight=%.4f aggressive=%.4f trap=%.4f", tight, aggressive, trap)
	}
	if loose != 0 {
		t.Fatalf("expected no loose bias for tight-aggressive profile, got %.4f", loose)
	}

	tight, loose, aggressive, trap = profileRangeBias(ai.Profile{Style: "跟注站", Tendencies: []string{"爱跟到底"}, Advice: "多做价值"})
	if loose <= 0 {
		t.Fatalf("expected loose bias for calling-station profile, got %.4f", loose)
	}
	if tight != 0 || aggressive != 0 || trap != 0 {
		t.Fatalf("expected no extra tight/aggressive/trap bias for pure station profile, got tight=%.4f aggressive=%.4f trap=%.4f", tight, aggressive, trap)
	}
}

func TestStore_OpponentHandWeight_ProfileCanWidenSpeculativeCalls(t *testing.T) {
	hole := []domain.Card{{Rank: 7, Suit: domain.Hearts}, {Rank: 6, Suit: domain.Hearts}}
	villain := ai.PlayerSnapshot{UserID: "villain", LastAction: "call"}
	summary := visibleActionSummary{PreflopCalls: 1}
	looseInput := ai.DecisionInput{
		Stage: "preflop",
		Profiles: map[string]ai.Profile{
			"villain": {Style: "跟注站", Tendencies: []string{"爱跟到底"}},
		},
	}
	tightInput := ai.DecisionInput{
		Stage: "preflop",
		Profiles: map[string]ai.Profile{
			"villain": {Style: "紧弱", Tendencies: []string{"保守", "弃牌偏多"}},
		},
	}
	looseWeight := opponentHandWeight(looseInput, villain, hole, summary, nil)
	tightWeight := opponentHandWeight(tightInput, villain, hole, summary, nil)
	if looseWeight <= tightWeight {
		t.Fatalf("expected loose profile to widen speculative suited connectors, got loose=%.4f tight=%.4f", looseWeight, tightWeight)
	}
}

func TestStore_RiverHeroBlockerScore_PrefersNutFlushBlocker(t *testing.T) {
	board := []domain.Card{
		{Rank: 13, Suit: domain.Hearts},
		{Rank: 12, Suit: domain.Hearts},
		{Rank: 7, Suit: domain.Clubs},
		{Rank: 2, Suit: domain.Diamonds},
		{Rank: 4, Suit: domain.Hearts},
	}
	withBlocker := riverHeroBlockerScore([]domain.Card{{Rank: 14, Suit: domain.Hearts}, {Rank: 9, Suit: domain.Spades}}, board, 1)
	withoutBlocker := riverHeroBlockerScore([]domain.Card{{Rank: 14, Suit: domain.Spades}, {Rank: 9, Suit: domain.Clubs}}, board, 1)
	if withBlocker <= withoutBlocker {
		t.Fatalf("expected nut flush blocker to score higher, got with=%.4f without=%.4f", withBlocker, withoutBlocker)
	}
}

func TestStore_FallbackDecision_RiverCallsBlockerBluffCatcherVsAggressiveBet(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		RoomID:           "river-blocker-call",
		AIUserID:         "ai-1",
		Stage:            "river",
		Pot:              100,
		RoundBet:         100,
		CallAmount:       100,
		Stack:            700,
		AllowedActions:   []string{"call", "fold"},
		HandCategory:     "one_pair",
		HandCategoryRank: 1,
		MadeHandStrength: "weak",
		DrawFlags:        []string{"none"},
		CommunityCards:   []string{"KH", "QH", "9C", "2D", "4H"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "villain", Folded: false, LastAction: "bet", RoundContrib: 100},
		},
		RecentActionLog: []ai.ActionLog{
			{UserID: "villain", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "ai-1", Action: "call", Amount: 30, Stage: "preflop"},
			{UserID: "villain", Action: "bet", Amount: 45, Stage: "flop"},
			{UserID: "ai-1", Action: "call", Amount: 45, Stage: "flop"},
			{UserID: "villain", Action: "bet", Amount: 100, Stage: "river"},
		},
		Profiles: map[string]ai.Profile{
			"villain": {Style: "松凶", Tendencies: []string{"爱偷鸡", "河牌会持续开火"}, Advice: "可抓诈唬"},
		},
		OpponentStats: map[string]ai.OpponentStats{
			"villain": {
				Hands:            34,
				VPIP:             0.44,
				PFR:              0.29,
				AggressionFactor: 3.1,
				FoldRate:         0.18,
				ShowdownRate:     0.27,
				ShowdownWinRate:  0.44,
			},
		},
		HoleCards: []string{"AH", "9D"},
	})
	if decision.Action != "call" {
		t.Fatalf("expected blocker bluff-catcher to call, got %s", decision.Action)
	}
}

func TestStore_FallbackDecision_RiverFoldsMediumPairVsPassiveOverbet(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		RoomID:           "river-passive-overbet-fold",
		AIUserID:         "ai-1",
		Stage:            "river",
		Pot:              100,
		RoundBet:         150,
		CallAmount:       150,
		Stack:            700,
		AllowedActions:   []string{"call", "fold"},
		HandCategory:     "one_pair",
		HandCategoryRank: 1,
		MadeHandStrength: "weak",
		DrawFlags:        []string{"none"},
		CommunityCards:   []string{"KH", "QH", "9C", "2D", "4H"},
		HoleCards:        []string{"AS", "9D"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "villain", Folded: false, LastAction: "bet", RoundContrib: 150},
		},
		RecentActionLog: []ai.ActionLog{
			{UserID: "villain", Action: "call", Amount: 10, Stage: "preflop"},
			{UserID: "ai-1", Action: "check", Amount: 0, Stage: "flop"},
			{UserID: "villain", Action: "check", Amount: 0, Stage: "flop"},
			{UserID: "ai-1", Action: "check", Amount: 0, Stage: "turn"},
			{UserID: "villain", Action: "bet", Amount: 150, Stage: "river"},
		},
		Profiles: map[string]ai.Profile{
			"villain": {Style: "保守被动", Tendencies: []string{"弃牌偏多", "价值线偏直"}, Advice: "大注多是真值"},
		},
		OpponentStats: map[string]ai.OpponentStats{
			"villain": {
				Hands:            41,
				VPIP:             0.18,
				PFR:              0.10,
				AggressionFactor: 0.8,
				FoldRate:         0.34,
				ShowdownRate:     0.35,
				ShowdownWinRate:  0.58,
			},
		},
	})
	if decision.Action != "fold" {
		t.Fatalf("expected medium pair to fold vs passive overbet, got %s", decision.Action)
	}
}

func TestStore_HeroHasInitiative_FromPreflopAggressor(t *testing.T) {
	if !heroHasInitiative(ai.DecisionInput{
		AIUserID: "ai-1",
		Stage:    "flop",
		RecentActionLog: []ai.ActionLog{
			{UserID: "ai-1", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "p-1", Action: "call", Amount: 30, Stage: "preflop"},
		},
	}) {
		t.Fatalf("expected hero to keep initiative from preflop raise")
	}
	if heroHasInitiative(ai.DecisionInput{
		AIUserID: "ai-1",
		Stage:    "flop",
		RecentActionLog: []ai.ActionLog{
			{UserID: "p-1", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "ai-1", Action: "call", Amount: 30, Stage: "preflop"},
		},
	}) {
		t.Fatalf("expected caller not to have initiative")
	}
}

func TestStore_BoardRangeAdvantage_FavorsDryHighBoardsForAggressor(t *testing.T) {
	dry := boardRangeAdvantage(ai.DecisionInput{
		AIUserID:        "ai-1",
		PreflopPosition: "btn",
		CommunityCards:  []string{"AH", "KD", "4C"},
		RecentActionLog: []ai.ActionLog{{UserID: "ai-1", Action: "bet", Amount: 30, Stage: "preflop"}, {UserID: "p-1", Action: "call", Amount: 30, Stage: "preflop"}},
		Players:         []ai.PlayerSnapshot{{UserID: "ai-1"}, {UserID: "p-1"}},
	}, []domain.Card{{Rank: 14, Suit: domain.Hearts}, {Rank: 13, Suit: domain.Diamonds}, {Rank: 4, Suit: domain.Clubs}})
	wet := boardRangeAdvantage(ai.DecisionInput{
		AIUserID:        "ai-1",
		PreflopPosition: "btn",
		CommunityCards:  []string{"9H", "8H", "7D"},
		RecentActionLog: []ai.ActionLog{{UserID: "ai-1", Action: "bet", Amount: 30, Stage: "preflop"}, {UserID: "p-1", Action: "call", Amount: 30, Stage: "preflop"}},
		Players:         []ai.PlayerSnapshot{{UserID: "ai-1"}, {UserID: "p-1"}},
	}, []domain.Card{{Rank: 9, Suit: domain.Hearts}, {Rank: 8, Suit: domain.Hearts}, {Rank: 7, Suit: domain.Diamonds}})
	if dry <= wet {
		t.Fatalf("expected dry high board to favor aggressor more, got dry=%.4f wet=%.4f", dry, wet)
	}
}

func TestStore_FallbackDecision_FlopCBetDryAceHighAsAggressor(t *testing.T) {
	base := ai.DecisionInput{
		RoomID:           "flop-cbet-dry-ace-high",
		HandID:           88,
		AIUserID:         "ai-1",
		Stage:            "flop",
		AllowedActions:   []string{"bet", "check", "fold"},
		Pot:              75,
		RoundBet:         0,
		OpenBetMin:       10,
		BetMin:           10,
		MinBet:           10,
		Stack:            980,
		PreflopPosition:  "btn",
		HoleCards:        []string{"7C", "6D"},
		CommunityCards:   []string{"AH", "KD", "2S"},
		HandCategory:     "high_card",
		HandCategoryRank: 0,
		MadeHandStrength: "none",
		DrawFlags:        []string{"none"},
		Players:          []ai.PlayerSnapshot{{UserID: "ai-1"}, {UserID: "p-1", Folded: false}},
		RecentActionLog: []ai.ActionLog{
			{UserID: "ai-1", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "p-1", Action: "call", Amount: 30, Stage: "preflop"},
		},
		Profiles: map[string]ai.Profile{
			"p-1": {Style: "紧弱", Tendencies: []string{"弃牌偏多"}, Advice: "可以压制"},
		},
		OpponentStats: map[string]ai.OpponentStats{
			"p-1": {Hands: 24, VPIP: 0.20, PFR: 0.12, AggressionFactor: 1.0, FoldRate: 0.42, ShowdownRate: 0.22, ShowdownWinRate: 0.48},
		},
	}
	bets := 0
	for version := int64(320); version < 360; version++ {
		input := base
		input.StateVersion = version
		decision := fallbackDecision(input)
		if decision.Action == "bet" {
			bets++
		}
	}
	if bets < 18 {
		t.Fatalf("expected meaningful c-bet frequency on dry ace-high flop, got %d bets", bets)
	}
}

func TestStore_PostflopLinePressure_DetectsCalledThenCheckedLine(t *testing.T) {
	input := ai.DecisionInput{
		AIUserID: "ai-1",
		Stage:    "turn",
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false, LastAction: "check"},
		},
		RecentActionLog: []ai.ActionLog{
			{UserID: "ai-1", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "p-1", Action: "call", Amount: 30, Stage: "preflop"},
			{UserID: "ai-1", Action: "bet", Amount: 35, Stage: "flop"},
			{UserID: "p-1", Action: "call", Amount: 35, Stage: "flop"},
			{UserID: "p-1", Action: "check", Amount: 0, Stage: "turn"},
		},
		Profiles: map[string]ai.Profile{
			"p-1": {Style: "跟注站", Tendencies: []string{"爱跟到底"}, Advice: "多做价值"},
		},
		OpponentStats: map[string]ai.OpponentStats{
			"p-1": {Hands: 18, VPIP: 0.41, PFR: 0.11, AggressionFactor: 0.9, FoldRate: 0.18, ShowdownRate: 0.38, ShowdownWinRate: 0.44},
		},
	}
	if barrels := heroPostflopBarrelCount(input); barrels != 1 {
		t.Fatalf("expected one previous postflop barrel, got %d", barrels)
	}
	if !previousStreetHeroBarrelCalled(input) {
		t.Fatalf("expected flop barrel to be recognized as called")
	}
	if score := visibleRangeCapScore(input); score < 0.18 {
		t.Fatalf("expected capped-line score from call-check pattern, got %.4f", score)
	}
}

func TestStore_ChooseFallbackBetAmount_ContextualSizing(t *testing.T) {
	smallDryFlop := chooseFallbackBetAmount(ai.DecisionInput{Stage: "flop", Pot: 100, Stack: 1000}, 10, "probe", 0.50, 0.28, 0.35, fallbackBetSizingContext{
		Initiative: true,
		RangeAdv:   0.14,
	}, defaultStrategyParams())
	bigPolarTurn := chooseFallbackBetAmount(ai.DecisionInput{Stage: "turn", Pot: 100, Stack: 1000}, 10, "polarize", 0.50, 0.42, 0.30, fallbackBetSizingContext{
		Initiative:  true,
		ScareScore:  0.18,
		CappedScore: 0.16,
		BarrelCount: 1,
	}, defaultStrategyParams())
	if smallDryFlop > 40 {
		t.Fatalf("expected dry flop range bet to stay small, got %d", smallDryFlop)
	}
	if bigPolarTurn < 90 {
		t.Fatalf("expected polar turn barrel to use larger size, got %d", bigPolarTurn)
	}
	if bigPolarTurn <= smallDryFlop {
		t.Fatalf("expected turn polar size to exceed dry flop size, got flop=%d turn=%d", smallDryFlop, bigPolarTurn)
	}
}

func TestStore_FallbackDecision_TurnDoubleBarrelsScareCardAfterFlopCall(t *testing.T) {
	base := ai.DecisionInput{
		RoomID:           "turn-double-barrel-scare",
		HandID:           109,
		AIUserID:         "ai-1",
		Stage:            "turn",
		AllowedActions:   []string{"bet", "check", "fold"},
		Pot:              145,
		RoundBet:         0,
		OpenBetMin:       10,
		BetMin:           10,
		MinBet:           10,
		Stack:            935,
		PreflopPosition:  "btn",
		HoleCards:        []string{"QH", "JD"},
		CommunityCards:   []string{"KH", "7D", "2C", "AS"},
		HandCategory:     "high_card",
		HandCategoryRank: 0,
		MadeHandStrength: "none",
		DrawFlags:        []string{"gutshot"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false, LastAction: "check"},
		},
		RecentActionLog: []ai.ActionLog{
			{UserID: "ai-1", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "p-1", Action: "call", Amount: 30, Stage: "preflop"},
			{UserID: "ai-1", Action: "bet", Amount: 40, Stage: "flop"},
			{UserID: "p-1", Action: "call", Amount: 40, Stage: "flop"},
			{UserID: "p-1", Action: "check", Amount: 0, Stage: "turn"},
		},
		Profiles: map[string]ai.Profile{
			"p-1": {Style: "紧弱", Tendencies: []string{"弃牌偏多", "转牌诚实"}, Advice: "二枪可以继续压制"},
		},
		OpponentStats: map[string]ai.OpponentStats{
			"p-1": {Hands: 26, VPIP: 0.22, PFR: 0.13, AggressionFactor: 0.95, FoldRate: 0.41, ShowdownRate: 0.24, ShowdownWinRate: 0.47},
		},
	}
	bets := 0
	for version := int64(410); version < 450; version++ {
		input := base
		input.StateVersion = version
		decision := fallbackDecision(input)
		if decision.Action == "bet" {
			bets++
		}
	}
	if bets < 16 {
		t.Fatalf("expected meaningful turn double-barrel frequency on scare card, got %d bets", bets)
	}
}

func TestStore_RiverMissedDrawScore_PrefersBrickedTurnDraw(t *testing.T) {
	board := []domain.Card{{Rank: 13, Suit: domain.Hearts}, {Rank: 7, Suit: domain.Hearts}, {Rank: 2, Suit: domain.Clubs}, {Rank: 3, Suit: domain.Diamonds}, {Rank: 9, Suit: domain.Spades}}
	withMiss := riverMissedDrawScore([]domain.Card{{Rank: 14, Suit: domain.Hearts}, {Rank: 5, Suit: domain.Hearts}}, board, 0)
	withoutMiss := riverMissedDrawScore([]domain.Card{{Rank: 14, Suit: domain.Spades}, {Rank: 5, Suit: domain.Diamonds}}, board, 0)
	if withMiss <= withoutMiss {
		t.Fatalf("expected bricked turn draw to score higher, got with=%.4f without=%.4f", withMiss, withoutMiss)
	}
}

func TestStore_FallbackDecision_RiverTripleBarrelsMissedDrawIntoCappedRange(t *testing.T) {
	base := ai.DecisionInput{
		RoomID:           "river-triple-barrel-missed-draw",
		HandID:           126,
		AIUserID:         "ai-1",
		Stage:            "river",
		AllowedActions:   []string{"bet", "check", "fold"},
		Pot:              240,
		RoundBet:         0,
		OpenBetMin:       10,
		BetMin:           10,
		MinBet:           10,
		Stack:            760,
		PreflopPosition:  "btn",
		HoleCards:        []string{"AH", "5H"},
		CommunityCards:   []string{"KH", "7H", "2C", "3D", "QS"},
		HandCategory:     "high_card",
		HandCategoryRank: 0,
		MadeHandStrength: "none",
		DrawFlags:        []string{"none"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false, LastAction: "check"},
		},
		RecentActionLog: []ai.ActionLog{
			{UserID: "ai-1", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "p-1", Action: "call", Amount: 30, Stage: "preflop"},
			{UserID: "ai-1", Action: "bet", Amount: 45, Stage: "flop"},
			{UserID: "p-1", Action: "call", Amount: 45, Stage: "flop"},
			{UserID: "ai-1", Action: "bet", Amount: 85, Stage: "turn"},
			{UserID: "p-1", Action: "call", Amount: 85, Stage: "turn"},
			{UserID: "p-1", Action: "check", Amount: 0, Stage: "river"},
		},
		Profiles: map[string]ai.Profile{
			"p-1": {Style: "紧弱", Tendencies: []string{"弃牌偏多", "河牌诚实"}, Advice: "三枪可继续施压"},
		},
		OpponentStats: map[string]ai.OpponentStats{
			"p-1": {Hands: 33, VPIP: 0.21, PFR: 0.13, AggressionFactor: 0.9, FoldRate: 0.39, ShowdownRate: 0.23, ShowdownWinRate: 0.48},
		},
	}
	bets := 0
	for version := int64(600); version < 640; version++ {
		input := base
		input.StateVersion = version
		decision := fallbackDecision(input)
		if decision.Action == "bet" {
			bets++
		}
	}
	if bets < 16 {
		t.Fatalf("expected meaningful triple-barrel frequency with missed draw and blocker, got %d bets", bets)
	}
}

func TestStore_FallbackDecision_RiverChecksBackWeakPairVsCallingStation(t *testing.T) {
	decision := fallbackDecision(ai.DecisionInput{
		RoomID:           "river-checkback-vs-station",
		HandID:           127,
		AIUserID:         "ai-1",
		Stage:            "river",
		AllowedActions:   []string{"bet", "check", "fold"},
		Pot:              220,
		RoundBet:         0,
		OpenBetMin:       10,
		BetMin:           10,
		MinBet:           10,
		Stack:            780,
		PreflopPosition:  "co",
		HoleCards:        []string{"7C", "6D"},
		CommunityCards:   []string{"KH", "7S", "2C", "AS", "4D"},
		HandCategory:     "one_pair",
		HandCategoryRank: 1,
		MadeHandStrength: "weak",
		DrawFlags:        []string{"none"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false, LastAction: "check"},
		},
		RecentActionLog: []ai.ActionLog{
			{UserID: "ai-1", Action: "bet", Amount: 30, Stage: "preflop"},
			{UserID: "p-1", Action: "call", Amount: 30, Stage: "preflop"},
			{UserID: "ai-1", Action: "bet", Amount: 40, Stage: "flop"},
			{UserID: "p-1", Action: "call", Amount: 40, Stage: "flop"},
			{UserID: "ai-1", Action: "bet", Amount: 70, Stage: "turn"},
			{UserID: "p-1", Action: "call", Amount: 70, Stage: "turn"},
			{UserID: "p-1", Action: "check", Amount: 0, Stage: "river"},
		},
		Profiles: map[string]ai.Profile{
			"p-1": {Style: "跟注站", Tendencies: []string{"爱跟到底", "河牌过度跟注"}, Advice: "不要乱诈唬"},
		},
		OpponentStats: map[string]ai.OpponentStats{
			"p-1": {Hands: 36, VPIP: 0.46, PFR: 0.11, AggressionFactor: 0.8, FoldRate: 0.15, ShowdownRate: 0.41, ShowdownWinRate: 0.45},
		},
	})
	if decision.Action != "check" {
		t.Fatalf("expected weak pair to check back versus calling station, got %s", decision.Action)
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

func TestStore_ChipRefreshVoteRejectEndsVoting(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "vote", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}

	if _, err := s.StartChipRefreshVote(room.RoomID, guest.UserID); err == nil {
		t.Fatalf("expected non-owner start vote fail")
	}
	if _, err := s.StartChipRefreshVote(room.RoomID, owner.UserID); err != nil {
		t.Fatalf("start chip refresh vote failed: %v", err)
	}

	if _, err := s.CastChipRefreshVote(room.RoomID, guest.UserID, "agree"); err != nil {
		t.Fatalf("guest agree failed: %v", err)
	}
	r1, _ := s.GetRoom(room.RoomID)
	if r1.ChipRefreshVote == nil || r1.ChipRefreshVote.Result != ChipRefreshVotePending {
		t.Fatalf("expected vote still pending after partial agree")
	}

	if _, err := s.CastChipRefreshVote(room.RoomID, owner.UserID, "reject"); err != nil {
		t.Fatalf("owner reject failed: %v", err)
	}
	r2, _ := s.GetRoom(room.RoomID)
	if r2.ChipRefreshVote == nil {
		t.Fatalf("expected vote state after reject")
	}
	if r2.ChipRefreshVote.Result != ChipRefreshVoteRejected {
		t.Fatalf("expected rejected result, got %s", r2.ChipRefreshVote.Result)
	}
	if r2.ChipRefreshVote.Votes[owner.UserID] != ChipRefreshVoteReject {
		t.Fatalf("expected owner reject vote recorded")
	}
	if _, err := s.CastChipRefreshVote(room.RoomID, guest.UserID, "agree"); err == nil {
		t.Fatalf("expected no active vote after reject")
	}
}

func TestStore_ChipRefreshVoteAllAgreeResetsAllPlayerStacks(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "vote-reset", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddAI(room.RoomID, owner.UserID, "bot"); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	internalRoom := s.rooms[room.RoomID]
	internalRoom.Players[0].Stack = 3200
	internalRoom.Players[1].Stack = 4500
	internalRoom.Players[2].Stack = 800
	s.mu.Unlock()

	if _, err := s.StartChipRefreshVote(room.RoomID, owner.UserID); err != nil {
		t.Fatalf("start chip refresh vote failed: %v", err)
	}
	r1, _ := s.GetRoom(room.RoomID)
	if r1.ChipRefreshVote == nil {
		t.Fatalf("expected vote state")
	}
	if len(r1.ChipRefreshVote.EligibleUserIDs) != 2 {
		t.Fatalf("expected only human players eligible, got %d", len(r1.ChipRefreshVote.EligibleUserIDs))
	}
	for _, uid := range r1.ChipRefreshVote.EligibleUserIDs {
		if strings.HasPrefix(uid, "ai-") {
			t.Fatalf("ai should not be eligible voter")
		}
	}

	if _, err := s.CastChipRefreshVote(room.RoomID, guest.UserID, "agree"); err != nil {
		t.Fatalf("guest agree failed: %v", err)
	}
	if _, err := s.CastChipRefreshVote(room.RoomID, owner.UserID, "agree"); err != nil {
		t.Fatalf("owner agree failed: %v", err)
	}

	r2, _ := s.GetRoom(room.RoomID)
	if r2.ChipRefreshVote == nil || r2.ChipRefreshVote.Result != ChipRefreshVoteApproved {
		t.Fatalf("expected approved vote result")
	}
	for _, p := range r2.Players {
		if p.Stack != DefaultPlayerStack {
			t.Fatalf("expected player %s stack reset to %d, got %d", p.UserID, DefaultPlayerStack, p.Stack)
		}
	}
}

func TestStore_ChipRefreshVoteAllowedWhenHandFinished(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")
	room := s.CreateRoom(owner, "vote-finished", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	r1, _ := s.GetRoom(room.RoomID)
	turnUser := r1.Game.Players[r1.Game.TurnPos].UserID
	if _, err := s.ApplyAction(room.RoomID, turnUser, "finish-by-fold", "fold", 0, r1.StateVersion); err != nil {
		t.Fatal(err)
	}

	r2, _ := s.GetRoom(room.RoomID)
	if r2.Game == nil || r2.Game.Stage != "finished" {
		t.Fatalf("expected finished hand before vote")
	}
	if _, err := s.StartChipRefreshVote(room.RoomID, owner.UserID); err != nil {
		t.Fatalf("expected start vote allowed after hand finished, got %v", err)
	}
	if _, err := s.CastChipRefreshVote(room.RoomID, guest.UserID, "agree"); err != nil {
		t.Fatalf("guest vote agree failed: %v", err)
	}
	if _, err := s.CastChipRefreshVote(room.RoomID, owner.UserID, "agree"); err != nil {
		t.Fatalf("owner vote agree failed: %v", err)
	}

	r3, _ := s.GetRoom(room.RoomID)
	if r3.ChipRefreshVote == nil || r3.ChipRefreshVote.Result != ChipRefreshVoteApproved {
		t.Fatalf("expected approved vote result after finished-hand voting")
	}
}

func TestStore_ChipRefreshVote_ZeroStackSittingOutPlayerStillEligibleAndCanReturn(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest1 := s.CreateSession("guest-1")
	guest2 := s.CreateSession("guest-2")

	room := s.CreateRoom(owner, "vote-sitout", 10, 10)
	if _, err := s.JoinRoom(room.RoomID, guest1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinRoom(room.RoomID, guest2); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.rooms[room.RoomID].Players[0].Stack = 0
	s.mu.Unlock()

	r1, err := s.StartGame(room.RoomID, owner.UserID)
	if err != nil {
		t.Fatalf("start game failed: %v", err)
	}
	if r1.Game == nil {
		t.Fatalf("expected started game")
	}
	for _, gp := range r1.Game.Players {
		if gp.UserID == owner.UserID {
			t.Fatalf("expected zero-stack owner sitting out this hand")
		}
	}

	turnUser := r1.Game.Players[r1.Game.TurnPos].UserID
	r2, err := s.ApplyAction(room.RoomID, turnUser, "vote-sitout-finish", "fold", 0, r1.StateVersion)
	if err != nil {
		t.Fatalf("finish hand failed: %v", err)
	}
	if r2.Game == nil || r2.Game.Stage != "finished" {
		t.Fatalf("expected finished hand before vote")
	}

	if _, err := s.StartChipRefreshVote(room.RoomID, owner.UserID); err != nil {
		t.Fatalf("start chip refresh vote failed: %v", err)
	}
	r3, _ := s.GetRoom(room.RoomID)
	if r3.ChipRefreshVote == nil {
		t.Fatalf("expected vote state")
	}
	foundOwner := false
	for _, uid := range r3.ChipRefreshVote.EligibleUserIDs {
		if uid == owner.UserID {
			foundOwner = true
			break
		}
	}
	if !foundOwner {
		t.Fatalf("expected sitting-out owner to keep voting rights")
	}

	if _, err := s.CastChipRefreshVote(room.RoomID, owner.UserID, "agree"); err != nil {
		t.Fatalf("expected sitting-out owner can vote agree, got %v", err)
	}
	if _, err := s.CastChipRefreshVote(room.RoomID, guest1.UserID, "agree"); err != nil {
		t.Fatalf("guest1 agree failed: %v", err)
	}
	if _, err := s.CastChipRefreshVote(room.RoomID, guest2.UserID, "agree"); err != nil {
		t.Fatalf("guest2 agree failed: %v", err)
	}

	r4, _ := s.GetRoom(room.RoomID)
	if r4.Players[0].Stack != DefaultPlayerStack {
		t.Fatalf("expected owner stack refreshed to %d, got %d", DefaultPlayerStack, r4.Players[0].Stack)
	}

	r5, err := s.NextHand(room.RoomID, owner.UserID)
	if err != nil {
		t.Fatalf("next hand failed after refresh: %v", err)
	}
	foundOwnerInHand := false
	for _, gp := range r5.Game.Players {
		if gp.UserID == owner.UserID {
			foundOwnerInHand = true
			break
		}
	}
	if !foundOwnerInHand {
		t.Fatalf("expected refreshed owner to rejoin next hand")
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

func TestStore_GuardAIDecision_PrefersFallbackOnBadRiverBluff(t *testing.T) {
	input := ai.DecisionInput{
		AIUserID:         "ai-1",
		Stage:            "river",
		Pot:              180,
		Stack:            600,
		RoundBet:         0,
		MinBet:           20,
		CallAmount:       0,
		AllowedActions:   []string{"check", "bet", "fold"},
		HoleCards:        []string{"7S", "6C"},
		CommunityCards:   []string{"KD", "8C", "8S", "3H", "2D"},
		HandCategory:     "one_pair",
		HandCategoryRank: 1,
		MadeHandStrength: "weak",
		DrawFlags:        []string{"none"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false},
		},
	}
	decision := guardAIDecision(input, ai.Decision{Action: "bet", Amount: 140}, ai.Decision{Action: "check", Amount: 0})
	if decision.Action != "check" {
		t.Fatalf("expected fallback check, got %s", decision.Action)
	}
}

func TestStore_GuardAIDecision_PrefersFallbackOnMissedRiverValue(t *testing.T) {
	input := ai.DecisionInput{
		AIUserID:         "ai-1",
		Stage:            "river",
		Pot:              160,
		Stack:            700,
		RoundBet:         0,
		MinBet:           20,
		CallAmount:       0,
		AllowedActions:   []string{"check", "bet", "fold"},
		HoleCards:        []string{"AS", "AH"},
		CommunityCards:   []string{"2C", "2D", "9H", "TS", "KD"},
		HandCategory:     "two_pair",
		HandCategoryRank: 2,
		MadeHandStrength: "strong",
		DrawFlags:        []string{"none"},
		Players: []ai.PlayerSnapshot{
			{UserID: "ai-1"},
			{UserID: "p-1", Folded: false},
		},
	}
	decision := guardAIDecision(input, ai.Decision{Action: "check", Amount: 0}, ai.Decision{Action: "bet", Amount: 80})
	if decision.Action != "bet" || decision.Amount != 80 {
		t.Fatalf("expected fallback value bet, got %#v", decision)
	}
}

func TestStore_AITurnUsesOfflineFallbackWhenLLMDisabled(t *testing.T) {
	stub := &stubAIService{}
	stub.decisionFn = func(_ context.Context, input ai.DecisionInput) (ai.Decision, error) {
		return ai.Decision{Action: "allin", Amount: 0}, nil
	}

	s := NewMemoryStore(Options{AI: stub})
	if _, err := s.UpdateAIRuntimeSettings(AIRuntimeSettings{UseLLM: false, Model: "offline-only"}); err != nil {
		t.Fatalf("disable llm: %v", err)
	}
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
	managedRoom, err := s.SetPlayerAIManaged(room.RoomID, turn.UserID, true)
	if err != nil {
		t.Fatalf("enable offline ai managed failed: %v", err)
	}
	startVersion := managedRoom.StateVersion

	deadline := time.Now().Add(2 * time.Second)
	for {
		latest, ok := s.GetRoom(room.RoomID)
		if !ok || latest == nil || latest.Game == nil {
			t.Fatalf("room/game missing while waiting offline ai action")
		}
		acted := false
		for _, gp := range latest.Game.Players {
			if gp.UserID == turn.UserID {
				acted = gp.LastAction != ""
				break
			}
		}
		if latest.StateVersion > startVersion && acted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("offline ai did not act in time: version=%d start=%d", latest.StateVersion, startVersion)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if stub.decideCount != 0 {
		t.Fatalf("expected no llm calls when disabled, got %d", stub.decideCount)
	}
}
