package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Bubbletea messages
type (
	wsMessageMsg      WSMessage
	wsConnectedMsg    struct{}
	wsDisconnectedMsg struct{ err error }
	wsReconnectMsg    struct{ attempt int; err error }
)

type connState int

const (
	stateDisconnected connState = iota
	stateConnected
	stateReconnecting
)

const defaultMetaCount = 20

type model struct {
	textInput     textinput.Model
	viewport      viewport.Model
	ws            *WSClient
	messages      []chatMessage
	typing        bool
	width         int
	height        int
	assistantName string
	connState     connState
	statusText    string
	metaCount     int // number of recent messages to show (0 = show all)
	ready         bool
}

func initialModel(ws *WSClient) model {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.PromptStyle = inputPrefixStyle
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.CharLimit = 0 // No limit

	return model{
		textInput:     ti,
		ws:            ws,
		assistantName: "Andy", // Updated on hello_ack
		connState:     stateDisconnected,
		statusText:    "Connecting...",
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.connectCmd(),
	)
}

// bottomHeight returns the number of terminal rows used by the bottom section.
func (m model) bottomHeight() int {
	h := 3 // input box: top border + input line + bottom border
	if m.typing {
		h++
	}
	if m.statusText != "" {
		h++
	}
	return h
}

// syncViewportSize updates the viewport dimensions.
func (m *model) syncViewportSize() {
	if !m.ready {
		return
	}
	h := m.height - m.bottomHeight()
	if h < 1 {
		h = 1
	}
	m.viewport.Width = m.width
	m.viewport.Height = h
}

// updateViewport rebuilds the viewport content from messages.
func (m *model) updateViewport() {
	if !m.ready {
		return
	}
	var sb strings.Builder

	startIdx := 0
	if m.metaCount > 0 && m.metaCount < len(m.messages) {
		startIdx = len(m.messages) - m.metaCount
	}

	if startIdx > 0 {
		sb.WriteString(formatBanner(
			fmt.Sprintf("ctrl+e to show %d previous messages", startIdx), m.width))
		sb.WriteString("\n\n")
	}

	for i := startIdx; i < len(m.messages); i++ {
		showMeta := m.metaCount > 0
		sb.WriteString(formatMessage(m.messages[i], m.width, showMeta))
		if i < len(m.messages)-1 {
			sb.WriteString("\n\n")
		}
	}

	m.viewport.SetContent(sb.String())
}

// updateViewportAndScroll rebuilds content and scrolls to bottom.
func (m *model) updateViewportAndScroll() {
	m.updateViewport()
	m.viewport.GotoBottom()
}

// newChatMessage creates a chatMessage, parsing the timestamp and pre-rendering markdown.
func newChatMessage(senderName, content, timestamp string, isBotMessage bool) chatMessage {
	ts, _ := time.Parse(time.RFC3339Nano, timestamp)
	if ts.IsZero() {
		ts = time.Now()
	}
	cm := chatMessage{
		senderName:   senderName,
		content:      content,
		timestamp:    ts,
		isBotMessage: isBotMessage,
	}
	if cm.isBotMessage {
		cm.rendered = renderMarkdown(cm.content)
	}
	return cm
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cleanup()
			return m, tea.Quit
		case tea.KeyEnter:
			content := strings.TrimSpace(m.textInput.Value())
			if content != "" && m.connState == stateConnected {
				m.textInput.Reset()
				return m, m.sendMessageCmd(content)
			}
		case tea.KeyCtrlU:
			m.textInput.Reset()
		case tea.KeyCtrlO:
			if m.metaCount > 0 {
				m.metaCount = 0
			} else {
				m.metaCount = defaultMetaCount
			}
			m.updateViewportAndScroll()
			return m, nil
		case tea.KeyCtrlE:
			if m.metaCount > 0 {
				m.metaCount = len(m.messages)
				m.updateViewport()
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		widthChanged := m.width != msg.Width
		m.width = msg.Width
		m.height = msg.Height
		if widthChanged {
			initMarkdownRenderer(m.width)
		}
		m.textInput.Width = m.width - 4
		if !m.ready {
			m.viewport = viewport.New(m.width, m.height-m.bottomHeight())
			m.ready = true
		} else {
			m.syncViewportSize()
		}
		m.updateViewportAndScroll()

	case wsConnectedMsg:
		m.connState = stateConnected
		m.statusText = ""
		m.syncViewportSize()
		return m, m.listenCmd()

	case wsDisconnectedMsg:
		m.connState = stateReconnecting
		m.typing = false
		m.statusText = "Disconnected, reconnecting..."
		return m, m.tryConnectCmd(1)

	case wsReconnectMsg:
		m.connState = stateReconnecting
		if msg.err != nil {
			m.statusText = fmt.Sprintf("Connection failed: %v (attempt %d, retrying...)", msg.err, msg.attempt)
		} else {
			m.statusText = fmt.Sprintf("Connecting... (attempt %d)", msg.attempt)
		}
		m.syncViewportSize()
		return m, m.tryConnectCmd(msg.attempt)

	case wsMessageMsg:
		wsMsg := WSMessage(msg)
		switch wsMsg.Type {
		case MsgTypeHelloAck:
			if wsMsg.AssistantName != "" {
				m.assistantName = wsMsg.AssistantName
			}

		case MsgTypeMessage:
			cm := newChatMessage(wsMsg.SenderName, wsMsg.Content, wsMsg.Timestamp, wsMsg.SenderName != "User")
			m.messages = append(m.messages, cm)
			m.typing = false
			m.syncViewportSize()
			m.updateViewportAndScroll()

		case MsgTypeTyping:
			if wsMsg.IsTyping != nil {
				m.typing = *wsMsg.IsTyping
				m.syncViewportSize()
			}

		case MsgTypeHistory:
			for _, entry := range wsMsg.Messages {
				cm := newChatMessage(entry.SenderName, entry.Content, entry.Timestamp, entry.IsBotMessage)
				m.messages = append(m.messages, cm)
			}
			m.updateViewportAndScroll()

		case MsgTypeDisconnected:
			m.connState = stateReconnecting
			m.typing = false
			m.syncViewportSize()
			return m, m.tryConnectCmd(1)
		}

		return m, m.listenCmd()
	}

	// Update viewport (handles scrolling)
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	// Update text input
	var tiCmd tea.Cmd
	m.textInput, tiCmd = m.textInput.Update(msg)
	cmds = append(cmds, tiCmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	var sb strings.Builder
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")
	if m.typing {
		sb.WriteString(typingStyle.Render(fmt.Sprintf("%s is typing...", m.assistantName)))
		sb.WriteString("\n")
	}
	if m.statusText != "" {
		sb.WriteString(typingStyle.Render(m.statusText))
		sb.WriteString("\n")
	}
	sb.WriteString(inputBoxStyle.Width(m.width - 2).Render(m.textInput.View()))
	return sb.String()
}

// --- Commands ---

func (m *model) connectCmd() tea.Cmd {
	return func() tea.Msg {
		err := m.ws.Connect()
		if err != nil {
			return wsReconnectMsg{attempt: 1}
		}
		return wsConnectedMsg{}
	}
}

func (m *model) listenCmd() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.ws.incoming
		if !ok {
			return wsDisconnectedMsg{}
		}
		return wsMessageMsg(msg)
	}
}

func (m *model) sendMessageCmd(content string) tea.Cmd {
	return func() tea.Msg {
		msg := WSMessage{
			Type:       MsgTypeMessage,
			Content:    content,
			SenderName: "User",
		}
		_ = m.ws.Send(msg)

		return wsMessageMsg{
			Type:       MsgTypeMessage,
			Content:    content,
			SenderName: "User",
			Timestamp:  time.Now().Format(time.RFC3339Nano),
		}
	}
}

func (m *model) tryConnectCmd(attempt int) tea.Cmd {
	return func() tea.Msg {
		if attempt > 1 {
			delay := time.Duration(attempt) * time.Second
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			time.Sleep(delay)
		}
		err := m.ws.Connect()
		if err != nil {
			return wsReconnectMsg{attempt: attempt + 1, err: err}
		}
		return wsConnectedMsg{}
	}
}

func (m *model) cleanup() {
	m.ws.Close()
}
