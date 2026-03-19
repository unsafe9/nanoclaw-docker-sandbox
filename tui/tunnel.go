package main

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"time"
)

// sandboxConn wraps a "docker sandbox exec" process as a net.Conn.
// Stdin/stdout of the child process bridge TCP inside the sandbox.
type sandboxConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

// dialSandbox spawns a Node.js TCP bridge inside the sandbox via
// "docker sandbox exec -i". Node is already present (NanoClaw runs on it).
// Returns a net.Conn that tunnels to the given port inside the sandbox.
func dialSandbox(sandbox string, port int) (net.Conn, error) {
	script := fmt.Sprintf(
		`const s=require("net").connect(%d,"127.0.0.1",()=>{process.stdin.pipe(s);s.pipe(process.stdout)});s.on("error",()=>process.exit(1))`,
		port,
	)

	cmd := exec.Command("docker", "sandbox", "exec", "-i", sandbox, "node", "-e", script)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("start tunnel: %w", err)
	}

	// Give Node a moment to connect to the TCP port
	time.Sleep(300 * time.Millisecond)

	return &sandboxConn{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

func (c *sandboxConn) Read(b []byte) (int, error)  { return c.stdout.Read(b) }
func (c *sandboxConn) Write(b []byte) (int, error)  { return c.stdin.Write(b) }

func (c *sandboxConn) Close() error {
	c.stdin.Close()
	c.stdout.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	c.cmd.Wait()
	return nil
}

// net.Conn interface stubs — not used by gorilla/websocket but required.
func (c *sandboxConn) LocalAddr() net.Addr                { return stubAddr{} }
func (c *sandboxConn) RemoteAddr() net.Addr               { return stubAddr{} }
func (c *sandboxConn) SetDeadline(t time.Time) error      { return nil }
func (c *sandboxConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *sandboxConn) SetWriteDeadline(t time.Time) error { return nil }

type stubAddr struct{}

func (stubAddr) Network() string { return "sandbox" }
func (stubAddr) String() string  { return "sandbox" }
