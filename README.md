# Texas Yu（简易德州扑克）

一个使用 Go 编写的简化版德州扑克项目：
- 不使用数据库，所有状态在内存中
- 前端为静态页面
- 客户端通过轮询获取房间与游戏状态

## 启动

```bash
go run ./cmd/server
```

打开：
- `http://localhost:8080/index.html`（用户名输入页）
- `http://localhost:8080/rooms.html`（房间页）
- `http://localhost:8080/game.html?roomId=r-1`（游戏页）

## 页面与轮询

1. 用户名页：创建会话
2. 房间页：每 2 秒轮询房间列表，支持“退出大厅”并清理本地用户状态
3. 游戏页：
   - 自己回合约 700ms 轮询
   - 非自己回合约 1200ms 轮询

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
      "maxPlayers": 4,
      "ownerUserId": "u-1",
      "status": "waiting",
      "players": [
        {"userId":"u-1","username":"alice","seat":0}
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
  "maxPlayers": 4
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

### 6) 离开房间（新增）

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

响应：返回房间对象（`status=playing`，并带游戏状态）。

### 8) 下一局（新增）

`POST /api/v1/rooms/{roomId}/next-hand`

请求：
```json
{}
```

约束：
- 仅房主可调用
- 当前局必须已经 finished

响应：返回新一局初始化后的房间对象。

---

### 9) 获取房间游戏状态（轮询）

`GET /api/v1/rooms/{roomId}/state?sinceVersion=12`

有变化响应（示例）：
```json
{
  "roomId": "r-1",
  "roomName": "room-a",
  "roomStatus": "playing",
  "stateVersion": 13,
  "game": {
    "stage": "flop",
    "pot": 20,
    "dealerPos": 0,
    "turnPos": 1,
    "communityCards": [
      {"rank": 14, "suit": 3},
      {"rank": 10, "suit": 2},
      {"rank": 7, "suit": 1}
    ],
    "players": [
      {
        "userId": "u-1",
        "username": "alice",
        "seatIndex": 0,
        "stack": 190,
        "folded": false,
        "lastAction": "bet",
        "won": 0,
        "isTurn": false,
        "holeCards": [
          {"rank": 13, "suit": 0},
          {"rank": 13, "suit": 1}
        ]
      }
    ],
    "result": null
  }
}
```

无变化响应：
```json
{
  "notModified": true,
  "version": 13
}
```

### 10) 提交动作

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
- 动作：`check / call / bet / fold`
- 若只剩 1 人未弃牌，立即结束
- showdown：7 选 5 比较牌型并分配底池
- 不实现 side pot

## 测试

```bash
go test ./...
go test -race ./...
```
