(async function initRoomsPage() {
  const me = await restoreSessionOrRedirect();
  if (!me) return;

  document.getElementById("me").textContent = `当前用户：${me.username || ""}`;

  document.getElementById("btn-logout-lobby").addEventListener("click", async () => {
    try {
      await api("/api/v1/session/logout", { method: "POST", body: {} });
    } catch (_) {}
    clearSession();
    location.href = "/index.html";
  });

  let lastVersion = 0;

  async function loadRooms() {
    try {
      const data = await api(`/api/v1/rooms?sinceVersion=${lastVersion}`);
      if (data.notModified) return;
      lastVersion = data.version || lastVersion;
      renderRooms(data.rooms || []);
    } catch (err) {
      console.error(err);
    }
  }

  function roomStatusText(status) {
    return status === "waiting" ? "等待中" : status === "playing" ? "游戏中" : status;
  }

  function renderRooms(rooms) {
    const root = document.getElementById("rooms");
    if (!rooms.length) {
      root.innerHTML = `<p class="hint">暂无房间，创建一个吧</p>`;
      return;
    }
    root.innerHTML = rooms
      .map(
        (r) => `
        <div class="room-item">
          <div>
            <strong>${r.name}</strong>
            <div class="hint">${r.players.length} 人 · ${roomStatusText(r.status)} · 开局≥${r.openBetMin || 10} · 加注≥${r.betMin || 10}</div>
          </div>
          <button onclick="joinRoom('${r.roomId}')">进入</button>
        </div>
      `
      )
      .join("");
  }

  window.joinRoom = async function joinRoom(roomId) {
    try {
      await api(`/api/v1/rooms/${roomId}/join`, { method: "POST", body: {} });
      location.href = `/game.html?roomId=${roomId}`;
    } catch (err) {
      alert(err.message);
    }
  };

  document.getElementById("create-room-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const name = document.getElementById("room-name").value.trim() || "房间";
    const openBetMin = Number(document.getElementById("open-bet-min").value) || 10;
    const betMin = Number(document.getElementById("bet-min").value) || 10;
    try {
      const room = await api("/api/v1/rooms", {
        method: "POST",
        body: { name, openBetMin, betMin },
      });
      location.href = `/game.html?roomId=${room.roomId}`;
    } catch (err) {
      alert(err.message);
    }
  });

  setInterval(loadRooms, 2000);
  loadRooms();
})();
