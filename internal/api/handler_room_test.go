package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"texas_yu/internal/store"
)

func TestRoomHandler_AddRemoveAIOwnerOnly(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	h := &RoomHandler{Store: ms}

	nonOwnerAddReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/ai", strings.NewReader(`{"name":"bot"}`))
	nonOwnerAddW := httptest.NewRecorder()
	h.AddAI(nonOwnerAddW, nonOwnerAddReq, guest)
	if nonOwnerAddW.Code != http.StatusBadRequest {
		t.Fatalf("expected non-owner add ai bad request, got %d", nonOwnerAddW.Code)
	}

	ownerAddReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/ai", strings.NewReader(`{"name":"bot"}`))
	ownerAddW := httptest.NewRecorder()
	h.AddAI(ownerAddW, ownerAddReq, owner)
	if ownerAddW.Code != http.StatusOK {
		t.Fatalf("expected owner add ai success, got %d body=%s", ownerAddW.Code, ownerAddW.Body.String())
	}

	r, _ := ms.GetRoom(room.RoomID)
	var aiUserID string
	for _, p := range r.Players {
		if p.IsAI {
			aiUserID = p.UserID
			break
		}
	}
	if aiUserID == "" {
		t.Fatalf("expected ai player in room")
	}

	nonOwnerRemoveReq := httptest.NewRequest(http.MethodDelete, "/api/v1/rooms/"+room.RoomID+"/ai/"+aiUserID, nil)
	nonOwnerRemoveW := httptest.NewRecorder()
	h.RemoveAI(nonOwnerRemoveW, nonOwnerRemoveReq, guest)
	if nonOwnerRemoveW.Code != http.StatusBadRequest {
		t.Fatalf("expected non-owner remove ai bad request, got %d", nonOwnerRemoveW.Code)
	}

	ownerRemoveReq := httptest.NewRequest(http.MethodDelete, "/api/v1/rooms/"+room.RoomID+"/ai/"+aiUserID, nil)
	ownerRemoveW := httptest.NewRecorder()
	h.RemoveAI(ownerRemoveW, ownerRemoveReq, owner)
	if ownerRemoveW.Code != http.StatusOK {
		t.Fatalf("expected owner remove ai success, got %d body=%s", ownerRemoveW.Code, ownerRemoveW.Body.String())
	}
}

func TestRoomHandler_SpectateAndLeave(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	spectator := ms.CreateSession("spectator")
	room := ms.CreateRoom(owner, "room", 10, 10)
	h := &RoomHandler{Store: ms}

	spectateReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/spectate", strings.NewReader(`{}`))
	spectateW := httptest.NewRecorder()
	h.SpectateRoom(spectateW, spectateReq, spectator)
	if spectateW.Code != http.StatusOK {
		t.Fatalf("expected spectate success, got %d body=%s", spectateW.Code, spectateW.Body.String())
	}

	leaveReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/leave", strings.NewReader(`{}`))
	leaveW := httptest.NewRecorder()
	h.LeaveRoom(leaveW, leaveReq, spectator)
	if leaveW.Code != http.StatusOK {
		t.Fatalf("expected spectator leave success, got %d body=%s", leaveW.Code, leaveW.Body.String())
	}
}

func TestRoomHandler_JoinSpectateIdempotentBehavior(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	user := ms.CreateSession("user")
	room := ms.CreateRoom(owner, "room", 10, 10)
	h := &RoomHandler{Store: ms}

	spectateReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/spectate", strings.NewReader(`{}`))
	spectateW := httptest.NewRecorder()
	h.SpectateRoom(spectateW, spectateReq, user)
	if spectateW.Code != http.StatusOK {
		t.Fatalf("expected spectate success, got %d body=%s", spectateW.Code, spectateW.Body.String())
	}

	joinReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/join", strings.NewReader(`{}`))
	joinW := httptest.NewRecorder()
	h.JoinRoom(joinW, joinReq, user)
	if joinW.Code != http.StatusOK {
		t.Fatalf("expected join success after spectate, got %d body=%s", joinW.Code, joinW.Body.String())
	}

	r1, _ := ms.GetRoom(room.RoomID)
	if len(r1.Spectators) != 0 {
		t.Fatalf("expected spectator removed after join, got %d", len(r1.Spectators))
	}

	spectateAgainReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/spectate", strings.NewReader(`{}`))
	spectateAgainW := httptest.NewRecorder()
	h.SpectateRoom(spectateAgainW, spectateAgainReq, user)
	if spectateAgainW.Code != http.StatusOK {
		t.Fatalf("expected spectate on player to return ok, got %d body=%s", spectateAgainW.Code, spectateAgainW.Body.String())
	}

	r2, _ := ms.GetRoom(room.RoomID)
	if len(r2.Spectators) != 0 {
		t.Fatalf("expected spectator list unchanged for player, got %d", len(r2.Spectators))
	}
}
