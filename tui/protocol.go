package main

// WebSocket message types matching the TypeScript channel protocol.

type MessageType string

const (
	MsgTypeHello          MessageType = "hello"
	MsgTypeHelloAck       MessageType = "hello_ack"
	MsgTypeMessage        MessageType = "message"
	MsgTypeTyping         MessageType = "typing"
	MsgTypeHistoryRequest MessageType = "history_request"
	MsgTypeHistory        MessageType = "history"
	MsgTypeError          MessageType = "error"
	MsgTypeDisconnected   MessageType = "disconnected"
)

// WSMessage is the envelope for all WebSocket JSON messages.
type WSMessage struct {
	Type          MessageType    `json:"type"`
	Content       string         `json:"content,omitempty"`
	SenderName    string         `json:"sender_name,omitempty"`
	Timestamp     string         `json:"timestamp,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	AssistantName string         `json:"assistant_name,omitempty"`
	IsTyping      *bool          `json:"is_typing,omitempty"`
	Messages      []HistoryEntry `json:"messages,omitempty"`
	Limit         int            `json:"limit,omitempty"`
	Message       string         `json:"message,omitempty"` // error message
}

// HistoryEntry is a single message in a history response.
type HistoryEntry struct {
	Content      string `json:"content"`
	SenderName   string `json:"sender_name"`
	Timestamp    string `json:"timestamp"`
	IsBotMessage bool   `json:"is_bot_message"`
}
