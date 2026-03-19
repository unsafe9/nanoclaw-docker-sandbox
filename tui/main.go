package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	sandbox := flag.String("sandbox", "", "Docker sandbox name (tunnels via docker sandbox exec)")
	port := flag.Int("port", 3333, "TUI WebSocket port inside the sandbox")
	url := flag.String("url", "", "Direct ws:// URL (alternative to --sandbox)")
	flag.Parse()

	var wsClient *WSClient
	if *sandbox != "" {
		wsClient = NewWSClient("", *sandbox, *port)
	} else if *url != "" {
		wsClient = NewWSClient(*url, "", 0)
	} else if name := detectSandbox(); name != "" {
		wsClient = NewWSClient("", name, *port)
	} else {
		wsClient = NewWSClient("ws://localhost:3333", "", 0)
	}

	m := initialModel(wsClient)

	// Start the read loop in the background once connected
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if wsClient.IsConnected() {
				wsClient.ReadLoop(ctx)
			}
			// Sleep before checking again to avoid hot loop
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}()

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// detectSandbox finds the first running nanoclaw sandbox from "docker sandbox ls".
func detectSandbox() string {
	out, err := exec.Command("docker", "sandbox", "ls").CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range bytes.Split(out, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) < 3 {
			continue
		}
		name, status := fields[0], fields[2]
		if strings.Contains(name, "nanoclaw") && status == "running" {
			return name
		}
	}
	return ""
}
