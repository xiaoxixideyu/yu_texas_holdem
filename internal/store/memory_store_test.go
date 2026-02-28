package store

import "testing"

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
