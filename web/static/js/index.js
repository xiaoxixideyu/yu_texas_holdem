(async function initIndexPage() {
  const me = await tryRestoreSession();
  if (me) {
    location.href = "/rooms.html";
    return;
  }

  document.getElementById("login-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const username = document.getElementById("username").value.trim();
    if (!username) return;
    try {
      const session = await api("/api/v1/session", {
        method: "POST",
        body: { username },
      });
      setSession(session);
      location.href = "/rooms.html";
    } catch (err) {
      alert(err.message);
    }
  });
})();
