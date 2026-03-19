package main

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient manages a WebSocket connection with auto-reconnect.
type WSClient struct {
	// TCP mode: ws:// URL. Sandbox mode: sandbox name.
	addr    string
	sandbox string // non-empty = tunnel through docker sandbox exec
	port    int    // port inside the sandbox (default 3333)
	conn      *websocket.Conn
	mu        sync.Mutex
	connected bool
	incoming  chan WSMessage
}

// NewWSClient creates a WebSocket client.
// If sandbox is non-empty, connections tunnel through "docker sandbox exec".
func NewWSClient(addr string, sandbox string, port int) *WSClient {
	return &WSClient{
		addr:     addr,
		sandbox:  sandbox,
		port:     port,
		incoming: make(chan WSMessage, 64),
	}
}

// Connect establishes the WebSocket connection and sends a hello.
func (c *WSClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var conn *websocket.Conn
	var err error

	if c.sandbox != "" {
		// Tunnel mode: bridge TCP through docker sandbox exec + Node.js
		dialer := websocket.Dialer{
			NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialSandbox(c.sandbox, c.port)
			},
			HandshakeTimeout: 10 * time.Second,
		}
		conn, _, err = dialer.Dial("ws://localhost/", nil)
	} else {
		conn, _, err = websocket.DefaultDialer.Dial(c.addr, nil)
	}
	if err != nil {
		return err
	}
	c.conn = conn
	c.connected = true

	// Send hello
	hello := WSMessage{Type: MsgTypeHello, SenderName: "User"}
	data, _ := json.Marshal(hello)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		conn.Close()
		c.connected = false
		return err
	}

	// Request history on connect
	histReq := WSMessage{Type: MsgTypeHistoryRequest, Limit: 50}
	data, _ = json.Marshal(histReq)
	_ = conn.WriteMessage(websocket.TextMessage, data)

	return nil
}

// ReadLoop reads messages from the WebSocket and pushes them to the incoming channel.
// Blocks until the connection is closed or context is cancelled.
func (c *WSClient) ReadLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()

			select {
			case c.incoming <- WSMessage{Type: MsgTypeDisconnected}:
			default:
			}
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		select {
		case c.incoming <- msg:
		default:
		}
	}
}

// Send sends a message to the WebSocket server.
func (c *WSClient) Send(msg WSMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return websocket.ErrCloseSent
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// Close cleanly closes the WebSocket connection.
func (c *WSClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
}

// IsConnected returns whether the client has an active connection.
func (c *WSClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}
