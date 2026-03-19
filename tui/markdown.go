package main

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

var renderer *glamour.TermRenderer

func uintPtr(u uint) *uint { return &u }

func initMarkdownRenderer(width int) {
	contentWidth := width
	if contentWidth < 20 {
		contentWidth = 20
	}
	// Use dark style with zero document margin so we don't need to strip it later.
	style := styles.DarkStyleConfig
	style.Document.Margin = uintPtr(0)
	style.CodeBlock.StyleBlock.Margin = uintPtr(0)
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(contentWidth),
	)
	if err != nil {
		renderer = nil
		return
	}
	renderer = r
}

// normalizeMarkdown converts common non-standard patterns into proper markdown
// before rendering. For example, lines starting with "• " (literal bullet)
// are converted to "- " so glamour treats them as list items instead of
// merging them into a single paragraph.
func normalizeMarkdown(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "• ") {
			indent := line[:len(line)-len(trimmed)]
			lines[i] = indent + "- " + trimmed[len("• "):]
		}
	}
	return strings.Join(lines, "\n")
}

// renderMarkdown renders markdown content for terminal display.
// Falls back to plain text if rendering fails.
func renderMarkdown(content string) string {
	if renderer == nil {
		return content
	}
	rendered, err := renderer.Render(normalizeMarkdown(content))
	if err != nil {
		return content
	}
	rendered = strings.TrimSpace(rendered)
	// Strip glamour's trailing ANSI-styled space padding from each line.
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = trimTrailingStyledSpaces(line)
	}
	return strings.Join(lines, "\n")
}

// trimTrailingStyledSpaces removes trailing spaces and ANSI escape sequences
// from a line. Glamour pads lines with styled spaces like \x1b[38;5;252m \x1b[0m.
func trimTrailingStyledSpaces(s string) string {
	end := len(s)
	for end > 0 {
		if s[end-1] == ' ' {
			end--
		} else if end >= 4 && s[end-4:end] == "\x1b[0m" {
			end -= 4
		} else if s[end-1] == 'm' {
			// Other ANSI sequence ending with 'm' — find its \x1b start
			j := end - 2
			for j >= 0 && s[j] != '\x1b' {
				j--
			}
			if j >= 0 && s[j] == '\x1b' {
				end = j
			} else {
				break
			}
		} else {
			break
		}
	}
	return s[:end]
}
