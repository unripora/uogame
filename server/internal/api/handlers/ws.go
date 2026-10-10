package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"uogame-go/internal/api/middleware"
	"uogame-go/internal/database"
	"uogame-go/internal/models"
)

// Client WebSocket 客户端连接
type Client struct {
	ID          uint
	Username    string
	Nickname    string
	Role        string
	AccountCode string
	IsManager   bool
	Conn        *websocket.Conn
	Send        chan []byte
}

// ChatManager 聊天广播中心
type ChatManager struct {
	Clients    map[*Client]bool
	Broadcast  chan []byte
	Register   chan *Client
	Unregister chan *Client
	mu         sync.RWMutex
}

// chatManager 全局聊天管理器实例
var chatManager = &ChatManager{
	Clients:    make(map[*Client]bool),
	Broadcast:  make(chan []byte, 256),
	Register:   make(chan *Client),
	Unregister: make(chan *Client),
}

// upgrader WebSocket 升级器（与原版一致允许同源）
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// ChatManagerRun 启动全局聊天广播中心（供 main 调用）
func ChatManagerRun() {
	chatManager.Run()
}

// Run 启动广播中心事件循环（main 中以 goroutine 调用）
func (m *ChatManager) Run() {
	for {
		select {
		case client := <-m.Register:
			m.mu.Lock()
			m.Clients[client] = true
			m.mu.Unlock()
		case client := <-m.Unregister:
			m.mu.Lock()
			if _, ok := m.Clients[client]; ok {
				delete(m.Clients, client)
				close(client.Send)
			}
			m.mu.Unlock()
		case message := <-m.Broadcast:
			m.mu.RLock()
			for client := range m.Clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(m.Clients, client)
				}
			}
			m.mu.RUnlock()
		}
	}
}

// WsChat 聊天 WebSocket 入口
func WsChat(c *gin.Context) {
	userID, _ := c.Get("userID")
	username, _ := c.Get("username")
	role, _ := c.Get("role")
	accountCode, _ := c.Get("accountCode")
	isManager := middleware.IsManagerRequest(c)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	client := &Client{
		ID:          toUint(userID),
		Username:    toString(username),
		Nickname:    lookupNickname(toUint(userID), isManager),
		Role:        toString(role),
		AccountCode: toString(accountCode),
		IsManager:   isManager,
		Conn:        conn,
		Send:        make(chan []byte, 256),
	}
	chatManager.Register <- client

	go client.readPump()
	go client.writePump()
}

// readPump 读取客户端消息并广播
func (c *Client) readPump() {
	defer func() {
		chatManager.Unregister <- c
		c.Conn.Close()
	}()
	c.Conn.SetReadLimit(4096)
	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		// 禁言校验（仅玩家体系；管理员不受禁言限制）
		if !c.IsManager && isUserMuted(c.ID) {
			c.Send <- []byte(`{"type":"error","message":"您已被管理员禁言，无法发送消息"}`)
			continue
		}
		// 解析客户端消息（前端发送 {username, nickname, content}）
		var incoming struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(msg, &incoming)
		if strings.TrimSpace(incoming.Content) == "" {
			continue
		}
		// 持久化聊天记录
		record := models.ChatMessage{
			UserID:      c.ID,
			Username:    c.Username,
			Nickname:    c.Nickname,
			Avatar:      lookupAvatar(c.ID, c.IsManager),
			Content:     incoming.Content,
			Timestamp:   time.Now().Unix(),
			IsManager:   boolToInt(c.IsManager),
			Role:        c.Role,
			AccountCode: c.AccountCode,
		}
		database.DB.Create(&record)
		// 广播完整消息体（与 history 接口字段一致，便于前端统一渲染）
		if out, err := json.Marshal(record); err == nil {
			chatManager.Broadcast <- out
		}
	}
}

// writePump 向客户端推送消息
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.Conn.WriteMessage(websocket.TextMessage, msg)
		case <-ticker.C:
			c.Conn.WriteMessage(websocket.PingMessage, nil)
		}
	}
}

// GetChatHistory 获取最近聊天记录
// 前端约定：返回**裸数组**（非对象包裹）
func GetChatHistory(c *gin.Context) {
	var messages []models.ChatMessage
	if err := database.DB.Order("timestamp desc").Limit(100).Find(&messages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch chat history"})
		return
	}
	// 反转为时间正序
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	if messages == nil {
		messages = []models.ChatMessage{}
	}
	c.JSON(http.StatusOK, messages)
}

// ClearChatHistory 清空聊天记录
func ClearChatHistory(c *gin.Context) {
	if err := database.DB.Where("1 = 1").Delete(&models.ChatMessage{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear chat history"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "聊天记录已清空"})
}

// lookupNickname 查询账号展示名（根据账号体系查对应表）
func lookupNickname(id uint, isManager bool) string {
	if isManager {
		var m models.SysManager
		if err := database.DB.First(&m, id).Error; err != nil {
			return "管理员"
		}
		return managerDisplayName(m)
	}
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		return "游客"
	}
	return displayName(u)
}

// lookupAvatar 查询账号头像（根据账号体系查对应表）
func lookupAvatar(id uint, isManager bool) string {
	if isManager {
		var m models.SysManager
		if err := database.DB.First(&m, id).Error; err != nil {
			return ""
		}
		return m.Avatar
	}
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		return ""
	}
	return u.Avatar
}

// isUserMuted 判断用户是否被禁言
func isUserMuted(id uint) bool {
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		return false
	}
	return u.IsMuted == 1
}

// managerDisplayName 返回管理员展示名：昵称优先，其次用户名，最后 "管理员"
func managerDisplayName(m models.SysManager) string {
	if m.Nickname != "" {
		return m.Nickname
	}
	if m.Username != "" {
		return m.Username
	}
	return "管理员"
}

// boolToInt 布尔转 0/1
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// toString 安全转换为字符串
func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
