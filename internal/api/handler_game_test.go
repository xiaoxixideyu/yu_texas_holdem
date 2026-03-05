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
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/actions", strings.NewReader(`{"actionId":"r-before","type":"reveal","revealMask":1,"expectedVersion":`+int64ToString(r.StateVersion)+`} `))
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

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/actions", strings.NewReader(`{"actionId":"r-conflict","type":"reveal","revealMask":2,"expectedVersion":`+int64ToString(r.StateVersion-1)+`} `))
	w2 := httptest.NewRecorder()
	h.Action(w2, req2, owner)
	if w2.Code != http.StatusConflict {
		t.Fatalf("expected version conflict status 409, got %d body=%s", w2.Code, w2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/actions", strings.NewReader(`{"actionId":"r-invalid","type":"reveal","revealMask":9,"expectedVersion":`+int64ToString(r.StateVersion)+`} `))
	w3 := httptest.NewRecorder()
	h.Action(w3, req3, owner)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid reveal mask bad request, got %d body=%s", w3.Code, w3.Body.String())
	}
}

func TestGameHandler_QuickChatSendAndPoll(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	h := &GameHandler{Store: ms}

	sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/quick-chats", strings.NewReader(`{"actionId":"qc-send-1","phraseId":"nh"}`))
	sendW := httptest.NewRecorder()
	h.QuickChat(sendW, sendReq, owner)
	if sendW.Code != http.StatusOK {
		t.Fatalf("expected quick chat send success, got %d body=%s", sendW.Code, sendW.Body.String())
	}
	if !strings.Contains(sendW.Body.String(), `"chatEventId":`) {
		t.Fatalf("expected chatEventId in response, body=%s", sendW.Body.String())
	}
	if strings.Contains(sendW.Body.String(), `"stateVersion"`) {
		t.Fatalf("quick chat success should not include stateVersion, body=%s", sendW.Body.String())
	}

	pollReq := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/quick-chats?sinceEventId=0", nil)
	pollW := httptest.NewRecorder()
	h.GetQuickChats(pollW, pollReq, guest)
	if pollW.Code != http.StatusOK {
		t.Fatalf("expected quick chat poll success, got %d body=%s", pollW.Code, pollW.Body.String())
	}
	body := pollW.Body.String()
	if !strings.Contains(body, `"phraseId":"nh"`) {
		t.Fatalf("expected phrase in poll response, body=%s", body)
	}
	if !strings.Contains(body, `"latestEventId":`) {
		t.Fatalf("expected latestEventId in response, body=%s", body)
	}
	if !strings.Contains(body, `"cooldownMs":`) {
		t.Fatalf("expected cooldown config in response, body=%s", body)
	}
}

func TestGameHandler_QuickChatCooldownAndValidation(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	room := ms.CreateRoom(owner, "room", 10, 10)
	h := &GameHandler{Store: ms}

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/quick-chats", strings.NewReader(`{"actionId":"qc-cool-1","phraseId":"nh"}`))
	w1 := httptest.NewRecorder()
	h.QuickChat(w1, req1, owner)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected first quick chat send success, got %d body=%s", w1.Code, w1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/quick-chats", strings.NewReader(`{"actionId":"qc-cool-2","phraseId":"gg"}`))
	w2 := httptest.NewRecorder()
	h.QuickChat(w2, req2, owner)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected cooldown status 429, got %d body=%s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), `"retryAfterMs":`) {
		t.Fatalf("expected retryAfterMs in cooldown response, body=%s", w2.Body.String())
	}
	if strings.Contains(w2.Body.String(), `"stateVersion"`) {
		t.Fatalf("quick chat cooldown response should not include stateVersion, body=%s", w2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/quick-chats", strings.NewReader(`{"actionId":"qc-invalid","phraseId":"free"}`))
	w3 := httptest.NewRecorder()
	h.QuickChat(w3, req3, owner)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid phrase bad request, got %d body=%s", w3.Code, w3.Body.String())
	}
	if strings.Contains(w3.Body.String(), `"stateVersion"`) {
		t.Fatalf("quick chat invalid phrase response should not include stateVersion, body=%s", w3.Body.String())
	}
}

func TestGameHandler_QuickChatForbiddenForNonMember(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	outsider := ms.CreateSession("outsider")
	room := ms.CreateRoom(owner, "room", 10, 10)
	h := &GameHandler{Store: ms}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/quick-chats?sinceEventId=0", nil)
	getW := httptest.NewRecorder()
	h.GetQuickChats(getW, getReq, outsider)
	if getW.Code != http.StatusForbidden {
		t.Fatalf("expected get quick chats forbidden, got %d body=%s", getW.Code, getW.Body.String())
	}

	sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/quick-chats", strings.NewReader(`{"actionId":"qc-out","phraseId":"nh"}`))
	sendW := httptest.NewRecorder()
	h.QuickChat(sendW, sendReq, outsider)
	if sendW.Code != http.StatusForbidden {
		t.Fatalf("expected send quick chats forbidden, got %d body=%s", sendW.Code, sendW.Body.String())
	}
}

func TestGameHandler_SpectatorActionRevealDenied(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	spectator := ms.CreateSession("spectator")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.SpectateRoom(room.RoomID, spectator); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	r, _ := ms.GetRoom(room.RoomID)

	h := &GameHandler{Store: ms}
	actionReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/actions", strings.NewReader(`{"actionId":"spec-a","type":"check","expectedVersion":`+int64ToString(r.StateVersion)+`} `))
	actionW := httptest.NewRecorder()
	h.Action(actionW, actionReq, spectator)
	if actionW.Code != http.StatusForbidden {
		t.Fatalf("expected spectator action forbidden, got %d body=%s", actionW.Code, actionW.Body.String())
	}

	revealReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/actions", strings.NewReader(`{"actionId":"spec-r","type":"reveal","revealMask":1,"expectedVersion":`+int64ToString(r.StateVersion)+`} `))
	revealW := httptest.NewRecorder()
	h.Action(revealW, revealReq, spectator)
	if revealW.Code != http.StatusForbidden {
		t.Fatalf("expected spectator reveal forbidden, got %d body=%s", revealW.Code, revealW.Body.String())
	}
}

func TestGameHandler_SpectatorHoleCardsVisibility(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	spectator := ms.CreateSession("spectator")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.SpectateRoom(room.RoomID, spectator); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	h := &GameHandler{Store: ms}
	reqPlaying := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/state", nil)
	wPlaying := httptest.NewRecorder()
	h.GetState(wPlaying, reqPlaying, spectator)
	bodyPlaying := wPlaying.Body.String()
	if !strings.Contains(bodyPlaying, `"viewerRole":"spectator"`) {
		t.Fatalf("expected spectator viewerRole, body=%s", bodyPlaying)
	}
	if strings.Contains(bodyPlaying, `"holeCards":[{`) {
		t.Fatalf("spectator should not see any hole card during playing, body=%s", bodyPlaying)
	}

	r, _ := ms.GetRoom(room.RoomID)
	turnUser := r.Game.Players[r.Game.TurnPos].UserID
	if _, err := ms.ApplyAction(room.RoomID, turnUser, "fold-finish", "fold", 0, r.StateVersion); err != nil {
		t.Fatal(err)
	}
	r, _ = ms.GetRoom(room.RoomID)
	if _, err := ms.ApplyReveal(room.RoomID, guest.UserID, "guest-reveal-mask", 1, r.StateVersion); err != nil {
		t.Fatal(err)
	}

	reqFinished := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/state", nil)
	wFinished := httptest.NewRecorder()
	h.GetState(wFinished, reqFinished, spectator)
	bodyFinished := wFinished.Body.String()
	if !strings.Contains(bodyFinished, `"viewerRole":"spectator"`) {
		t.Fatalf("expected spectator viewerRole finished, body=%s", bodyFinished)
	}
	if !strings.Contains(bodyFinished, `"revealMask":1`) {
		t.Fatalf("expected reveal mask present in finished state, body=%s", bodyFinished)
	}
	if !strings.Contains(bodyFinished, `"holeCards":[{`) {
		t.Fatalf("expected one revealed card visible after finished, body=%s", bodyFinished)
	}
}

func TestGameHandler_SpectatorQuickChatReadOnly(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	spectator := ms.CreateSession("spectator")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.SpectateRoom(room.RoomID, spectator); err != nil {
		t.Fatal(err)
	}
	h := &GameHandler{Store: ms}

	sendByOwnerReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/quick-chats", strings.NewReader(`{"actionId":"qc-owner","phraseId":"nh"}`))
	sendByOwnerW := httptest.NewRecorder()
	h.QuickChat(sendByOwnerW, sendByOwnerReq, owner)
	if sendByOwnerW.Code != http.StatusOK {
		t.Fatalf("expected owner quick chat success, got %d body=%s", sendByOwnerW.Code, sendByOwnerW.Body.String())
	}

	pollReq := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/quick-chats?sinceEventId=0", nil)
	pollW := httptest.NewRecorder()
	h.GetQuickChats(pollW, pollReq, spectator)
	if pollW.Code != http.StatusOK {
		t.Fatalf("expected spectator quick chat poll success, got %d body=%s", pollW.Code, pollW.Body.String())
	}

	sendReq := httptest.NewRequest(http.MethodPost, "/api/v1/rooms/"+room.RoomID+"/quick-chats", strings.NewReader(`{"actionId":"qc-spec","phraseId":"gg"}`))
	sendW := httptest.NewRecorder()
	h.QuickChat(sendW, sendReq, spectator)
	if sendW.Code != http.StatusForbidden {
		t.Fatalf("expected spectator quick chat send forbidden, got %d body=%s", sendW.Code, sendW.Body.String())
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

func TestGameHandler_GetStateIncludesIsAi(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, _, err := ms.AddAI(room.RoomID, owner.UserID, "bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.StartGame(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}

	h := &GameHandler{Store: ms}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/state", nil)
	w := httptest.NewRecorder()
	h.GetState(w, req, owner)

	body := w.Body.String()
	if !strings.Contains(body, "\"isAi\":true") {
		t.Fatalf("expected game/player isAi true in state body=%s", body)
	}
	if !strings.Contains(body, "\"aiMemory\":") {
		t.Fatalf("expected aiMemory in state body=%s", body)
	}
}

func TestGameHandler_GetStateIncludesChipRefreshVote(t *testing.T) {
	ms := store.NewMemoryStore()
	owner := ms.CreateSession("owner")
	guest := ms.CreateSession("guest")
	room := ms.CreateRoom(owner, "room", 10, 10)
	if _, err := ms.JoinRoom(room.RoomID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.StartChipRefreshVote(room.RoomID, owner.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.CastChipRefreshVote(room.RoomID, guest.UserID, "agree"); err != nil {
		t.Fatal(err)
	}

	h := &GameHandler{Store: ms}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/"+room.RoomID+"/state", nil)
	w := httptest.NewRecorder()
	h.GetState(w, req, owner)

	body := w.Body.String()
	if !strings.Contains(body, `"chipRefreshVote":`) {
		t.Fatalf("expected chipRefreshVote in state body=%s", body)
	}
	if !strings.Contains(body, `"votes":{"`) {
		t.Fatalf("expected vote decisions in state body=%s", body)
	}
}
