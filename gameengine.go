package shed

import (
	"errors"
	"fmt"
	"sync"

	"github.com/minaorangina/shed/protocol"
)

var (
	ErrNilGame                = errors.New("game is nil")
	ErrTooFewPlayers          = errors.New("minimum of 2 players required")
	ErrTooManyPlayers         = errors.New("maximum of 4 players allowed")
	ErrNoPlayers              = errors.New("game has no players")
	ErrGameUnexpectedResponse = errors.New("game received unexpected response")
	ErrGameAwaitingResponse   = errors.New("game is awaiting a response")
)

// PlayState represents the state of the current game
// idle -> no game play (pre game and post game)
// InProgress -> game in progress
// paused -> game is paused
type PlayState int

func (gps PlayState) String() string {
	if gps == 0 {
		return "idle"
	} else if gps == 1 {
		return "InProgress"
	} else if gps == 2 {
		return "paused"
	}
	return ""
}

const (
	Idle PlayState = iota
	InProgress
	Paused
)

// GameEngine represents the engine of the game
type GameEngine interface {
	Start() error
	Send([]OutboundMessage)
	Players() Players
	ID() string
	CreatorID() string
	AddPlayer(Player) error
	RemovePlayer(Player)
	Receive(InboundMessage)
	PlayState() PlayState
}

// gameEngine represents the engine of the game

type gameEngine struct {
	id                       string
	creatorID                string
	playState                PlayState
	players                  Players
	registerCh, unregisterCh chan Player
	inboundCh                chan InboundMessage
	outboundCh               chan []OutboundMessage
	gameCh                   chan []InboundMessage
	game                     Game
}

// GameEngineOpts represents options for constructing a new GameEngine
type GameEngineOpts struct {
	GameID                   string
	CreatorID                string
	Players                  Players
	RegisterCh, UnregisterCh chan Player
	InboundCh                chan InboundMessage
	OutboundCh               chan []OutboundMessage
	GameCh                   chan []InboundMessage
	PlayState                PlayState
	Game                     Game
}

// NewGameEngine constructs a new GameEngine
func NewGameEngine(opts GameEngineOpts) (*gameEngine, error) {
	if opts.Game == nil {
		return nil, ErrNilGame
	}
	if opts.RegisterCh == nil {
		opts.RegisterCh = make(chan Player)
	}
	if opts.InboundCh == nil {
		opts.InboundCh = make(chan InboundMessage)
	}
	if opts.OutboundCh == nil {
		opts.OutboundCh = make(chan []OutboundMessage)
	}
	if opts.GameCh == nil {
		opts.GameCh = make(chan []InboundMessage)
	}
	engine := &gameEngine{
		id:           opts.GameID,
		creatorID:    opts.CreatorID,
		players:      opts.Players,
		registerCh:   opts.RegisterCh,
		unregisterCh: opts.UnregisterCh,
		inboundCh:    opts.InboundCh,
		outboundCh:   opts.OutboundCh,
		gameCh:       opts.GameCh,
		playState:    opts.PlayState,
		game:         opts.Game,
	}

	// Listen for websocket connections
	go engine.Listen()

	return engine, nil
}

// Start starts a game
func (ge *gameEngine) Start() error {
	if ge.playState != Idle {
		return nil
	}
	if ge.game == nil {
		return ErrNilGame
	}

	err := ge.game.Start(ge.players.IDs())
	if err != nil {
		return err
	}

	// mutex
	ge.playState = InProgress

	return nil
}

func (ge *gameEngine) Play() {
	for inbound := range ge.gameCh {
		var (
			outbound []OutboundMessage
			err      error
		)

		if len(inbound) == 0 {
			outbound, err = ge.game.Next()
		} else {
			outbound, err = ge.game.ReceiveResponse(inbound)
		}
		if err != nil {
			// err
		}

		ge.Send(outbound)
	}
}

// Listen forwards outbound messages to target Players
// outside of the interface
func (ge *gameEngine) Listen() {
	commTracker := struct {
		mu              *sync.Mutex
		messages        []InboundMessage
		expectedCommand protocol.Cmd
	}{
		mu:              &sync.Mutex{},
		messages:        []InboundMessage{},
		expectedCommand: protocol.Start,
	}

	for {
		select {
		case joiner := <-ge.registerCh:
			ps := ge.Players()
			ge.players = AddPlayer(ps, joiner)
			for _, p := range ge.players {
				if p.ID() == joiner.ID() {
					continue
				}
				outbound := buildNewJoinerMessage(joiner, p)
				p.Send(outbound)
			}

		case leaver := <-ge.unregisterCh:
			ps := ge.Players()
			target, ok := ps.Find(leaver.ID())
			if ok {
				underlyingPlayer, typeOK := target.(*WSPlayer)
				if !typeOK {
					panic("this shouldn't have happened")
				}
				underlyingPlayer.conn = nil
			}

		case msgs := <-ge.outboundCh:
			ge.messagePlayers(msgs)

			if ge.game.AwaitingResponse() {
				// set expected command in a more robust way
				commTracker.mu.Lock()
				commTracker.expectedCommand = msgs[0].Command
				commTracker.mu.Unlock()
			} else {
				ge.sendToGame(nil)
			}

		case msg := <-ge.inboundCh:
			// Ignore messages that are not expected
			if msg.Command != commTracker.expectedCommand {
				continue
			}

			switch msg.Command {

			case protocol.Start:
				err := ge.game.Start(ge.players.IDs())
				if err != nil {
					p, _ := ge.players.Find(msg.PlayerID)

					p.Send(OutboundMessage{
						PlayerID: msg.PlayerID,
						Command:  protocol.Error,
						Error:    err.Error(),
					})
				}

				ge.Start()

				for _, p := range ge.players {
					p.Send(buildGameHasStartedMessage(p)) // broadcast
				}

			case protocol.Reorg:

				commTracker.messages = append(commTracker.messages, msg)
				if len(commTracker.messages) == len(ge.Players()) {
					commTracker.mu.Lock()

					ge.sendToGame(commTracker.messages)

					commTracker.messages = []InboundMessage{}
					commTracker.expectedCommand = protocol.Null

					commTracker.mu.Unlock()
				}

			default:
				ge.sendToGame([]InboundMessage{msg}) // handle failures
				commTracker.mu.Lock()
				commTracker.expectedCommand = msg.Command
				commTracker.mu.Unlock()
			}
		}
	}
}

func (ge *gameEngine) messagePlayers(msgs []OutboundMessage) {
	for _, m := range msgs {
		p, ok := ge.players.Find(m.PlayerID)
		if ok {
			p.Send(m)
		}
	}
}

func (ge *gameEngine) sendToGame(msgs []InboundMessage) {
	ge.gameCh <- msgs
}

func (ge *gameEngine) Send(msgs []OutboundMessage) {
	ge.outboundCh <- msgs
}

// Receive forwards InboundMessages from Players for sorting
func (ge *gameEngine) Receive(msg InboundMessage) {
	ge.inboundCh <- msg
}

// AddPlayer adds a player to a game
func (ge *gameEngine) AddPlayer(p Player) error {
	if ge.playState != Idle {
		return errors.New("cannot add player - game has started")
	}
	ge.registerCh <- p
	return nil
}

func (ge *gameEngine) RemovePlayer(p Player) {
	ge.unregisterCh <- p
}

func (ge *gameEngine) ID() string {
	return ge.id
}

func (ge *gameEngine) CreatorID() string {
	return ge.creatorID
}

func (ge *gameEngine) Players() Players {
	return ge.players
}

func (ge *gameEngine) PlayState() PlayState {
	return ge.playState
}

func buildGameHasStartedMessage(recipient Player) OutboundMessage {
	return OutboundMessage{
		PlayerID: recipient.ID(),
		Name:     recipient.Name(),
		Message:  fmt.Sprintf("STARTED"),
		Command:  protocol.HasStarted,
	}
}

func buildNewJoinerMessage(joiner, recipient Player) OutboundMessage {
	return OutboundMessage{
		PlayerID: recipient.ID(),
		Name:     recipient.Name(),
		Message:  fmt.Sprintf("%s has joined the game!", joiner.Name()),
		Command:  protocol.NewJoiner,
	}
}
