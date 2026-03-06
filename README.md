# Texas Yu（简易德州扑克）

一个使用 Go 编写的简化版德州扑克项目：
- 不使用数据库，所有状态在内存中
- 前端为静态页面
- 客户端通过轮询获取房间与游戏状态
- 支持配置化 AI 玩家（可多 AI）

## 启动

```bash
go run ./cmd/server
```

打开：
- `http://localhost:8080/index.html`（用户名输入页）
- `http://localhost:8080/rooms.html`（房间页）
- `http://localhost:8080/game.html?roomId=r-1`（游戏页）

## AI 配置

通过环境变量启用 AI（缺失关键配置时自动降级 noop，不影响真人玩法）：

- `AI_BASE_URL`：AI 网关地址（如 `https://api.openai.com/v1`）
- `AI_API_KEY`：API key
- `AI_MODEL`：模型名（如 `gpt-4o-mini`）
- `AI_API_FORMAT`：`chat_completions` 或 `responses`
- `AI_TIMEOUT_MS`：请求超时毫秒（默认 8000）
- `AI_MAX_RETRY`：请求重试次数（默认 2）

示例：

```bash
export AI_BASE_URL="https://api.openai.com/v1"
export AI_API_KEY="<your_key>"
export AI_MODEL="gpt-4o-mini"
export AI_API_FORMAT="chat_completions"
export AI_TIMEOUT_MS="8000"
export AI_MAX_RETRY="2"
go run ./cmd/server
```

## 页面与轮询

1. 用户名页：创建会话
2. 房间页：每 2 秒轮询房间列表，支持“退出大厅”并清理本地用户状态
3. 游戏页：
   - 自己回合约 700ms 轮询
   - 非自己回合约 1200ms 轮询
   - 房间不存在（404）时自动停止轮询并返回大厅

## AI 行为说明

- 房主可在 `waiting` 状态下添加/删除多个 AI 玩家
- AI 与真人一样走 `ApplyAction` 版本链路（`expectedVersion/stateVersion`）
- AI 的 LLM 调用在锁外执行，锁内仅快照/校验/提交
- AI 回合自动行动；模型输出非法时使用“混合策略兜底”（牌力+压力+行动历史+对手画像），包含慢打、半诈唬与控池，不再固定单一路径
- 真人玩家可开启/取消 `AI托管`（开启后由 AI 自动代打，手动下注类操作会被禁用）
- 每手结束（正常结束 + leave 强制结束）写入 AI 复盘与对手画像
- `state` 中可见：
  - `roomPlayers[].isAi`
  - `roomPlayers[].aiManaged`
  - `game.players[].isAi`
  - `game.players[].aiManaged`
  - `aiMemory`
- 当房间无真人玩家时自动删房（即使仍有 AI）

## 鉴权

接口支持以下任一方式携带用户 ID：
- `X-User-Id: u-xxx`
- `Authorization: Bearer u-xxx`
- URL Query: `?userId=u-xxx`

---

## API 联调文档

### 1) 创建会话

`POST /api/v1/session`

请求：
```json
{
  "username": "alice"
}
```

响应：
```json
{
  "userId": "u-1",
  "username": "alice",
  "expiresAt": 1771450000
}
```

### 2) 查询当前会话

`GET /api/v1/session/me`

响应：同上。

---

### 3) 房间列表（轮询）

`GET /api/v1/rooms?sinceVersion=3`

有变化响应：
```json
{
  "rooms": [
    {
      "roomId": "r-1",
      "name": "room-a",
      "ownerUserId": "u-1",
      "status": "waiting",
      "players": [
        {"userId":"u-1","username":"alice","seat":0,"stack":10000,"isAi":false}
      ],
      "stateVersion": 1,
      "updatedAtUnix": 1771450000
    }
  ],
  "version": 5
}
```

无变化响应：
```json
{
  "notModified": true,
  "version": 5
}
```

### 4) 创建房间

`POST /api/v1/rooms`

请求：
```json
{
  "name": "room-a",
  "openBetMin": 10,
  "betMin": 10
}
```

响应：返回完整房间对象。

### 5) 加入房间

`POST /api/v1/rooms/{roomId}/join`

请求：
```json
{}
```

响应：返回房间对象。

### 6) 离开房间

`POST /api/v1/rooms/{roomId}/leave`

请求：
```json
{}
```

响应：
- 房间仍存在：返回房间对象
- 房间被清空删除：
```json
{
  "deleted": true
}
```

### 7) 开始游戏

`POST /api/v1/rooms/{roomId}/start`

请求：
```json
{}
```

约束：
- 至少 2 名玩家且至少 2 名玩家筹码 `> 0`
- 筹码 `<= 0` 的玩家不会进入当局 `game.players`

响应：返回房间对象（`status=playing`，并带游戏状态）。

### 8) 下一局

`POST /api/v1/rooms/{roomId}/next-hand`

请求：
```json
{}
```

约束：
- 仅房主可调用
- 当前局必须已经 finished
- 下一局创建时仅纳入筹码 `> 0` 的玩家

响应：返回新一局初始化后的房间对象。

### 9) 房主添加 AI

`POST /api/v1/rooms/{roomId}/ai`

请求：
```json
{
  "name": "Bot A"
}
```

约束：
- 仅房主
- 仅 waiting

### 10) 房主移除 AI

`DELETE /api/v1/rooms/{roomId}/ai/{aiUserId}`

约束：
- 仅房主
- 仅 waiting

---

### 11) 获取房间游戏状态（轮询）

`GET /api/v1/rooms/{roomId}/state?sinceVersion=12`

响应包含：
- `roomPlayers[].isAi`
- `roomPlayers[].aiManaged`
- `game.players[].isAi`
- `game.players[].aiManaged`
- `aiMemory`

### 12) 切换 AI 托管（当前玩家）

`POST /api/v1/rooms/{roomId}/ai-managed`

请求：
```json
{
  "enabled": true
}
```

说明：
- 仅房间内真人玩家可切换自己
- `enabled=true` 需要服务端 AI 已启用
- 托管开启后，`check/call/bet/allin/fold` 等手动动作会被拒绝

### 13) 提交动作

`POST /api/v1/rooms/{roomId}/actions`

请求：
```json
{
  "actionId": "a-1700000000-1",
  "type": "check",
  "expectedVersion": 13
}
```

响应：
```json
{
  "ok": true,
  "stateVersion": 14
}
```

版本冲突（409）：
```json
{
  "error": "version conflict",
  "stateVersion": 14
}
```

---

## 错误码约定

- `400`: 参数错误 / 状态不允许（如非房主开局、当前局未结束就 next-hand）
- `401`: 未登录或会话失效
- `404`: 资源不存在（如房间不存在）
- `409`: 版本冲突（`expectedVersion` 不匹配）

## 游戏规则（MVP）

- 每人 2 张底牌
- 公共牌按 flop(3) / turn(1) / river(1)
- 动作：`check / call / bet / allin / fold`
- 新开局时，筹码 `<= 0` 的玩家不参与该局，整局流程会自动跳过该玩家
- 若只剩 1 人未弃牌，立即结束
- showdown：7 选 5 比较牌型并分配底池
- 不实现 side pot

## 测试

```bash
go test ./...
go test -race ./...
```
