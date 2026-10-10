package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"uogame-go/internal/api/middleware"
)

// 多人联机房间（Netplay）——阶段一：内存房间 + REST + WS 信令/输入中继
//
// 设计（对标 romm-master 的房间思路，但适配 UOGame 原生 WebSocket 栈）：
//   - RoomManager：进程内内存存储，读写加锁；房间为短生命周期数据，无需落库
//   - REST：创建/列表/详情/关闭房间
//   - WS：房间通道，承载成员上下线、聊天、输入中继、房主管理事件
//   - 鉴权：沿用手写 JWT 中间件；房内敏感操作在事件层二次校验（房主）

// RoomMember 房间成员
type RoomMember struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	IsOwner  bool   `json:"is_owner"`
	JoinedAt int64  `json:"joined_at"`
}

// Room 房间（内存态）
type Room struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	GameID    uint          `json:"game_id"`
	OwnerID   uint          `json:"owner_id"`
	MaxUsers  int           `json:"max_users"`
	HasPwd    bool          `json:"has_password"`
	Status    string        `json:"status"` // waiting / playing
	CreatedAt int64         `json:"created_at"`
	Members   []*RoomMember `json:"members"`

	// 运行时（不序列化）：密码明文仅内存持有，用于加入校验
	pwd     string
	clients map[*RoomClient]bool
}

// RoomClient 房间内一条 WebSocket 连接
type RoomClient struct {
	Member *RoomMember
	Conn   *websocket.Conn
	Send   chan []byte
	mu     sync.Mutex // 串行化写入
}

// RoomManager 房间管理器（内存 + 读写锁）
type RoomManager struct {
	mu    sync.RWMutex
	rooms map[string]*Room
	seq   int64
}

// roomManager 全局房间管理器实例
var roomManager = &RoomManager{rooms: make(map[string]*Room)}

// maxRoomUsers 房间人数上限
const maxRoomUsers = 8

// 房间空置回收宽限期（最后一名成员离开后延迟回收，容忍断线重连）
const roomReapDelay = 30 * time.Second

// ---------- RoomManager 核心操作 ----------

// CreateRoom 创建房间（owner 自动成为房主成员）
func (m *RoomManager) CreateRoom(name string, gameID uint, owner *RoomMember, maxUsers int, pwd string) *Room {
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxUsers <= 0 || maxUsers > maxRoomUsers {
		maxUsers = maxRoomUsers
	}
	m.seq++
	room := &Room{
		ID:        newRoomID(m.seq),
		Name:      strings.TrimSpace(name),
		GameID:    gameID,
		OwnerID:   owner.UserID,
		MaxUsers:  maxUsers,
		HasPwd:    pwd != "",
		Status:    "waiting",
		CreatedAt: time.Now().Unix(),
		Members:   []*RoomMember{},
		pwd:       pwd,
		clients:   make(map[*RoomClient]bool),
	}
	owner.IsOwner = true
	room.Members = append(room.Members, owner)
	m.rooms[room.ID] = room
	return room
}

// GetRoom 查询房间（副本，避免外部直接改内部状态）
func (m *RoomManager) GetRoom(id string) *Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[id]
	if !ok {
		return nil
	}
	return r.snapshot()
}

// ListRooms 列出房间，可按 gameID 过滤（0 表示不过滤）
func (m *RoomManager) ListRooms(gameID uint) []*Room {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		if gameID != 0 && r.GameID != gameID {
			continue
		}
		out = append(out, r.snapshot())
	}
	return out
}

// CloseRoom 关闭房间（仅房主）
func (m *RoomManager) CloseRoom(id string, userID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[id]
	if !ok {
		return errRoomNotFound
	}
	if r.OwnerID != userID {
		return errNotOwner
	}
	m.broadcastLocked(r, gin.H{"type": "room_closed"})
	m.closeRoomLocked(r)
	return nil
}

// closeRoomLocked 关闭全部连接并移除房间（需持锁）
// 仅关闭发送通道，由 writePump 排空已排队消息（如 room_closed）后再自行关闭连接，
// 避免「立即 Conn.Close()」与异步写产生竞态导致末条消息丢失
func (m *RoomManager) closeRoomLocked(r *Room) {
	for c := range r.clients {
		close(c.Send)
	}
	r.clients = make(map[*RoomClient]bool)
	delete(m.rooms, r.ID)
}

// joinRoom 校验并加入房间（密码 / 满员 / 重复），返回连接客户端
func (m *RoomManager) joinRoom(id string, member *RoomMember, pwd string, conn *websocket.Conn) (*Room, *RoomClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[id]
	if !ok {
		return nil, nil, errRoomNotFound
	}
	if r.HasPwd && !secureEqual(r.pwd, pwd) {
		return nil, nil, errBadPassword
	}
	// 重复校验基于「活动连接」而非成员列表：
	// 房主在创建房间时已预置成员条目但尚无连接，应允许其 WS 连接附着
	for c := range r.clients {
		if c.Member.UserID == member.UserID {
			return nil, nil, errAlreadyJoined
		}
	}
	// 复用已存在的成员条目（创建时预置的房主 / 断线重连），否则新增并做满员校验
	var exist *RoomMember
	for _, mem := range r.Members {
		if mem.UserID == member.UserID {
			exist = mem
			break
		}
	}
	if exist != nil {
		exist.Username = member.Username
		exist.Nickname = member.Nickname
		member = exist
	} else {
		if len(r.Members) >= r.MaxUsers {
			return nil, nil, errRoomFull
		}
		r.Members = append(r.Members, member)
	}
	// 房主标识与 OwnerID 保持一致（含断线重连场景）
	member.IsOwner = member.UserID == r.OwnerID
	client := &RoomClient{Member: member, Conn: conn, Send: make(chan []byte, 128)}
	r.clients[client] = true
	m.broadcastLocked(r, gin.H{"type": "member_joined", "member": member})
	m.broadcastLocked(r, gin.H{"type": "members", "members": r.Members})
	return r.snapshot(), client, nil
}

// leaveRoom 移除成员（断线或主动离开）；空房启动延迟回收
func (m *RoomManager) leaveRoom(roomID string, client *RoomClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return
	}
	if _, ok := r.clients[client]; ok {
		delete(r.clients, client)
		close(client.Send)
	}
	// 移除成员
	for i, mem := range r.Members {
		if mem.UserID == client.Member.UserID {
			r.Members = append(r.Members[:i], r.Members[i+1:]...)
			break
		}
	}
	// 房主离开 → 转让给首位成员
	if client.Member.UserID == r.OwnerID && len(r.Members) > 0 {
		r.Members[0].IsOwner = true
		r.OwnerID = r.Members[0].UserID
		m.broadcastLocked(r, gin.H{"type": "owner_transferred", "owner_id": r.OwnerID})
	}
	m.broadcastLocked(r, gin.H{"type": "member_left", "user_id": client.Member.UserID})
	m.broadcastLocked(r, gin.H{"type": "members", "members": r.Members})

	// 空房回收（延迟，容忍短暂断线）
	if len(r.Members) == 0 {
		go func(id string) {
			time.Sleep(roomReapDelay)
			m.mu.Lock()
			defer m.mu.Unlock()
			if rr, ok := m.rooms[id]; ok && len(rr.Members) == 0 {
				m.closeRoomLocked(rr)
			}
		}(roomID)
	}
}

// kickMember 房主踢人
func (m *RoomManager) kickMember(roomID string, ownerID, targetID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return errRoomNotFound
	}
	if r.OwnerID != ownerID {
		return errNotOwner
	}
	for c := range r.clients {
		if c.Member.UserID == targetID {
			c.Send <- mustJSON(gin.H{"type": "kicked"})
			close(c.Send)
			// 不在此处 Close 连接：交由 writePump 先写出 kicked，再关闭，避免消息丢失
			delete(r.clients, c)
		}
	}
	for i, mem := range r.Members {
		if mem.UserID == targetID {
			r.Members = append(r.Members[:i], r.Members[i+1:]...)
			break
		}
	}
	m.broadcastLocked(r, gin.H{"type": "members", "members": r.Members})
	return nil
}

// transferOwner 房主转让
func (m *RoomManager) transferOwner(roomID string, ownerID, targetID uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return errRoomNotFound
	}
	if r.OwnerID != ownerID {
		return errNotOwner
	}
	found := false
	for _, mem := range r.Members {
		if mem.UserID == targetID {
			found = true
		}
		mem.IsOwner = mem.UserID == targetID
	}
	if !found {
		return errRoomNotFound
	}
	r.OwnerID = targetID
	m.broadcastLocked(r, gin.H{"type": "members", "members": r.Members})
	m.broadcastLocked(r, gin.H{"type": "owner_transferred", "owner_id": targetID})
	return nil
}

// broadcast 向房间内所有客户端广播（自行加写锁，供事件层调用）
func (m *RoomManager) broadcast(roomID string, payload gin.H) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return
	}
	m.broadcastLocked(r, payload)
}

// broadcastLocked 向房间内所有客户端广播（需持锁）
func (m *RoomManager) broadcastLocked(r *Room, payload gin.H) {
	msg := mustJSON(payload)
	for c := range r.clients {
		select {
		case c.Send <- msg:
		default:
			// 发送缓冲满：标记断开，交由 readPump 清理
			close(c.Send)
			if c.Conn != nil {
				_ = c.Conn.Close()
			}
			delete(r.clients, c)
		}
	}
}

// snapshot 生成房间副本（成员切片深拷贝）
func (r *Room) snapshot() *Room {
	members := make([]*RoomMember, len(r.Members))
	for i, m := range r.Members {
		cp := *m
		members[i] = &cp
	}
	return &Room{
		ID:        r.ID,
		Name:      r.Name,
		GameID:    r.GameID,
		OwnerID:   r.OwnerID,
		MaxUsers:  r.MaxUsers,
		HasPwd:    r.HasPwd,
		Status:    r.Status,
		CreatedAt: r.CreatedAt,
		Members:   members,
	}
}

// newRoomID 生成 6 位房间号（基于自增序号，避免随机碰撞）
func newRoomID(seq int64) string {
	n := seq
	s := strconv.FormatInt(n, 10)
	for len(s) < 6 {
		s = "0" + s
	}
	return s
}

// secureEqual 常数时间字符串比较（防时序侧信道）
func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// mustJSON 序列化为 JSON 字节（失败返回空对象，避免 panic）
func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// ---------- 错误定义 ----------

var (
	errRoomNotFound  = errRoom("房间不存在")
	errNotOwner      = errRoom("仅房主可执行此操作")
	errBadPassword   = errRoom("房间密码错误")
	errRoomFull      = errRoom("房间已满")
	errAlreadyJoined = errRoom("您已在该房间中")
)

type errRoom string

func (e errRoom) Error() string { return string(e) }

// ---------- REST handlers ----------

// CreateRoom 创建房间
// POST /api/rooms（登录即可）
func CreateRoom(c *gin.Context) {
	var payload struct {
		Name     string `json:"name"`
		GameID   uint   `json:"game_id"`
		MaxUsers int    `json:"max_users"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数格式错误"})
		return
	}
	owner := currentMember(c)
	room := roomManager.CreateRoom(payload.Name, payload.GameID, owner, payload.MaxUsers, payload.Password)
	c.JSON(http.StatusOK, gin.H{"message": "房间已创建", "room": room})
}

// ListRooms 房间列表（可按 game_id 过滤）
// GET /api/rooms?game_id=1
func ListRooms(c *gin.Context) {
	gameID, _ := strconv.Atoi(c.Query("game_id"))
	rooms := roomManager.ListRooms(uint(gameID))
	c.JSON(http.StatusOK, rooms)
}

// GetRoomDetail 房间详情
// GET /api/rooms/:id
func GetRoomDetail(c *gin.Context) {
	room := roomManager.GetRoom(c.Param("id"))
	if room == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errRoomNotFound.Error()})
		return
	}
	c.JSON(http.StatusOK, room)
}

// DeleteRoom 关闭房间（仅房主）
// DELETE /api/rooms/:id
func DeleteRoom(c *gin.Context) {
	userID := toUint(mustGet(c, "userID"))
	if err := roomManager.CloseRoom(c.Param("id"), userID); err != nil {
		c.JSON(roomErrStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "房间已关闭"})
}

// ---------- WebSocket 房间通道 ----------

// WsRoom 房间 WebSocket 入口
// GET /api/ws/room/:id（鉴权通过 Cookie 或 ?token=；密码经 ?password= 传入）
func WsRoom(c *gin.Context) {
	roomID := c.Param("id")
	userID := toUint(mustGet(c, "userID"))
	isManager := middleware.IsManagerRequest(c)
	member := &RoomMember{
		UserID:   userID,
		Username: toString(mustGet(c, "username")),
		Nickname: lookupNickname(userID, isManager),
		JoinedAt: time.Now().Unix(),
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	// 加入校验（满员/密码/重复）在事件层完成
	room, client, jerr := roomManager.joinRoom(roomID, member, c.Query("password"), conn)
	if jerr != nil {
		_ = conn.WriteMessage(websocket.TextMessage, mustJSON(gin.H{"type": "error", "message": jerr.Error()}))
		_ = conn.Close()
		return
	}
	// 回执当前房间快照，便于新成员追平状态
	client.Send <- mustJSON(gin.H{"type": "joined", "room": room, "self_id": userID})

	go roomReadPump(roomID, client)
	go roomWritePump(client)
}

// roomReadPump 读取房间内客户端消息：聊天 / 输入中继 / 房主管理
func roomReadPump(roomID string, client *RoomClient) {
	defer func() {
		roomManager.leaveRoom(roomID, client)
		_ = client.Conn.Close()
	}()
	client.Conn.SetReadLimit(8192)
	for {
		_, msg, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}
		var in struct {
			Type    string          `json:"type"`
			Content string          `json:"content"`
			Payload json.RawMessage `json:"payload"`
			Target  uint            `json:"target"`
		}
		if err := json.Unmarshal(msg, &in); err != nil {
			continue
		}
		switch in.Type {
		case "chat":
			// 房内聊天：广播带发送者信息的消息
			roomManager.broadcast(roomID, gin.H{
				"type":     "chat",
				"user_id":  client.Member.UserID,
				"nickname": client.Member.Nickname,
				"content":  strings.TrimSpace(in.Content),
			})
		case "input":
			// 游戏输入中继：转发原始 payload（由其他成员在本地实例回放）
			roomManager.broadcast(roomID, gin.H{
				"type":    "input",
				"user_id": client.Member.UserID,
				"payload": json.RawMessage(in.Payload),
			})
		case "kick":
			_ = roomManager.kickMember(roomID, client.Member.UserID, in.Target)
		case "transfer":
			_ = roomManager.transferOwner(roomID, client.Member.UserID, in.Target)
		case "close":
			_ = roomManager.CloseRoom(roomID, client.Member.UserID)
		}
	}
}

// roomWritePump 向客户端推送消息
func roomWritePump(client *RoomClient) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = client.Conn.Close()
	}()
	for {
		select {
		case msg, ok := <-client.Send:
			if !ok {
				client.mu.Lock()
				_ = client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				client.mu.Unlock()
				return
			}
			client.mu.Lock()
			err := client.Conn.WriteMessage(websocket.TextMessage, msg)
			client.mu.Unlock()
			if err != nil {
				return
			}
		case <-ticker.C:
			client.mu.Lock()
			err := client.Conn.WriteMessage(websocket.PingMessage, nil)
			client.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// ---------- 辅助函数 ----------

// currentMember 从上下文构造当前成员（REST 场景）
func currentMember(c *gin.Context) *RoomMember {
	userID := toUint(mustGet(c, "userID"))
	isManager := middleware.IsManagerRequest(c)
	return &RoomMember{
		UserID:   userID,
		Username: toString(mustGet(c, "username")),
		Nickname: lookupNickname(userID, isManager),
		JoinedAt: time.Now().Unix(),
	}
}

// mustGet 读取上下文键（不存在返回 nil）
func mustGet(c *gin.Context, key string) interface{} {
	v, _ := c.Get(key)
	return v
}

// roomErrStatus 将业务错误映射为 HTTP 状态码
func roomErrStatus(err error) int {
	switch err {
	case errRoomNotFound:
		return http.StatusNotFound
	case errNotOwner:
		return http.StatusForbidden
	case errBadPassword:
		return http.StatusUnauthorized
	case errRoomFull:
		return http.StatusConflict
	case errAlreadyJoined:
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

