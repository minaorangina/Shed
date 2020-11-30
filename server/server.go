package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/minaorangina/shed"
	uuid "github.com/satori/go.uuid"
)

var (
	homepage        = "static/index.html"
	waitingRoomPage = "static/waiting-room.html"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type NewGameReq struct {
	Name string `json:"name"`
}

type NewGameRes struct {
	GameID   string `json:"game_id"`
	PlayerID string `json:"player_id"`
	Name     string `json:"name"`
}

type JoinGameReq struct {
	GameID string `json:"game_id"`
	Name   string `json:"name"`
}

type JoinGameRes struct {
	PlayerID string `json:"player_id"`
}

type GetGameRes struct {
	Status string `json:"status"`
	GameID string `json:"game_id"`
}

// GameServer is a game server
type GameServer struct {
	store shed.GameStore
	http.Server
}

func NewID() string {
	return uuid.NewV4().String()
}

func servePage(w http.ResponseWriter, path string) {
	tmpl, err := template.ParseFiles(path)

	if err != nil {
		http.Error(w, fmt.Sprintf("problem loading template %s", err.Error()), http.StatusInternalServerError)
		return
	}

	tmpl.Execute(w, nil)
}

// NewServer creates a new GameServer
func NewServer(store shed.GameStore) *GameServer {
	s := new(GameServer)

	router := http.NewServeMux()

	router.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Root endpoint")
		if r.URL.Path != "/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		servePage(w, homepage)
	}))
	router.Handle("/new", http.HandlerFunc(s.HandleNewGame))
	router.Handle("/game/", http.HandlerFunc(s.HandleFindGame))
	router.Handle("/join", http.HandlerFunc(s.HandleJoinGame))
	router.Handle("/waitingroom", http.HandlerFunc(s.HandleWaitingRoom))
	router.Handle("/ws", http.HandlerFunc(s.HandleWS))

	s.store = store

	s.Handler = router

	return s
}

// ServeHTTP serves http
func (g *GameServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.Handler.ServeHTTP(w, r)
}

// HandleNewGame handles a request to create a new game
func (g *GameServer) HandleNewGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var data NewGameReq
	err := json.NewDecoder(r.Body).Decode(&data)
	defer r.Body.Close()

	if err != nil {
		writeParseError(err, w, r)
		return
	}

	// generate game ID
	gameID, playerID := NewID(), NewID()
	game, err := shed.NewGameEngine(gameID, playerID, nil, nil)
	if err != nil {
		log.Println(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// get hub running
	go game.Listen()

	err = g.store.AddInactiveGame(game)
	if err != nil {
		log.Println(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = g.store.AddPendingPlayer(gameID, playerID, data.Name)
	if err != nil {
		log.Println(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	payload := NewGameRes{gameID, playerID, data.Name}
	bytes, err := json.Marshal(payload)
	if err != nil {
		log.Println(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Add("Content-Type", "application/json")
	w.Write(bytes)
}

func (g *GameServer) HandleFindGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	gameID := strings.Replace(r.URL.String(), "/game/", "", 1)
	if gameID == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var found bool
	response := GetGameRes{}

	game := g.store.FindActiveGame(gameID)
	if game != nil {
		response.Status = "active"
		response.GameID = gameID
		found = true
	} else {
		game := g.store.FindInactiveGame(gameID)
		if game != nil {
			response.Status = "pending"
			response.GameID = gameID
			found = true
		}
	}

	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	responseBytes, err := json.Marshal(response)
	if err != nil {
		log.Println(err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.Write(responseBytes)
}

func (g *GameServer) HandleJoinGame(w http.ResponseWriter, r *http.Request) {
	var data JoinGameReq
	err := json.NewDecoder(r.Body).Decode(&data)
	defer r.Body.Close()

	if err != nil {
		writeParseError(err, w, r)
		return
	}

	if data.GameID == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Missing game ID"))
		return
	}

	if data.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Missing player name"))
		return
	}

	// This step is repeated in AddPendingPlayer. One of these will have to go eventually.
	game := g.store.FindInactiveGame(data.GameID)
	if game == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("Game matching id '%s' not found", data.GameID)))
		return
	}

	playerID := NewID()

	err = g.store.AddPendingPlayer(data.GameID, playerID, data.Name)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	payload := JoinGameRes{playerID}
	bytes, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Add("Content-Type", "application/json")
	w.Write(bytes)
}

func (g *GameServer) HandleWaitingRoom(w http.ResponseWriter, r *http.Request) {
	// check if this person should get the file
	servePage(w, waitingRoomPage)
}

func (g *GameServer) HandleWS(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	vals, ok := query["game_id"]
	if !ok || len(vals) != 1 {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("could not parse game ID"))
		return
	}
	gameID := vals[0]

	vals, ok = query["player_id"]
	if !ok || len(vals) != 1 {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("could not parse user ID"))
		return
	}

	playerID := vals[0]

	game := g.store.FindInactiveGame(gameID)
	if game == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("unknown game id"))
		return
	}

	pendingPlayer := g.store.FindPendingPlayer(gameID, playerID)
	if pendingPlayer == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("unknown user id"))
		return
	}

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("could not upgrade to websocket: %v", err)))
		return
	}

	// create player
	player := shed.NewWSPlayer(playerID, pendingPlayer.Name, rawConn)

	// reference to hub etc
	err = game.AddPlayer(player)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("could not add player to game: %v", err)))
		return
	}
}

func writeParseError(err error, w http.ResponseWriter, r *http.Request) {
	if err == io.EOF {
		log.Println(err.Error())
		w.Header().Add("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Missing body"))
		return
	}
	if err != nil {
		log.Println(err.Error())
		w.Header().Add("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
