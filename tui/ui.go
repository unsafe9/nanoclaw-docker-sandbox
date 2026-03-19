package main

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Styles
var (
	timeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	senderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	userMsgStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("237"))

	typingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Italic(true)

	inputPrefixStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Bold(true)

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false).
			BorderForeground(lipgloss.Color("8"))
)

// formatBanner renders a centered banner line with dashes on either side.
func formatBanner(text string, width int) string {
	textLen := lipgloss.Width(text)
	if width <= textLen+4 {
		return timeStyle.Render(text)
	}
	side := (width - textLen - 2) / 2
	dashes := strings.Repeat("─", side)
	return timeStyle.Render(dashes + " " + text + " " + dashes)
}

// chatMessage represents a single message in the viewport.
type chatMessage struct {
	senderName   string
	content      string
	timestamp    time.Time
	isBotMessage bool
	rendered     string // cached rendered content
}

const userPrefix = "> "
const userPrefixWidth = 2

// formatMessage renders a chat message for display.
// When showMeta is true, a header with timestamp + sender appears above content.
func formatMessage(msg chatMessage, width int, showMeta bool) string {
	var content string
	if msg.isBotMessage && msg.rendered != "" {
		content = msg.rendered
	} else if msg.isBotMessage {
		content = renderMarkdown(msg.content)
	} else {
		content = msg.content
	}

	contentWidth := width
	if !msg.isBotMessage {
		contentWidth = width - userPrefixWidth
	}
	if contentWidth < 20 {
		contentWidth = 20
	}

	lines := strings.Split(content, "\n")

	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, wrapLine(line, contentWidth)...)
	}

	var sb strings.Builder
	if showMeta {
		ts := timeStyle.Render(msg.timestamp.Local().Format("3:04 PM"))
		name := senderStyle.Render(msg.senderName)
		sb.WriteString(ts)
		sb.WriteString("  ")
		sb.WriteString(name)
		sb.WriteString("\n")
	}
	for i, line := range wrapped {
		if i > 0 {
			sb.WriteString("\n")
		}
		if !msg.isBotMessage {
			visLen := lipgloss.Width(line) + userPrefixWidth
			padded := userPrefix + line
			if visLen < width {
				padded += strings.Repeat(" ", width-visLen)
			}
			sb.WriteString(userMsgStyle.Render(padded))
		} else {
			sb.WriteString(line)
		}
	}
	return sb.String()
}

// wrapLine wraps a single line to fit within maxWidth visible columns.
// Handles ANSI escape sequences (zero width) and wide characters (CJK = 2 columns).
func wrapLine(line string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{line}
	}
	if lipgloss.Width(line) <= maxWidth {
		return []string{line}
	}

	var result []string
	remaining := line
	for lipgloss.Width(remaining) > maxWidth {
		cut := findBreak(remaining, maxWidth)
		if cut <= 0 {
			cut = findByteAtWidth(remaining, maxWidth)
			if cut <= 0 {
				break // safety: avoid infinite loop
			}
		}
		result = append(result, remaining[:cut])
		remaining = remaining[cut:]
		remaining = strings.TrimLeft(remaining, " ")
	}
	if len(remaining) > 0 {
		result = append(result, remaining)
	}
	return result
}

// findBreak finds the byte offset of the last space that fits within maxWidth
// visible columns. Returns 0 if no suitable space is found.
func findBreak(s string, maxWidth int) int {
	width := 0
	lastSpace := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			i = skipAnsi(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		if width+w > maxWidth {
			break
		}
		if r == ' ' {
			lastSpace = i
		}
		width += w
		i += size
	}
	return lastSpace
}

// findByteAtWidth returns the byte offset at exactly maxWidth visible columns.
// Used as a forced break point when no space is found.
func findByteAtWidth(s string, maxWidth int) int {
	width := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			i = skipAnsi(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		if width+w > maxWidth {
			break
		}
		width += w
		i += size
	}
	return i
}

// skipAnsi advances past an ANSI escape sequence starting at s[i].
func skipAnsi(s string, i int) int {
	j := i + 1
	if j < len(s) && s[j] == '[' {
		j++
		for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
			j++
		}
		if j < len(s) {
			j++
		}
	}
	return j
}
