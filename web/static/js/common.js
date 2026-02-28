const API_BASE = "";

function getUserId() {
  return localStorage.getItem("userId") || "";
}

function setSession(session) {
  localStorage.setItem("userId", session.userId);
  localStorage.setItem("username", session.username);
}

function clearSession() {
  localStorage.removeItem("userId");
  localStorage.removeItem("username");
}

async function restoreSessionOrRedirect() {
  const userId = getUserId();
  if (!userId) {
    location.href = "/index.html";
    return null;
  }
  try {
    const me = await api("/api/v1/session/me");
    setSession(me);
    return me;
  } catch (_) {
    clearSession();
    location.href = "/index.html";
    return null;
  }
}

async function tryRestoreSession() {
  const userId = getUserId();
  if (!userId) return null;
  try {
    const me = await api("/api/v1/session/me");
    setSession(me);
    return me;
  } catch (_) {
    clearSession();
    return null;
  }
}

async function api(path, opts = {}) {
  const headers = Object.assign(
    {
      "Content-Type": "application/json",
      "X-User-Id": getUserId(),
    },
    opts.headers || {}
  );
  const resp = await fetch(API_BASE + path, {
    method: opts.method || "GET",
    headers,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    const err = new Error(data.error || `HTTP ${resp.status}`);
    err.status = resp.status;
    err.data = data;
    throw err;
  }
  return data;
}

function qs(name) {
  return new URLSearchParams(location.search).get(name) || "";
}
