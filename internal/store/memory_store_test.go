package store

import "testing"

func TestStore_RoomLifecycleAndVersionConflict(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")

	room := s.CreateRoom(owner, "r1", 2, 10, 10)
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

func TestStore_LeaveRoomAndNextHand(t *testing.T) {
	s := NewMemoryStore()
	owner := s.CreateSession("owner")
	guest := s.CreateSession("guest")

	room := s.CreateRoom(owner, "r2", 2, 10, 10)
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
