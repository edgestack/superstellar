package server

import (
	"errors"
	"io"
	"log"
	"superstellar/backend/pb"
	"superstellar/backend/space"
	"time"

	"github.com/golang/protobuf/proto"

	"golang.org/x/net/websocket"
)

const channelBufSize = 100

// Client struct holds client-specific variables.
type Client struct {
	id       uint32
	username string
	ws       *websocket.Conn
	server   *Server
	ch       chan *[]byte
	doneCh   chan bool
	monitor  *Monitor
}

// NewClient initializes a new Client struct with given websocket and Server.
func NewClient(ws *websocket.Conn, server *Server) *Client {
	if ws == nil {
		panic("ws cannot be nil")
	}

	if server == nil {
		panic("server cannot be nil")
	}

	ch := make(chan *[]byte, channelBufSize)
	doneCh := make(chan bool)
	id := server.GenerateID()
	monitor := server.monitor

	return &Client{id, "", ws, server, ch, doneCh, monitor}
}

// Conn returns client's websocket.Conn struct.
func (c *Client) Conn() *websocket.Conn {
	return c.ws
}

// SendMessage sends game state to the client.
func (c *Client) SendMessage(bytes *[]byte) {
	select {
	case c.ch <- bytes:
	default:
		c.monitor.addDroppedMessage()
	}
}

// Done sends done message to the Client which closes the conection.
func (c *Client) Done() {
	c.doneCh <- true
}

// Listen Write and Read request via chanel
func (c *Client) Listen() {
	go c.listenWrite()
	c.listenRead()
}

// Listen write request via chanel
func (c *Client) listenWrite() {
	log.Println("Listening write to client")
	for {
		select {

		case bytes := <-c.ch:
			before := time.Now()
			err := websocket.Message.Send(c.ws, *bytes)
			after := time.Now()

			if err != nil {
				log.Println(err)
			} else {
				elapsed := after.Sub(before)
				c.monitor.addSendTime(elapsed)
			}

		case <-c.doneCh:
			c.server.Del(c)
			c.doneCh <- true
			return
		}
	}
}

func (c *Client) listenRead() {
	log.Println("Listening read from client")
	for {
		select {

		case <-c.doneCh:
			c.server.Del(c)
			c.doneCh <- true
			return

		default:
			c.readFromWebSocket()
		}
	}
}

func (c *Client) readFromWebSocket() {
	var data []byte
	err := websocket.Message.Receive(c.ws, &data)
	if err == io.EOF {
		c.doneCh <- true
	} else if err != nil {
		c.server.Err(err)
	} else {
		c.unmarshalUserInput(data)
	}
}

func (c *Client) unmarshalUserInput(data []byte) {
	protoUserMessage := &pb.UserMessage{}
	if err := proto.Unmarshal(data, protoUserMessage); err != nil {
		log.Fatalln("Failed to unmarshal UserInput:", err)
		return
	}

	switch x := protoUserMessage.Content.(type) {
	case *pb.UserMessage_UserAction:
		userInput := space.UserInputFromProto(protoUserMessage.GetUserAction().UserInput, c.id)
		c.server.UserInput(userInput)
	case *pb.UserMessage_JoinGame:
		c.tryToJoinGame(protoUserMessage.GetJoinGame())
	default:
		log.Fatalln("Unknown message type %T", x)
	}
}

func (c *Client) tryToJoinGame(joinGameMsg *pb.JoinGame) {
	username := joinGameMsg.Username
	ok, err := validateUsername(username)

	if !ok {
		c.server.SendJoinGameAckMessage(
			c, &pb.JoinGameAck{Success: ok, Error: err.Error()},
		)
		return
	}

	c.username = username
	c.server.JoinGame(c)
}

func validateUsername(username string) (bool, error) {
	length := len(username)

	if length < 3 {
		return false, errors.New("I doubt your name is shorter than 3 characters, Captain.")
	}

	if length > 25 {
		return false, errors.New("Space fleet doesn't allow names longer than 25 characters!")
	}

	return true, nil
}