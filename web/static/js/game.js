let currentUserId = "";

const roomId = qs("roomId");
if (!roomId) {
  location.href = "/rooms.html";
}

let stateVersion = 0;
let pollTimer = null;
let quickChatPollTimer = null;
let quickChatLastEventId = 0;
let quickChatCooldownMs = 6000;
let localQuickChatEventId = 0;

const shownQuickChatEventIds = new Set();
const bubbleTimersByUser = new Map();
const activeQuickChatByUser = new Map();

const ACTION_TEXT = {
  check: "过牌",
  call: "跟注",
  bet: "下注",
  raise: "加注",
  allin: "梭哈",
  fold: "弃牌",
  leave: "离开房间",
  small_blind: "小盲",
  big_blind: "大盲",
};

const STAGE_TEXT = {
  preflop: "翻牌前",
  flop: "翻牌",
  turn: "转牌",
  river: "河牌",
  showdown: "比牌",
  finished: "已结束",
};

const RESULT_REASON_TEXT = {
  showdown: "比牌结束",
  "others folded": "其余玩家已弃牌",
  "no active players": "没有存活玩家",
};

const HAND_TEXT = {
  straight_flush: "同花顺",
  four_of_a_kind: "四条",
  full_house: "葫芦",
  flush: "同花",
  straight: "顺子",
  three_of_a_kind: "三条",
  two_pair: "两对",
  one_pair: "一对",
  high_card: "高牌",
};

const REVEAL_TEXT = {
  0: "不亮牌",
  1: "亮第一张",
  2: "亮第二张",
  3: "全亮",
};

const QUICK_CHAT_TEXT = {
  wait_flowers: "快点吧，我等到花都谢了",
  solve_universe: "你这手在算宇宙最优解吗",
  tea_refill: "我茶都续第三杯了",
  countdown: "倒计时了兄弟，手别抖",
  thinker_mode: "这把是“思考者”模式吗",
  dawn_table: "再想下去牌桌都天亮了",
  cappuccino: "给阿姨倒一杯卡布奇诺",
  showtime: "这波节目效果拉满",
  you_act_i_act: "你演我也演，大家都别闲",
  something_here: "这手牌有点东西",
  mind_game: "主打一个心理战",
  script_seen: "这把剧本我看过",
  allin_warning: "别逼我，我要梭哈了",
  just_this: "就这？",
  easy_sigh: "唉，easy",
  fold_now: "别跟了，弃牌吧",
  you_call_i_show: "你敢跟，我就敢开",
  take_the_shot: "这枪你接不接",
  pressure_on: "我先给压力，你随意",
  tilt_alert: "你这波有点上头啊",
  nh: "打得漂亮",
  gg: "好局好局",
  luck_is_skill: "运气也是实力的一部分",
  next_real: "下把见真章",
};

const QUICK_CHAT_GROUPS = [
  {
    label: "催节奏",
    ids: ["wait_flowers", "solve_universe", "tea_refill", "countdown", "thinker_mode", "dawn_table"],
  },
  {
    label: "整活梗",
    ids: ["cappuccino", "showtime", "you_act_i_act", "something_here", "mind_game", "script_seen"],
  },
  {
    label: "挑衅压迫",
    ids: ["allin_warning", "just_this", "easy_sigh", "fold_now", "you_call_i_show", "take_the_shot", "pressure_on", "tilt_alert"],
  },
  {
    label: "互动收尾",
    ids: ["nh", "gg", "luck_is_skill", "next_real"],
  },
];

function logLine(msg) {
  const el = document.getElementById("log");
  const now = new Date().toLocaleTimeString();
  el.textContent = `[${now}] ${msg}\n` + el.textContent;
}

function toActionText(action) {
  return ACTION_TEXT[action] || action || "-";
}

function toStageText(stage) {
  return STAGE_TEXT[stage] || stage || "-";
}

function toReasonText(reason) {
  return RESULT_REASON_TEXT[reason] || reason || "-";
}

function toHandText(name) {
  return HAND_TEXT[name] || name || "-";
}

function toQuickChatText(phraseId) {
  return QUICK_CHAT_TEXT[phraseId] || phraseId || "";
}

function renderHandLog(data) {
  const el = document.getElementById("hand-log");
  if (!el) return;
  if (!data.game || !data.game.actionLogs || !data.game.actionLogs.length) {
    el.textContent = "暂无记录";
    return;
  }
  el.textContent = data.game.actionLogs
    .map((log) => {
      const action = toActionText(log.action);
      const stage = toStageText(log.stage);
      if (log.amount > 0) {
        return `[${stage}] ${log.username} ${action} ${log.amount}`;
      }
      return `[${stage}] ${log.username} ${action}`;
    })
    .join("\n");
}

function getCurrentPlayer(data) {
  if (!data || !data.game || !Array.isArray(data.game.players)) return null;
  return data.game.players.find((p) => p.userId === currentUserId) || null;
}

function updateRevealControls(data) {
  const controls = document.getElementById("reveal-controls");
  const hint = document.getElementById("reveal-hint");
  if (!controls || !hint) return;

  const me = getCurrentPlayer(data);
  const canReveal = !!(me && me.canReveal);

  controls.style.display = canReveal ? "flex" : "none";
  hint.style.display = canReveal ? "block" : "none";

  controls.querySelectorAll("button[data-reveal]").forEach((btn) => {
    const mask = Number(btn.dataset.reveal);
    btn.disabled = !canReveal;
    btn.classList.toggle("is-active", canReveal && mask === Number(me.revealMask || 0));
  });
}

function updateActionButtons(data) {
  const buttons = {
    check: document.querySelector('button[data-action="check"]'),
    call: document.querySelector('button[data-action="call"]'),
    bet: document.querySelector('button[data-action="bet"]'),
    raise: document.querySelector('button[data-action="raise"]'),
    allin: document.querySelector('button[data-action="allin"]'),
    fold: document.querySelector('button[data-action="fold"]'),
  };

  const actionHint = document.getElementById("action-hint");
  const betAmountInput = document.getElementById("bet-amount");
  const callAmountLabel = document.getElementById("call-amount-label");

  const gameStarted = !!data.game;
  Object.values(buttons).forEach((btn) => {
    if (!btn) return;
    btn.style.display = gameStarted ? "inline-block" : "none";
    btn.disabled = true;
    btn.title = gameStarted ? "当前不可执行该操作" : "游戏未开始";
  });
  if (betAmountInput) betAmountInput.style.display = gameStarted ? "inline-block" : "none";

  if (!gameStarted) {
    if (actionHint) actionHint.textContent = "游戏未开始，暂不可操作。";
    return;
  }

  const me = getCurrentPlayer(data);
  if (!me) {
    if (actionHint) actionHint.textContent = "你当前不在牌局中，无法操作。";
    return;
  }

  const canAllIn = !!(me.isTurn && !me.folded && typeof me.stack === "number" && me.stack > 0);

  buttons.check.disabled = !me.canCheck;
  buttons.call.disabled = !me.canCall;
  buttons.bet.disabled = !me.canBet;
  buttons.raise.disabled = !me.canRaise;
  if (buttons.allin) buttons.allin.disabled = !canAllIn;
  buttons.fold.disabled = !me.canFold;

  if (callAmountLabel) {
    callAmountLabel.textContent = me.canCall ? `(${me.callAmount})` : "";
  }

  if (betAmountInput) {
    if (me.canBet) {
      betAmountInput.min = me.minBet;
      betAmountInput.placeholder = `≥${me.minBet}`;
      if (!betAmountInput.value) betAmountInput.value = me.minBet;
    } else if (me.canRaise) {
      betAmountInput.min = me.minRaise;
      betAmountInput.placeholder = `≥${me.minRaise}`;
      if (!betAmountInput.value) betAmountInput.value = me.minRaise;
    }
    betAmountInput.max = me.stack;
    betAmountInput.disabled = !me.canBet && !me.canRaise;
  }

  if (buttons.check) buttons.check.title = me.canCheck ? "执行过牌" : "当前不可过牌";
  if (buttons.call) buttons.call.title = me.canCall ? `跟注 ${me.callAmount}` : "当前不可跟注";
  if (buttons.bet) buttons.bet.title = me.canBet ? `下注（最低 ${me.minBet}）` : "当前不可下注";
  if (buttons.raise) buttons.raise.title = me.canRaise ? `加注（最低 ${me.minRaise}）` : "当前不可加注";
  if (buttons.allin) buttons.allin.title = canAllIn ? `梭哈（全下 ${me.stack}）` : "当前不可梭哈";
  if (buttons.fold) buttons.fold.title = me.canFold ? "执行弃牌" : "当前不可弃牌";

  const availableActions = [];
  if (me.canCheck) availableActions.push("过牌");
  if (me.canCall) availableActions.push(`跟注(${me.callAmount})`);
  if (me.canBet) availableActions.push(`下注(≥${me.minBet})`);
  if (me.canRaise) availableActions.push(`加注(≥${me.minRaise})`);
  if (canAllIn) availableActions.push(`梭哈(${me.stack})`);
  if (me.canFold) availableActions.push("弃牌");

  if (actionHint) {
    actionHint.textContent = availableActions.length
      ? `当前可操作：${availableActions.join("、")}`
      : "当前不可操作，请等待轮到你。";
  }
}

function updateOwnerActions(data) {
  const btnStartGame = document.getElementById("btn-start-game");
  const btnNextHand = document.getElementById("btn-next-hand");
  const hint = document.getElementById("next-hand-hint");
  if (!btnStartGame || !btnNextHand || !hint) return;

  const isOwner = data.ownerUserId === currentUserId;
  const isWaiting = data.roomStatus === "waiting" && !data.game;
  const canStartNextHand = !!data.canStartNextHand;

  btnStartGame.style.display = isOwner && isWaiting ? "inline-block" : "none";
  btnNextHand.style.display = canStartNextHand ? "inline-block" : "none";

  const handFinished = !!(data.game && data.game.stage === "finished");
  hint.style.display = !isOwner && handFinished ? "block" : "none";
}

function updateMyStack(data) {
  const el = document.getElementById("my-stack");
  if (!el) return;

  let stack = null;
  const roomPlayer = (data.roomPlayers || []).find((p) => p.userId === currentUserId);
  if (roomPlayer && typeof roomPlayer.stack === "number") {
    stack = roomPlayer.stack;
  }

  if (stack === null && data.game && Array.isArray(data.game.players)) {
    const gp = data.game.players.find((p) => p.userId === currentUserId);
    if (gp && typeof gp.stack === "number") {
      stack = gp.stack;
    }
  }

  el.textContent = `筹码：${stack === null ? "-" : stack}`;
}

function attrEscape(value) {
  return String(value || "")
    .replace(/&/g, "&amp;")
    .replace(/"/g, "&quot;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function selectorEscape(value) {
  return String(value || "").replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function renderWaitingPlayers(data) {
  const players = data.roomPlayers || [];
  if (!players.length) {
    document.getElementById("players").innerHTML = `<p class="hint">当前房间暂无玩家</p>`;
    return;
  }

  document.getElementById("players").innerHTML = players
    .map(
      (p) => `
      <div class="player-row" data-user-id="${attrEscape(p.userId)}">
        <div class="player-avatar">${(p.username || "?")[0]}</div>
        <div class="player-info">
          <div class="player-name">
            ${p.username}
            ${p.userId === data.ownerUserId ? '<span class="badge badge-owner">房主</span>' : ""}
          </div>
          <div class="player-details">座位 ${p.seat} · 筹码 ${typeof p.stack === "number" ? p.stack : "-"}</div>
        </div>
      </div>
    `
    )
    .join("");
}

function renderState(data) {
  const gameMeta = document.getElementById("game-meta");
  if (!data.game) {
    gameMeta.innerHTML = `
      <div class="game-meta-grid">
        <div><span class="meta-label">房间</span><div class="meta-value">${data.roomName}</div></div>
        <div><span class="meta-label">状态</span><div class="meta-value">等待开局</div></div>
      </div>`;
    renderWaitingPlayers(data);
    renderActiveQuickChatBubbles();
    updateMyStack(data);
    updateOwnerActions(data);
    updateRevealControls(data);
    updateActionButtons(data);
    renderHandLog(data);
    return;
  }

  const g = data.game;
  const stageClass = g.stage === "finished" ? " finished" : "";
  const communityHtml = (g.communityCards || []).length
    ? (g.communityCards || []).map((c) => cardHtml(c)).join("")
    : '<span class="hint">等待发牌</span>';

  let resultHtml = "";
  if (g.result) {
    const reason = toReasonText(g.result.reason ?? g.result.Reason);
    const winnerIds = (g.result.winners ?? g.result.Winners) || [];
    const winnerTexts = winnerIds.map((id) => {
      const p = (g.players || []).find((pl) => pl.userId === id);
      return p ? `${p.username}(${p.userId})` : id;
    });
    const winners = winnerTexts.join("、") || "无";
    resultHtml = `<div class="result-banner">${reason} — 赢家：${winners}</div>`;
  }

  gameMeta.innerHTML = `
    <div class="game-meta-grid">
      <div><span class="meta-label">房间</span><div class="meta-value">${data.roomName}</div></div>
      <div><span class="meta-label">阶段</span><div class="meta-value"><span class="stage-badge${stageClass}">${toStageText(g.stage)}</span></div></div>
    </div>
    <div class="pot-display"><span class="pot-label">底池</span><br/>${g.pot}</div>
    <div class="community-cards">${communityHtml}</div>
    ${resultHtml}
  `;

  document.getElementById("players").innerHTML = (g.players || [])
    .map((p, idx) => {
      const isTurn = p.isTurn;
      const isFolded = p.folded;
      let rowClass = "player-row";
      if (isTurn) rowClass += " is-turn";
      if (isFolded) rowClass += " is-folded";

      const badges = [];
      if (idx === g.dealerPos) badges.push('<span class="badge badge-dealer">D</span>');
      if (idx === g.smallBlindPos) badges.push('<span class="badge badge-sb">SB</span>');
      if (idx === g.bigBlindPos) badges.push('<span class="badge badge-bb">BB</span>');
      if (isTurn) badges.push('<span class="badge badge-turn">行动中</span>');
      if (isFolded) badges.push('<span class="badge badge-folded">已弃牌</span>');

      const holeCardsHtml = (p.holeCards || []).length
        ? p.holeCards.map((c) => cardHtml(c)).join("")
        : '<span class="poker-card hidden-card"></span><span class="poker-card hidden-card"></span>';

      const bestHand = p.bestHandName ? `<span class="hint"> · ${toHandText(p.bestHandName)}</span>` : "";

      return `
        <div class="${rowClass}" data-user-id="${attrEscape(p.userId)}">
          <div class="player-avatar">${(p.username || "?")[0]}</div>
          <div class="player-info">
            <div class="player-name">${p.username} ${badges.join(" ")}</div>
            <div class="player-details">筹码 ${p.stack} · 押注 ${p.contributed || 0} · ${toActionText(p.lastAction)}${bestHand}</div>
          </div>
          <div class="player-cards">${holeCardsHtml}</div>
        </div>`;
    })
    .join("");

  renderActiveQuickChatBubbles();
  updateRevealControls(data);
  updateMyStack(data);
  updateOwnerActions(data);
  updateActionButtons(data);
  renderHandLog(data);
}

function cardText(c) {
  const suits = ["♣", "♦", "♥", "♠"];
  if (!c) return "?";

  const rankRaw = c.rank ?? c.Rank;
  const suitRaw = c.suit ?? c.Suit;

  const rankNum = typeof rankRaw === "number" ? rankRaw : Number(rankRaw);
  const suitNum = typeof suitRaw === "number" ? suitRaw : Number(suitRaw);

  const rankMap = { 11: "J", 12: "Q", 13: "K", 14: "A" };
  const rankText = Number.isFinite(rankNum) ? rankMap[rankNum] || String(rankNum) : "?";
  const suitText = Number.isInteger(suitNum) && suitNum >= 0 && suitNum < suits.length ? suits[suitNum] : "?";

  return `${rankText}${suitText}`;
}

function cardHtml(c) {
  if (!c) return '<span class="poker-card hidden-card"></span>';
  const suits = ["♣", "♦", "♥", "♠"];
  const rankRaw = c.rank ?? c.Rank;
  const suitRaw = c.suit ?? c.Suit;
  const rankNum = typeof rankRaw === "number" ? rankRaw : Number(rankRaw);
  const suitNum = typeof suitRaw === "number" ? suitRaw : Number(suitRaw);
  const rankMap = { 11: "J", 12: "Q", 13: "K", 14: "A" };
  const rankText = Number.isFinite(rankNum) ? rankMap[rankNum] || String(rankNum) : "?";
  const suitText = Number.isInteger(suitNum) && suitNum >= 0 && suitNum < suits.length ? suits[suitNum] : "?";
  const isRed = suitNum === 1 || suitNum === 2;
  return `<span class="poker-card${isRed ? " red" : ""}">${rankText}${suitText}</span>`;
}

function removeUserBubble(userId) {
  const selector = `.player-row[data-user-id="${selectorEscape(userId)}"] .player-bubble`;
  const bubble = document.querySelector(selector);
  if (bubble) bubble.remove();
}

function clearBubbleTimer(userId) {
  const timer = bubbleTimersByUser.get(userId);
  if (timer) {
    clearTimeout(timer);
    bubbleTimersByUser.delete(userId);
  }
}

function hideQuickChat(userId, eventId) {
  const active = activeQuickChatByUser.get(userId);
  if (!active || Number(active.eventId) !== Number(eventId)) return;
  activeQuickChatByUser.delete(userId);
  clearBubbleTimer(userId);
  removeUserBubble(userId);
}

function renderQuickChatBubble(event) {
  const row = document.querySelector(`.player-row[data-user-id="${selectorEscape(event.userId)}"]`);
  if (!row) return;

  const existing = row.querySelector(".player-bubble");
  if (existing) existing.remove();

  const bubble = document.createElement("div");
  bubble.className = "player-bubble";
  if (event.userId === currentUserId) {
    bubble.classList.add("is-self");
  }
  bubble.textContent = toQuickChatText(event.phraseId);
  row.appendChild(bubble);
}

function scheduleQuickChatHide(event, serverNowMs) {
  clearBubbleTimer(event.userId);
  const ttl = Math.max(1, Number(event.expireAtMs || 0) - Number(serverNowMs || Date.now()));
  const timer = setTimeout(() => hideQuickChat(event.userId, event.eventId), ttl);
  bubbleTimersByUser.set(event.userId, timer);
}

function applyQuickChatEvent(event, serverNowMs) {
  if (!event || !event.userId || !event.eventId) return;
  if (shownQuickChatEventIds.has(event.eventId)) return;

  shownQuickChatEventIds.add(event.eventId);
  activeQuickChatByUser.set(event.userId, event);
  renderQuickChatBubble(event);
  scheduleQuickChatHide(event, serverNowMs);
}

function renderActiveQuickChatBubbles() {
  const now = Date.now();
  activeQuickChatByUser.forEach((event, userId) => {
    if (Number(event.expireAtMs || 0) <= now) {
      hideQuickChat(userId, event.eventId);
      return;
    }
    renderQuickChatBubble(event);
  });
}

function trimSeenQuickChatEvents() {
  if (shownQuickChatEventIds.size <= 1000) return;
  shownQuickChatEventIds.clear();
}

function clearAllQuickChatState() {
  if (quickChatPollTimer) {
    clearInterval(quickChatPollTimer);
    quickChatPollTimer = null;
  }
  bubbleTimersByUser.forEach((timer) => clearTimeout(timer));
  bubbleTimersByUser.clear();
  shownQuickChatEventIds.clear();
  activeQuickChatByUser.clear();
}

function resetQuickChatPolling() {
  if (quickChatPollTimer) clearInterval(quickChatPollTimer);
  quickChatPollTimer = setInterval(loadQuickChats, 1000);
}

function setQuickChatFeedback(text, isError = false) {
  const feedback = document.getElementById("quick-chat-feedback");
  if (!feedback) return;
  if (!text) {
    feedback.textContent = "";
    feedback.classList.remove("is-error");
    return;
  }
  feedback.textContent = text;
  feedback.classList.toggle("is-error", !!isError);
}

function renderQuickChatButtons(phrases) {
  const select = document.getElementById("quick-chat-select");
  const sendBtn = document.getElementById("quick-chat-send");
  if (!select || !sendBtn) return;

  const source = Array.isArray(phrases) && phrases.length ? phrases : Object.keys(QUICK_CHAT_TEXT);
  const available = new Set(source);

  const optionHtml = QUICK_CHAT_GROUPS.map((group) => {
    const ids = group.ids.filter((id) => available.has(id));
    if (!ids.length) return "";
    ids.forEach((id) => available.delete(id));
    const options = ids
      .map((id) => `<option value="${attrEscape(id)}">${toQuickChatText(id)}</option>`)
      .join("");
    return `<optgroup label="${attrEscape(group.label)}">${options}</optgroup>`;
  })
    .filter(Boolean)
    .join("");

  const leftover = Array.from(available);
  const leftoverHtml = leftover.length
    ? `<optgroup label="${attrEscape("其他")}">${leftover
        .map((id) => `<option value="${attrEscape(id)}">${toQuickChatText(id)}</option>`)
        .join("")}</optgroup>`
    : "";

  select.innerHTML = ['<option value="">请选择短句</option>', optionHtml, leftoverHtml].join("");

  sendBtn.onclick = () => sendQuickChat(select.value || "");
  select.onchange = () => {
    setQuickChatFeedback("");
  };
}

function createLocalQuickChatEvent(phraseId) {
  localQuickChatEventId -= 1;
  const now = Date.now();
  return {
    eventId: localQuickChatEventId,
    userId: currentUserId,
    phraseId,
    expireAtMs: now + 3500,
  };
}

async function loadQuickChats() {
  try {
    const data = await api(`/api/v1/rooms/${roomId}/quick-chats?sinceEventId=${quickChatLastEventId}`);
    if (typeof data.cooldownMs === "number" && data.cooldownMs > 0) {
      quickChatCooldownMs = data.cooldownMs;
    }

    const latest = Number(data.latestEventId || 0);
    if (latest > quickChatLastEventId) {
      quickChatLastEventId = latest;
    }

    const events = Array.isArray(data.events) ? data.events.slice() : [];
    events.sort((a, b) => Number(a.eventId || 0) - Number(b.eventId || 0));
    const serverNowMs = Number(data.serverNowMs || Date.now());

    events.forEach((event) => applyQuickChatEvent(event, serverNowMs));
    trimSeenQuickChatEvents();
  } catch (err) {
    console.error(err);
  }
}

async function sendQuickChat(phraseId) {
  if (!phraseId) {
    setQuickChatFeedback("请先选择短句", true);
    return;
  }
  try {
    await api(`/api/v1/rooms/${roomId}/quick-chats`, {
      method: "POST",
      body: {
        actionId: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
        phraseId,
      },
    });
    applyQuickChatEvent(createLocalQuickChatEvent(phraseId), Date.now());
    setQuickChatFeedback("");

    const select = document.getElementById("quick-chat-select");
    if (select) select.value = "";

    const panel = document.getElementById("quick-chat-panel");
    if (panel && panel.open) panel.open = false;

    await loadQuickChats();
  } catch (err) {
    if (String(err.message || "").includes("quick chat cooldown")) {
      const retryAfterMs = Number(err && err.data && err.data.retryAfterMs);
      const hintMs = Number.isFinite(retryAfterMs) && retryAfterMs > 0 ? retryAfterMs : quickChatCooldownMs;
      setQuickChatFeedback(`发送过快，请约 ${Math.ceil(hintMs / 1000)} 秒后再试`, true);
      return;
    }
    setQuickChatFeedback(`短句发送失败：${err.message}`, true);
  }
}

async function initQuickChatConfig() {
  try {
    const data = await api(`/api/v1/rooms/${roomId}/quick-chats?sinceEventId=0`);
    renderQuickChatButtons(data.phrases || []);

    if (typeof data.cooldownMs === "number" && data.cooldownMs > 0) {
      quickChatCooldownMs = data.cooldownMs;
    }

    const latest = Number(data.latestEventId || 0);
    if (latest > quickChatLastEventId) {
      quickChatLastEventId = latest;
    }

    const events = Array.isArray(data.events) ? data.events.slice() : [];
    events.sort((a, b) => Number(a.eventId || 0) - Number(b.eventId || 0));
    const serverNowMs = Number(data.serverNowMs || Date.now());
    events.forEach((event) => applyQuickChatEvent(event, serverNowMs));
  } catch (err) {
    renderQuickChatButtons([]);
    setQuickChatFeedback(`短句初始化失败：${err.message}`, true);
  }
}

async function loadState() {
  try {
    const data = await api(`/api/v1/rooms/${roomId}/state?sinceVersion=${stateVersion}`);
    if (data.notModified) return;
    stateVersion = data.stateVersion || stateVersion;
    renderState(data);

    const isMyTurn = !!(data.game && data.game.players && data.game.players.find((p) => p.userId === currentUserId && p.isTurn));
    resetPolling(isMyTurn ? 700 : 1200);
  } catch (err) {
    console.error(err);
    logLine(`状态拉取失败：${err.message}`);
  }
}

async function doAction(type) {
  try {
    const body = {
      actionId: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
      type,
      expectedVersion: stateVersion,
    };
    if (type === "allin") {
      const yes = window.confirm("确认梭哈吗？此操作会将你当前剩余筹码全部投入本轮。\n梭哈后你将无法继续下注，只能等待比牌结果。");
      if (!yes) {
        logLine("已取消梭哈");
        return;
      }
    }
    if (type === "bet" || type === "raise") {
      const input = document.getElementById("bet-amount");
      const amount = Number(input ? input.value : 0);
      if (!amount || amount <= 0) {
        logLine("请输入下注金额");
        return;
      }
      body.amount = amount;
      body.type = "bet";
    }
    await api(`/api/v1/rooms/${roomId}/actions`, {
      method: "POST",
      body,
    });
    logLine(`操作成功：${toActionText(type)}`);
    const betInput = document.getElementById("bet-amount");
    if (betInput) betInput.value = "";
    await loadState();
  } catch (err) {
    logLine(`操作失败（${toActionText(type)}）：${err.message}`);
  }
}

async function doReveal(mask) {
  try {
    await api(`/api/v1/rooms/${roomId}/actions`, {
      method: "POST",
      body: {
        actionId: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
        type: "reveal",
        revealMask: Number(mask),
        expectedVersion: stateVersion,
      },
    });
    logLine(`亮牌设置成功：${REVEAL_TEXT[Number(mask)] || Number(mask)}`);
    await loadState();
  } catch (err) {
    logLine(`亮牌设置失败：${err.message}`);
  }
}

async function startGame() {
  try {
    await api(`/api/v1/rooms/${roomId}/start`, { method: "POST", body: {} });
    logLine("已开始游戏");
    await loadState();
  } catch (err) {
    logLine(`开始游戏失败：${err.message}`);
  }
}

async function nextHand() {
  try {
    await api(`/api/v1/rooms/${roomId}/next-hand`, { method: "POST", body: {} });
    logLine("已开始下一局");
    await loadState();
  } catch (err) {
    logLine(`开始下一局失败：${err.message}`);
  }
}

async function leaveRoom() {
  try {
    if (pollTimer) clearInterval(pollTimer);
    clearAllQuickChatState();
    await api(`/api/v1/rooms/${roomId}/leave`, { method: "POST", body: {} });
    location.href = "/rooms.html";
  } catch (err) {
    logLine(`离开房间失败：${err.message}`);
  }
}

function resetPolling(ms) {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = setInterval(loadState, ms);
}

(async function initGamePage() {
  const me = await restoreSessionOrRedirect();
  if (!me) return;
  currentUserId = me.userId;

  document.querySelectorAll("button[data-action]").forEach((btn) => {
    btn.addEventListener("click", () => doAction(btn.dataset.action));
  });

  document.querySelectorAll("button[data-reveal]").forEach((btn) => {
    btn.addEventListener("click", () => doReveal(btn.dataset.reveal));
  });

  document.getElementById("btn-start-game").addEventListener("click", startGame);
  document.getElementById("btn-next-hand").addEventListener("click", nextHand);
  document.getElementById("btn-leave-room").addEventListener("click", leaveRoom);
  document.getElementById("btn-start-game").style.display = "none";
  document.getElementById("btn-next-hand").style.display = "none";

  await initQuickChatConfig();
  resetQuickChatPolling();
  resetPolling(1200);
  await loadState();
  await loadQuickChats();

  window.addEventListener("beforeunload", () => {
    if (pollTimer) clearInterval(pollTimer);
    clearAllQuickChatState();
  });
})();
