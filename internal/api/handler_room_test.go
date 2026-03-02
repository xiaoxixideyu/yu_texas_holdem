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
