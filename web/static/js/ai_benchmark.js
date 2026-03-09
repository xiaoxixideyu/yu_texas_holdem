(async function () {
  const statusEl = document.getElementById("status-json");
  const paramsEl = document.getElementById("params-json");
  const persistedEl = document.getElementById("persisted-json");
  const messageEl = document.getElementById("status-message");
  const aiSettingsEl = document.getElementById("ai-settings-json");
  const useLLMEl = document.getElementById("use-llm");
  const modelEl = document.getElementById("llm-model");
  const saveSettingsBtn = document.getElementById("btn-save-settings");
  const refreshSettingsBtn = document.getElementById("btn-refresh-settings");
  const startBtn = document.getElementById("btn-start");
  const stopBtn = document.getElementById("btn-stop");
  const refreshBtn = document.getElementById("btn-refresh");

  let timer = null;

  function fmtTime(unix) {
    if (!unix) return "-";
    return new Date(unix * 1000).toLocaleString();
  }

  function renderAISettings(aiSettings) {
    const view = {
      useLlm: !!aiSettings.useLlm,
      model: aiSettings.model || "",
      decisionMode: aiSettings.decisionMode || "offline",
      llmConfigured: !!aiSettings.llmConfigured,
      apiKeyConfigured: !!aiSettings.apiKeyConfigured,
      baseUrl: aiSettings.baseUrl || "",
      apiFormat: aiSettings.apiFormat || "",
      timeoutMs: aiSettings.timeoutMs || 0,
      maxRetry: aiSettings.maxRetry || 0,
      configPath: aiSettings.configPath || "",
      updatedAt: fmtTime(aiSettings.lastUpdatedAtUnix),
    };
    useLLMEl.checked = !!aiSettings.useLlm;
    modelEl.value = aiSettings.model || "";
    aiSettingsEl.textContent = JSON.stringify(view, null, 2);
  }

  function renderStatus(status) {
    const view = {
      running: !!status.running,
      configPath: status.configPath || "",
      startedAt: fmtTime(status.startedAtUnix),
      updatedAt: fmtTime(status.updatedAtUnix),
      iterations: status.iterations || 0,
      accepted: status.accepted || 0,
      lastDeltaBb100: Number(status.lastDeltaBb100 || 0).toFixed(2),
      bestDeltaBb100: Number(status.bestDeltaBb100 || 0).toFixed(2),
      lastMessage: status.lastMessage || "",
    };
    statusEl.textContent = JSON.stringify(view, null, 2);
    paramsEl.textContent = JSON.stringify(status.currentParams || {}, null, 2);
    persistedEl.textContent = JSON.stringify(status.persistedParams || {}, null, 2);
    renderAISettings(status.aiSettings || {});
    const mode = status.aiSettings?.decisionMode === "llm" ? "LLM 优先" : "离线本地策略";
    messageEl.textContent = status.running
      ? `训练中：${status.lastMessage || "running"}｜线上决策：${mode}`
      : `空闲：${status.lastMessage || "idle"}｜线上决策：${mode}`;
    startBtn.disabled = !!status.running;
    stopBtn.disabled = !status.running;
  }

  async function loadStatus() {
    try {
      const status = await api("/api/v1/ai-benchmark/status");
      renderStatus(status);
    } catch (err) {
      messageEl.textContent = err.message || "加载状态失败";
    }
  }

  async function saveSettings() {
    saveSettingsBtn.disabled = true;
    try {
      const status = await api("/api/v1/ai-benchmark/settings", {
        method: "POST",
        body: {
          useLlm: !!useLLMEl.checked,
          model: (modelEl.value || "").trim(),
        },
      });
      renderStatus(status);
    } catch (err) {
      messageEl.textContent = err.data?.error || err.message || "保存设置失败";
    } finally {
      saveSettingsBtn.disabled = false;
    }
  }

  async function start() {
    startBtn.disabled = true;
    try {
      const status = await api("/api/v1/ai-benchmark/start", { method: "POST", body: {} });
      renderStatus(status);
    } catch (err) {
      messageEl.textContent = err.data?.error || err.message || "启动失败";
      startBtn.disabled = false;
    }
  }

  async function stop() {
    stopBtn.disabled = true;
    try {
      const status = await api("/api/v1/ai-benchmark/stop", { method: "POST", body: {} });
      renderStatus(status);
    } catch (err) {
      messageEl.textContent = err.data?.error || err.message || "停止失败";
      stopBtn.disabled = false;
    }
  }

  saveSettingsBtn.addEventListener("click", saveSettings);
  refreshSettingsBtn.addEventListener("click", loadStatus);
  startBtn.addEventListener("click", start);
  stopBtn.addEventListener("click", stop);
  refreshBtn.addEventListener("click", loadStatus);

  const me = await restoreSessionOrRedirect();
  if (!me) return;

  await loadStatus();
  timer = setInterval(loadStatus, 2000);
  window.addEventListener("beforeunload", () => {
    if (timer) clearInterval(timer);
  });
})();
