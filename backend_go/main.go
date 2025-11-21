package main

import (
	"backend_go/game"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"

	socket "github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"
)

const (
	waitingRoomLockKey = "waitingRoom"
	roomIDLength       = 6
	roomIDAlphabet     = "abcdefghijklmnopqrstuvwxyz0123456789"
)

type WaitingRoom struct {
	Players   []string `json:"players"`
	TimeStamp int64    `json:"timeStamp"`
}

type GameStartPayload struct {
	RoomID string `json:"roomId"`
	*game.RoomState
}

type PlayerMovePayload struct {
	RoomID   string
	MoveType string
	Pos      [2]int
	Player   int
	Down     bool
}

type GameState struct {
	io *socket.Server

	waitingMu    sync.RWMutex
	waitingRooms map[string]*WaitingRoom

	battleMu    sync.RWMutex
	battleRooms map[string]*game.RoomState

	locks *LockManager

	randMu     sync.Mutex
	randSource *rand.Rand
}

type gameOverEvent struct {
	roomID string
	winner string
}

type LockManager struct {
	mu    sync.Mutex
	locks map[string]struct{}
}

func main() {
	opts := socket.DefaultServerOptions()
	opts.SetCors(&types.Cors{
		Origin:      "*",
		Credentials: true,
	})
	opts.SetTransports(types.NewSet(socket.Polling, socket.WebSocket, socket.WebTransport))

	httpServer := types.NewWebServer(nil)
	io := socket.NewServer(httpServer, opts)
	state := NewGameState(io)

	io.On("connection", func(args ...any) {
		sock := args[0].(*socket.Socket)
		log.Printf("A user connected: %s", sock.Id())
		state.registerHandlers(sock)
	})

	httpServer.Listen("0.0.0.0:30602", func() {
		log.Println("Server running on http://0.0.0.0:30602")
	})

	select {}
}

func NewGameState(io *socket.Server) *GameState {
	return &GameState{
		io:           io,
		waitingRooms: make(map[string]*WaitingRoom),
		battleRooms:  make(map[string]*game.RoomState),
		locks:        NewLockManager(),
		randSource:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func NewLockManager() *LockManager {
	return &LockManager{locks: make(map[string]struct{})}
}

func (lm *LockManager) Acquire(key string) func() {
	for {
		lm.mu.Lock()
		if _, exists := lm.locks[key]; !exists {
			lm.locks[key] = struct{}{}
			lm.mu.Unlock()
			return func() {
				lm.mu.Lock()
				delete(lm.locks, key)
				lm.mu.Unlock()
			}
		}
		lm.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
}

func (gs *GameState) registerHandlers(sock *socket.Socket) {
	sock.On("createRoom", func(args ...any) {
		_, ack := splitAck(args)
		release := gs.locks.Acquire(waitingRoomLockKey)
		defer release()

		roomID := gs.generateRoomID()
		gs.waitingMu.Lock()
		gs.waitingRooms[roomID] = &WaitingRoom{Players: []string{}, TimeStamp: time.Now().UnixMilli()}
		gs.waitingMu.Unlock()

		gs.broadcastRoomsList()
		respondSuccess(ack, map[string]any{"message": "Room created", "roomId": roomID})
	})

	sock.On("requestRooms", func(args ...any) {
		_, ack := splitAck(args)
		if ack == nil {
			return
		}
		ack([]any{gs.listWaitingRooms()}, nil)
	})

	sock.On("joinRoom", func(args ...any) {
		payload, ack := splitAck(args)
		if len(payload) == 0 {
			respondError(ack, "Room does not exist")
			return
		}
		roomID, ok := payload[0].(string)
		if !ok {
			respondError(ack, "Room does not exist")
			return
		}

		release := gs.locks.Acquire(roomID)
		defer release()

		gs.waitingMu.Lock()
		room, exists := gs.waitingRooms[roomID]
		if !exists {
			gs.waitingMu.Unlock()
			respondError(ack, "Room does not exist")
			return
		}
		if len(room.Players) >= 2 {
			gs.waitingMu.Unlock()
			respondError(ack, "Room is full")
			return
		}

		playerID := string(sock.Id())
		if !containsPlayer(room.Players, playerID) {
			room.Players = append(room.Players, playerID)
		}
		playersCount := len(room.Players)
		gs.waitingMu.Unlock()

		sock.Join(socket.Room(roomID))
		respondSuccess(ack, map[string]any{"message": "Joined the room", "roomId": roomID})

		if playersCount == 2 {
			turn := gs.randomTurn()
			battleRoom := &game.RoomState{
				Players:       append([]string(nil), room.Players...),
				Turn:          turn,
				AllChess:      make([][3]int, 0),
				AnimatedChess: game.AnimatedChess{Player: turn, Pos: [2]int{1, 1}, Down: false},
				TimeStamp:     time.Now().UnixMilli(),
			}
			gs.battleMu.Lock()
			gs.battleRooms[roomID] = battleRoom
			gs.battleMu.Unlock()

			gs.waitingMu.Lock()
			delete(gs.waitingRooms, roomID)
			gs.waitingMu.Unlock()

			gs.broadcastRoomsList()
			payload := GameStartPayload{RoomID: roomID, RoomState: battleRoom.Clone()}
			gs.io.To(socket.Room(roomID)).Emit("gameStart", payload)
		}
	})

	sock.On("initGame", func(args ...any) {
		payload, ack := splitAck(args)
		if len(payload) == 0 {
			respondError(ack, "Room does not exist")
			return
		}
		roomID, ok := payload[0].(string)
		if !ok {
			respondError(ack, "Room does not exist")
			return
		}
		room := gs.snapshotBattleRoom(roomID)
		if room == nil {
			respondError(ack, "Room does not exist")
			return
		}
		respondSuccess(ack, map[string]any{"message": "Room exist", "room": room})
	})

	sock.On("playerMove", func(args ...any) {
		payload, ack := splitAck(args)
		if len(payload) == 0 {
			respondError(ack, "Invalid move")
			return
		}

		move, err := parsePlayerMovePayload(payload[0])
		if err != nil {
			respondError(ack, err.Error())
			return
		}

		release := gs.locks.Acquire(move.RoomID)
		defer release()

		gs.battleMu.RLock()
		room, exists := gs.battleRooms[move.RoomID]
		gs.battleMu.RUnlock()
		if !exists {
			respondError(ack, "Room does not exist")
			return
		}

		playerIndex := indexOfPlayer(room.Players, string(sock.Id()))
		if playerIndex == -1 {
			respondError(ack, "Not in the room")
			return
		}
		if room.Turn != playerIndex {
			respondError(ack, "Not your turn")
			return
		}
		if !game.IsMoveValid(room, move.Pos[0], move.Pos[1]) {
			respondError(ack, "Invalid move")
			return
		}

		if move.MoveType == "animatedChess" {
			room.AnimatedChess = game.AnimatedChess{Player: move.Player, Pos: move.Pos, Down: move.Down}
			respondSuccess(ack, map[string]any{"message": "animatedChess"})
			gs.emitRoomUpdate(move.RoomID)
			return
		}

		if move.MoveType != "addChess" {
			respondError(ack, "Invalid move")
			return
		}

		room.AllChess = append(room.AllChess, [3]int{move.Player, move.Pos[0], move.Pos[1]})
		if len(room.AllChess) == 7 {
			room.AllChess = room.AllChess[1:]
		}

		nextPlayer := (room.Turn + 1) % 2
		room.Turn = nextPlayer
		room.AnimatedChess = game.AnimatedChess{Player: nextPlayer, Pos: [2]int{1, 1}, Down: false}

		if result := game.CheckWinner(room); result != nil {
			room.AnimatedChess.Pos = [2]int{-1, -1}
			room.Turn = -1
			if len(result.WinPos) > 0 {
				room.AllChess = result.WinPos
			}
			gs.emitRoomUpdate(move.RoomID)
			time.AfterFunc(time.Second, func() {
				gs.io.To(socket.Room(move.RoomID)).Emit("gameOver", map[string]any{"winner": result.Winner})
				gs.deleteBattleRoom(move.RoomID)
			})
			respondSuccess(ack, map[string]any{"message": "addChess"})
			return
		}

		respondSuccess(ack, map[string]any{"room": room.Clone()})
		gs.emitRoomUpdate(move.RoomID)
	})

	sock.On("leaveRoom", func(args ...any) {
		payload, ack := splitAck(args)
		if len(payload) == 0 {
			respondError(ack, "Room does not exist")
			return
		}
		roomID, ok := payload[0].(string)
		if !ok {
			respondError(ack, "Room does not exist")
			return
		}

		release := gs.locks.Acquire(roomID)
		defer release()

		gs.battleMu.RLock()
		room, exists := gs.battleRooms[roomID]
		gs.battleMu.RUnlock()
		if !exists {
			respondError(ack, "Room does not exist")
			return
		}
		playerIndex := indexOfPlayer(room.Players, string(sock.Id()))
		if playerIndex == -1 {
			respondError(ack, "Not in the room")
			return
		}
		winner := ""
		if len(room.Players) == 2 {
			winner = room.Players[1-playerIndex]
		}
		gs.deleteBattleRoom(roomID)
		gs.io.To(socket.Room(roomID)).Emit("gameOver", map[string]any{"winner": winner})
		respondSuccess(ack, map[string]any{"message": "Room left"})
	})

	sock.On("disconnect", func(args ...any) {
		log.Printf("A user disconnected: %s", sock.Id())
		gs.handleDisconnect(string(sock.Id()))
	})
}

func (gs *GameState) handleDisconnect(socketID string) {
	gs.waitingMu.Lock()
	for roomID, room := range gs.waitingRooms {
		filtered := filterPlayers(room.Players, socketID)
		room.Players = filtered
		if len(room.Players) == 0 {
			delete(gs.waitingRooms, roomID)
		}
	}
	gs.waitingMu.Unlock()

	events := gs.removeFromBattleRooms(socketID)
	for _, evt := range events {
		gs.io.To(socket.Room(evt.roomID)).Emit("gameOver", map[string]any{"winner": evt.winner})
	}
	gs.broadcastRoomsList()
}

func (gs *GameState) removeFromBattleRooms(socketID string) []gameOverEvent {
	gs.battleMu.Lock()
	defer gs.battleMu.Unlock()
	var events []gameOverEvent
	for roomID, room := range gs.battleRooms {
		idx := indexOfPlayer(room.Players, socketID)
		if idx == -1 {
			continue
		}
		winner := ""
		if len(room.Players) == 2 {
			winner = room.Players[1-idx]
		}
		delete(gs.battleRooms, roomID)
		events = append(events, gameOverEvent{roomID: roomID, winner: winner})
	}
	return events
}

func (gs *GameState) deleteBattleRoom(roomID string) {
	gs.battleMu.Lock()
	delete(gs.battleRooms, roomID)
	gs.battleMu.Unlock()
}

func (gs *GameState) emitRoomUpdate(roomID string) {
	room := gs.snapshotBattleRoom(roomID)
	if room == nil {
		return
	}
	gs.io.To(socket.Room(roomID)).Emit("updateGame", map[string]any{"room": room})
}

func (gs *GameState) snapshotBattleRoom(roomID string) *game.RoomState {
	gs.battleMu.RLock()
	room, exists := gs.battleRooms[roomID]
	gs.battleMu.RUnlock()
	if !exists {
		return nil
	}
	return room.Clone()
}

func (gs *GameState) listWaitingRooms() []string {
	gs.waitingMu.RLock()
	defer gs.waitingMu.RUnlock()
	ids := make([]string, 0, len(gs.waitingRooms))
	for id := range gs.waitingRooms {
		ids = append(ids, id)
	}
	return ids
}

func (gs *GameState) broadcastRoomsList() {
	ids := gs.listWaitingRooms()
	gs.io.Emit("roomsList", ids)
}

func (gs *GameState) randomTurn() int {
	gs.randMu.Lock()
	defer gs.randMu.Unlock()
	return gs.randSource.Intn(2)
}

func (gs *GameState) generateRoomID() string {
	for {
		candidate := gs.randomRoomID()
		if !gs.waitingRoomExists(candidate) && !gs.battleRoomExists(candidate) {
			return candidate
		}
	}
}

func (gs *GameState) randomRoomID() string {
	buf := make([]byte, roomIDLength)
	gs.randMu.Lock()
	defer gs.randMu.Unlock()
	for i := range buf {
		buf[i] = roomIDAlphabet[gs.randSource.Intn(len(roomIDAlphabet))]
	}
	return string(buf)
}

func (gs *GameState) waitingRoomExists(id string) bool {
	gs.waitingMu.RLock()
	defer gs.waitingMu.RUnlock()
	_, exists := gs.waitingRooms[id]
	return exists
}

func (gs *GameState) battleRoomExists(id string) bool {
	gs.battleMu.RLock()
	defer gs.battleMu.RUnlock()
	_, exists := gs.battleRooms[id]
	return exists
}

func splitAck(args []any) ([]any, socket.Ack) {
	if len(args) == 0 {
		return args, nil
	}
	if ack, ok := args[len(args)-1].(socket.Ack); ok {
		return args[:len(args)-1], ack
	}
	return args, nil
}

func respondSuccess(ack socket.Ack, payload map[string]any) {
	if ack == nil {
		return
	}
	payload["success"] = true
	ack([]any{payload}, nil)
}

func respondError(ack socket.Ack, message string) {
	if ack == nil {
		return
	}
	ack([]any{map[string]any{"success": false, "message": message}}, nil)
}

func parsePlayerMovePayload(arg any) (PlayerMovePayload, error) {
	data, ok := arg.(map[string]any)
	if !ok {
		return PlayerMovePayload{}, errors.New("Invalid move payload")
	}

	roomID, _ := data["roomId"].(string)
	moveType, _ := data["moveType"].(string)
	if roomID == "" || moveType == "" {
		return PlayerMovePayload{}, errors.New("Invalid move payload")
	}

	posValue, ok := data["pos"]
	if !ok {
		return PlayerMovePayload{}, errors.New("Invalid move payload")
	}
	pos, err := toPoint(posValue)
	if err != nil {
		return PlayerMovePayload{}, err
	}

	playerValue, ok := data["player"]
	if !ok {
		return PlayerMovePayload{}, errors.New("Invalid move payload")
	}
	playerIndex, ok := toInt(playerValue)
	if !ok {
		return PlayerMovePayload{}, errors.New("Invalid move payload")
	}

	down := false
	if downVal, exists := data["down"]; exists {
		if flag, ok := downVal.(bool); ok {
			down = flag
		}
	}

	return PlayerMovePayload{
		RoomID:   roomID,
		MoveType: moveType,
		Pos:      pos,
		Player:   playerIndex,
		Down:     down,
	}, nil
}

func toPoint(value any) ([2]int, error) {
	switch v := value.(type) {
	case []interface{}:
		return pointFromSlice(v)
	case [2]int:
		return v, nil
	case []int:
		if len(v) < 2 {
			return [2]int{}, errors.New("Invalid move payload")
		}
		return [2]int{v[0], v[1]}, nil
	case []float64:
		if len(v) < 2 {
			return [2]int{}, errors.New("Invalid move payload")
		}
		return [2]int{int(v[0]), int(v[1])}, nil
	}
	return [2]int{}, errors.New("Invalid move payload")
}

func pointFromSlice(values []interface{}) ([2]int, error) {
	if len(values) < 2 {
		return [2]int{}, errors.New("Invalid move payload")
	}
	x, ok := toInt(values[0])
	if !ok {
		return [2]int{}, errors.New("Invalid move payload")
	}
	y, ok := toInt(values[1])
	if !ok {
		return [2]int{}, errors.New("Invalid move payload")
	}
	return [2]int{x, y}, nil
}

func toInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

func containsPlayer(players []string, target string) bool {
	for _, p := range players {
		if p == target {
			return true
		}
	}
	return false
}

func indexOfPlayer(players []string, target string) int {
	for i, p := range players {
		if p == target {
			return i
		}
	}
	return -1
}

func filterPlayers(players []string, target string) []string {
	filtered := make([]string, 0, len(players))
	for _, p := range players {
		if p != target {
			filtered = append(filtered, p)
		}
	}
	return filtered
}
