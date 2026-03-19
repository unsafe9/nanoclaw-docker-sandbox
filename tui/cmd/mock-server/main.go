// mock-server starts a minimal WebSocket server that simulates the NanoClaw TUI channel.
// It echoes messages back with a fake assistant response, supports typing indicators,
// and serves history. Useful for developing and testing the TUI without running NanoClaw.
//
// Usage: go run ./cmd/mock-server [-port 3333]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type message struct {
	Content    string `json:"content"`
	SenderName string `json:"sender_name"`
	Timestamp  string `json:"timestamp"`
	IsBotMsg   bool   `json:"is_bot_message"`
}

var (
	history   []message
	historyMu sync.Mutex
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
)

func broadcast(msg map[string]interface{}) {
	data, _ := json.Marshal(msg)
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for c := range clients {
		_ = c.WriteMessage(websocket.TextMessage, data)
	}
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	defer conn.Close()

	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients, conn)
		clientsMu.Unlock()
	}()

	log.Printf("Client connected (total: %d)", len(clients))

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Client disconnected: %v", err)
			return
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}

		msgType, _ := raw["type"].(string)

		switch msgType {
		case "hello":
			resp := map[string]interface{}{
				"type":           "hello_ack",
				"session_id":     "tui:local",
				"assistant_name": "Andy",
			}
			respData, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, respData)
			log.Println("→ hello_ack sent")

		case "history_request":
			historyMu.Lock()
			resp := map[string]interface{}{
				"type":     "history",
				"messages": history,
			}
			historyMu.Unlock()
			respData, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, respData)
			log.Printf("→ history sent (%d messages)", len(history))

		case "message":
			content, _ := raw["content"].(string)
			senderName, _ := raw["sender_name"].(string)
			if senderName == "" {
				senderName = "User"
			}

			log.Printf("← [%s]: %s", senderName, content)

			// Store user message in history
			historyMu.Lock()
			history = append(history, message{
				Content:    content,
				SenderName: senderName,
				Timestamp:  time.Now().Format(time.RFC3339Nano),
				IsBotMsg:   false,
			})
			historyMu.Unlock()

			// Simulate typing delay
			typing := map[string]interface{}{
				"type":      "typing",
				"is_typing": true,
			}
			broadcast(typing)

			go func() {
				time.Sleep(1 * time.Second)

				// Stop typing
				broadcast(map[string]interface{}{
					"type":      "typing",
					"is_typing": false,
				})

				// Generate response
				response := generateResponse(content)

				ts := time.Now().Format(time.RFC3339Nano)
				respMsg := map[string]interface{}{
					"type":        "message",
					"content":     response,
					"sender_name": "Andy",
					"timestamp":   ts,
				}
				broadcast(respMsg)

				historyMu.Lock()
				history = append(history, message{
					Content:    response,
					SenderName: "Andy",
					Timestamp:  ts,
					IsBotMsg:   true,
				})
				historyMu.Unlock()

				log.Printf("→ [Andy]: %s", truncate(response, 80))
			}()
		}
	}
}

func generateResponse(input string) string {
	lower := strings.ToLower(input)

	switch {
	case strings.Contains(lower, "hello") || strings.Contains(lower, "hi"):
		return "Hello! I'm Andy, your NanoClaw assistant. How can I help you today?"

	case strings.Contains(lower, "code") || strings.Contains(lower, "example"):
		return `Sure! Here's an example:

` + "```python" + `
def fibonacci(n):
    """Generate the first n Fibonacci numbers."""
    a, b = 0, 1
    result = []
    for _ in range(n):
        result.append(a)
        a, b = b, a + b
    return result

print(fibonacci(10))
` + "```" + `

This generates: ` + "`[0, 1, 1, 2, 3, 5, 8, 13, 21, 34]`"

	case strings.Contains(lower, "markdown") || strings.Contains(lower, "format"):
		return `Here's some **markdown** to test rendering:

## Features
- **Bold text** and *italic text*
- ` + "`inline code`" + ` formatting
- [Links](https://example.com)

> This is a blockquote
> spanning multiple lines

1. First item
2. Second item
3. Third item

---

| Column A | Column B |
|----------|----------|
| Cell 1   | Cell 2   |
| Cell 3   | Cell 4   |`

	case strings.Contains(lower, "long"):
		return `This is a longer response to test scrolling behavior in the viewport.

The TUI should handle multi-paragraph responses gracefully, allowing you to scroll up and down through the message history.

Here are some key things to verify:
1. The viewport scrolls to the bottom when new messages arrive
2. You can scroll up with PgUp to see older messages
3. The typing indicator appears at the bottom of the viewport
4. Messages wrap correctly at the terminal width
5. Markdown rendering works for code blocks, bold, italic, etc.

If everything looks good, the TUI is working correctly!`

	default:
		return fmt.Sprintf("I received your message: \"%s\"\n\nThis is a mock response from the test server. The real NanoClaw agent would process this through Claude.", truncate(input, 100))
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func main() {
	port := flag.Int("port", 3333, "WebSocket server port")
	flag.Parse()

	http.HandleFunc("/", handleWS)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("Mock NanoClaw TUI server listening on ws://%s", addr)
	log.Printf("Commands: 'hello', 'code'/'example', 'markdown'/'format', 'long', or anything else")

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
