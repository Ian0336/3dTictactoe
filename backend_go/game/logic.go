package game

var winningLines = [8][3]int{
	{0, 1, 2},
	{3, 4, 5},
	{6, 7, 8},
	{0, 3, 6},
	{1, 4, 7},
	{2, 5, 8},
	{0, 4, 8},
	{2, 4, 6},
}

type AnimatedChess struct {
	Player int    `json:"player"`
	Pos    [2]int `json:"pos"`
	Down   bool   `json:"down"`
}

type RoomState struct {
	Players       []string      `json:"players"`
	Turn          int           `json:"turn"`
	AllChess      [][3]int      `json:"allChess"`
	AnimatedChess AnimatedChess `json:"animatedChess"`
	TimeStamp     int64         `json:"timeStamp"`
}

// Clone creates a deep copy of RoomState
func (r *RoomState) Clone() *RoomState {
	if r == nil {
		return nil
	}
	clone := *r
	clone.Players = append([]string(nil), r.Players...)
	if len(r.AllChess) > 0 {
		clone.AllChess = append([][3]int(nil), r.AllChess...)
	} else {
		clone.AllChess = nil
	}
	return &clone
}

// GameResult captures the outcome of a game round.
type GameResult struct {
	Winner string   `json:"winner"`
	WinPos [][3]int `json:"winPos,omitempty"`
}

func CheckWinner(room *RoomState) *GameResult {
	if room == nil {
		return nil
	}

	board := make([]int, 9)
	for i := range board {
		board[i] = -1
	}

	for _, move := range room.AllChess {
		x := move[1]
		y := move[2]
		idx := x + y*3
		if idx >= 0 && idx < len(board) {
			board[idx] = move[0]
		}
	}

	for _, line := range winningLines {
		a, b, c := line[0], line[1], line[2]
		if board[a] != -1 && board[a] == board[b] && board[a] == board[c] {
			winnerIndex := board[a]
			if winnerIndex < 0 || winnerIndex >= len(room.Players) {
				break
			}
			winPos := make([][3]int, 0, 3)
			winPos = append(winPos, [3]int{winnerIndex, a % 3, a / 3})
			winPos = append(winPos, [3]int{winnerIndex, b % 3, b / 3})
			winPos = append(winPos, [3]int{winnerIndex, c % 3, c / 3})
			return &GameResult{Winner: room.Players[winnerIndex], WinPos: winPos}
		}
	}

	if len(room.AllChess) == 9 {
		return &GameResult{Winner: "draw"}
	}

	return nil
}

func IsMoveValid(room *RoomState, posX, posY int) bool {
	if room == nil {
		return false
	}
	for _, move := range room.AllChess {
		if move[1] == posX && move[2] == posY {
			return false
		}
	}
	return true
}
