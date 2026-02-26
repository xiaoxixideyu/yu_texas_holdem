package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"texas_yu/internal/store"
)

func TestGameHandler_GetState_FinishedDefaultsNoRevealForOthers(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	r, _ := ms.GetRoom(room.RoomID)
	turnUser := r.Game.Players[r.Game.TurnPos].UserID
	if _, err := ms.ApplyAction(room.RoomID, turnUser, "fold-end", "fold", 0, r.StateVersion); err != nil {
		t.Fatal(err)
	}

	h := &GameHandler{Store: ms}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/state", nil)
	w := httptest.NewRecorder()
	h.GetState(w, req, owner)

	body := w.Body.String()
	if !strings.Contains(body, "\"canReveal\":true") {
		t.Fatalf("expected self canReveal true, body=%s", body)
	}
	if !strings.Contains(body, "\"revealMask\":0") {
		t.Fatalf("expected default revealMask 0, body=%s", body)
	}
	if !strings.Contains(body, "\"holeCards\":[null,null]") {
		t.Fatalf("expected other player hidden cards by default, body=%s", body)
	}
}

func TestGameHandler_GetState_RevealMaskShowsSelectedCards(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	r, _ := ms.GetRoom(room.RoomID)
	turnUser := r.Game.Players[r.Game.TurnPos].UserID
	if _, err := ms.ApplyAction(room.RoomID, turnUser, "fold-end2", "fold", 0, r.StateVersion); err != nil {
		t.Fatal(err)
	}
	r, _ = ms.GetRoom(room.RoomID)
	if _, err := ms.ApplyReveal(room.RoomID, guest.UserID, "guest-reveal", 1, r.StateVersion); err != nil {
		t.Fatal(err)
	}

	h := &GameHandler{Store: ms}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/state", nil)
	w := httptest.NewRecorder()
	h.GetState(w, req, owner)

	body := w.Body.String()
	if !strings.Contains(body, "\"revealMask\":1") {
		t.Fatalf("expected revealMask 1 in response, body=%s", body)
	}
	if !strings.Contains(body, "\"holeCards\":[{") {
		t.Fatalf("expected one revealed card object, body=%s", body)
	}
	if !strings.Contains(body, ",null]") {
		t.Fatalf("expected second slot hidden, body=%s", body)
	}
}

func TestGameHandler_ActionRevealValidationAndVersionConflict(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	h := &GameHandler{Store: ms}

	r, _ := ms.GetRoom(room.RoomID)
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/actions", strings.NewReader(`{"actionId":"r-before","type":"reveal","revealMask":1,"expectedVersion":`+int64ToString(r.StateVersion)+`}`))
	w1 := httptest.NewRecorder()
	h.Action(w1, req1, owner)
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("expected reveal before finished bad request, got %d body=%s", w1.Code, w1.Body.String())
	}

	turnUser := r.Game.Players[r.Game.TurnPos].UserID
	if _, err := ms.ApplyAction(room.RoomID, turnUser, "fold-end3", "fold", 0, r.StateVersion); err != nil {
		t.Fatal(err)
	}
	r, _ = ms.GetRoom(room.RoomID)

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/actions", strings.NewReader(`{"actionId":"r-conflict","type":"reveal","revealMask":2,"expectedVersion":`+int64ToString(r.StateVersion-1)+`}`))
	w2 := httptest.NewRecorder()
	h.Action(w2, req2, owner)
	if w2.Code != http.StatusConflict {
		t.Fatalf("expected version conflict status 409, got %d body=%s", w2.Code, w2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/actions", strings.NewReader(`{"actionId":"r-invalid","type":"reveal","revealMask":9,"expectedVersion":`+int64ToString(r.StateVersion)+`}`))
	w3 := httptest.NewRecorder()
	h.Action(w3, req3, owner)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid reveal mask bad request, got %d body=%s", w3.Code, w3.Body.String())
	}
}

func int64ToString(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
