package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// newTestRoomManager 返回干净的房间管理器，避免用例间相互污染
func newTestRoomManager() *RoomManager {
	return &RoomManager{rooms: make(map[string]*Room)}
}

// memberOf 构造测试成员
func memberOf(id uint, name string) *RoomMember {
	return &RoomMember{UserID: id, Username: name, Nickname: name, JoinedAt: 1}
}

func TestRoomManager_CreateAndList(t *testing.T) {
	m := newTestRoomManager()
	r1 := m.CreateRoom("房间A", 1, memberOf(1, "alice"), 4, "")
	if r1.ID == "" {
		t.Fatal("房间 ID 不应为空")
	}
	if !r1.Members[0].IsOwner {
		t.Fatal("创建者应为房主")
	}
	m.CreateRoom("房间B", 2, memberOf(2, "bob"), 4, "")

	if got := len(m.ListRooms(0)); got != 2 {
		t.Fatalf("房间总数应为 2，实际 %d", got)
	}
	if got := len(m.ListRooms(1)); got != 1 {
		t.Fatalf("按 gameID=1 过滤应得 1，实际 %d", got)
	}
}

func TestRoomManager_RoomIDFormat(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("x", 0, memberOf(1, "a"), 4, "")
	if len(r.ID) != 6 {
		t.Fatalf("房间号应为 6 位，实际 %q", r.ID)
	}
}

func TestRoomManager_MaxUsersClamp(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("x", 0, memberOf(1, "a"), 999, "")
	if r.MaxUsers != maxRoomUsers {
		t.Fatalf("超限人数应被裁剪为 %d，实际 %d", maxRoomUsers, r.MaxUsers)
	}
	r2 := m.CreateRoom("y", 0, memberOf(2, "b"), 0, "")
	if r2.MaxUsers != maxRoomUsers {
		t.Fatalf("非法人数应回退为 %d，实际 %d", maxRoomUsers, r2.MaxUsers)
	}
}

func TestRoomManager_PasswordCheck(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("加密房", 0, memberOf(1, "alice"), 4, "secret")
	if !r.HasPwd {
		t.Fatal("设置密码后 HasPwd 应为 true")
	}
	// 密码错误
	_, _, err := m.joinRoom(r.ID, memberOf(2, "bob"), "wrong", nil)
	if err != errBadPassword {
		t.Fatalf("错误密码应返回 errBadPassword，实际 %v", err)
	}
	// 密码正确
	_, _, err = m.joinRoom(r.ID, memberOf(2, "bob"), "secret", nil)
	if err != nil {
		t.Fatalf("正确密码应加入成功，实际 %v", err)
	}
}

func TestRoomManager_JoinDuplicateAndFull(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("房", 0, memberOf(1, "alice"), 2, "")
	// 房主创建时已预置成员条目但无连接，首次 WS 连接应成功附着（复用该条目）
	if _, _, err := m.joinRoom(r.ID, memberOf(1, "alice"), "", nil); err != nil {
		t.Fatalf("房主首次连接应成功，实际 %v", err)
	}
	// 同一账号再开第二条活动连接应被拒绝
	if _, _, err := m.joinRoom(r.ID, memberOf(1, "alice"), "", nil); err != errAlreadyJoined {
		t.Fatalf("同账号重复连接应返回 errAlreadyJoined，实际 %v", err)
	}
	// 加入第二名成员（满员）
	if _, _, err := m.joinRoom(r.ID, memberOf(2, "bob"), "", nil); err != nil {
		t.Fatalf("第二名成员应加入成功，实际 %v", err)
	}
	// 第三名成员（超出 maxUsers=2）
	if _, _, err := m.joinRoom(r.ID, memberOf(3, "carol"), "", nil); err != errRoomFull {
		t.Fatalf("超员应返回 errRoomFull，实际 %v", err)
	}
}

func TestRoomManager_JoinNotFound(t *testing.T) {
	m := newTestRoomManager()
	if _, _, err := m.joinRoom("nope", memberOf(1, "a"), "", nil); err != errRoomNotFound {
		t.Fatalf("不存在的房间应返回 errRoomNotFound，实际 %v", err)
	}
}

func TestRoomManager_CloseRoomOnlyOwner(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("房", 0, memberOf(1, "alice"), 4, "")
	if err := m.CloseRoom(r.ID, 2); err != errNotOwner {
		t.Fatalf("非房主关闭应返回 errNotOwner，实际 %v", err)
	}
	if err := m.CloseRoom(r.ID, 1); err != nil {
		t.Fatalf("房主关闭应成功，实际 %v", err)
	}
	if m.GetRoom(r.ID) != nil {
		t.Fatal("关闭后房间应不存在")
	}
}

func TestRoomManager_KickOnlyOwner(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("房", 0, memberOf(1, "alice"), 4, "")
	_, _, _ = m.joinRoom(r.ID, memberOf(2, "bob"), "", nil)

	// 非房主踢人失败
	if err := m.kickMember(r.ID, 2, 1); err != errNotOwner {
		t.Fatalf("非房主踢人应返回 errNotOwner，实际 %v", err)
	}
	// 房主踢人成功
	if err := m.kickMember(r.ID, 1, 2); err != nil {
		t.Fatalf("房主踢人应成功，实际 %v", err)
	}
	room := m.GetRoom(r.ID)
	if len(room.Members) != 1 {
		t.Fatalf("踢人后应剩 1 名成员，实际 %d", len(room.Members))
	}
}

func TestRoomManager_TransferOwner(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("房", 0, memberOf(1, "alice"), 4, "")
	_, _, _ = m.joinRoom(r.ID, memberOf(2, "bob"), "", nil)

	if err := m.transferOwner(r.ID, 1, 2); err != nil {
		t.Fatalf("转让房主应成功，实际 %v", err)
	}
	room := m.GetRoom(r.ID)
	if room.OwnerID != 2 {
		t.Fatalf("OwnerID 应为 2，实际 %d", room.OwnerID)
	}
	for _, mem := range room.Members {
		if mem.UserID == 2 && !mem.IsOwner {
			t.Fatal("新成员应为房主")
		}
		if mem.UserID == 1 && mem.IsOwner {
			t.Fatal("原房主应失去房主标记")
		}
	}
	// 原房主再次转让应被拒
	if err := m.transferOwner(r.ID, 1, 1); err != errNotOwner {
		t.Fatalf("非房主转让应返回 errNotOwner，实际 %v", err)
	}
}

func TestRoomManager_LeaveTransfersOwnership(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("房", 0, memberOf(1, "alice"), 4, "")
	_, client, _ := m.joinRoom(r.ID, memberOf(2, "bob"), "", nil)
	_ = client

	// 模拟房主连接离开
	ownerClient := &RoomClient{Member: memberOf(1, "alice"), Send: make(chan []byte, 8)}
	m.mu.Lock()
	m.rooms[r.ID].clients[ownerClient] = true
	m.mu.Unlock()

	m.leaveRoom(r.ID, ownerClient)
	room := m.GetRoom(r.ID)
	if room == nil {
		t.Fatal("房间仍应有成员，不应被移除")
	}
	if room.OwnerID != 2 {
		t.Fatalf("房主离开后应自动转让给 2，实际 %d", room.OwnerID)
	}
}

func TestRoomManager_SnapshotIsolation(t *testing.T) {
	m := newTestRoomManager()
	r := m.CreateRoom("房", 0, memberOf(1, "alice"), 4, "")
	snap := m.GetRoom(r.ID)
	snap.Members[0].Nickname = "hacked"
	// 内部状态不应被外部快照修改影响
	again := m.GetRoom(r.ID)
	if again.Members[0].Nickname == "hacked" {
		t.Fatal("快照应深拷贝成员，外部修改不得影响内部状态")
	}
}

func TestRoomErrStatus(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{errRoomNotFound, http.StatusNotFound},
		{errNotOwner, http.StatusForbidden},
		{errBadPassword, http.StatusUnauthorized},
		{errRoomFull, http.StatusConflict},
		{errAlreadyJoined, http.StatusConflict},
	}
	for _, c := range cases {
		if got := roomErrStatus(c.err); got != c.want {
			t.Errorf("%v：got=%d want=%d", c.err, got, c.want)
		}
	}
}

func TestCreateRoomHandler(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()
	roomManager = newTestRoomManager()

	c, w := newCtx(http.MethodPost, "/api/rooms", gin.H{
		"name": "测试房", "game_id": 5, "max_users": 3,
	}, nil, 1)
	CreateRoom(c)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", w.Code)
	}
	var resp struct {
		Room *Room `json:"room"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if resp.Room == nil || resp.Room.GameID != 5 {
		t.Fatalf("房间创建结果不符: %+v", resp.Room)
	}
}

func TestGetRoomDetailHandler_NotFound(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()
	roomManager = newTestRoomManager()

	c, w := newCtx(http.MethodGet, "/api/rooms/xxxxxx", nil, gin.Params{{Key: "id", Value: "xxxxxx"}}, 1)
	GetRoomDetail(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在的房间应返回 404，实际 %d", w.Code)
	}
}

func TestDeleteRoomHandler_OnlyOwner(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()
	roomManager = newTestRoomManager()

	c, w := newCtx(http.MethodPost, "/api/rooms", gin.H{"name": "房"}, nil, 1)
	CreateRoom(c)
	var created struct {
		Room *Room `json:"room"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// 非房主删除
	c2, w2 := newCtx(http.MethodDelete, "/api/rooms/"+created.Room.ID, nil,
		gin.Params{{Key: "id", Value: created.Room.ID}}, 2)
	DeleteRoom(c2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("非房主关闭应返回 403，实际 %d", w2.Code)
	}

	// 房主删除
	c3, w3 := newCtx(http.MethodDelete, "/api/rooms/"+created.Room.ID, nil,
		gin.Params{{Key: "id", Value: created.Room.ID}}, 1)
	DeleteRoom(c3)
	if w3.Code != http.StatusOK {
		t.Fatalf("房主关闭应返回 200，实际 %d", w3.Code)
	}
}

func TestListRoomsHandler(t *testing.T) {
	teardown := setupTestDB(t)
	defer teardown()
	roomManager = newTestRoomManager()
	roomManager.CreateRoom("A", 1, memberOf(1, "a"), 4, "")
	roomManager.CreateRoom("B", 2, memberOf(2, "b"), 4, "")

	c, w := newCtx(http.MethodGet, "/api/rooms?game_id=1", nil, nil, 1)
	ListRooms(c)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码应为 200，实际 %d", w.Code)
	}
	var rooms []*Room
	if err := json.Unmarshal(w.Body.Bytes(), &rooms); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if len(rooms) != 1 || rooms[0].GameID != 1 {
		t.Fatalf("按 game_id=1 过滤结果不符: %+v", rooms)
	}
}
