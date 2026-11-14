// Package control implementa el socket Unix por el que el panel y la ventana
// RGB le envían comandos al daemon (una línea JSON por comando).
package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"device-hub/internal/led"
)

// manager es la porción de led.Manager que necesita el servidor.
type manager interface {
	Apply(deviceID string, color [3]uint8) error
	Sync(color [3]uint8) error
	SetEnabled(deviceID string, enabled bool) error
	SetController(deviceID string, controller led.Controller) error
}

type command struct {
	Action     string `json:"action"` // set | sync | on | off | controller | window | openrgb | profile
	Device     string `json:"device"`
	Color      string `json:"color"`
	Controller string `json:"controller"`
	Profile    string `json:"profile"`
}

type reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Server accepts newline-delimited JSON commands over a Unix socket.
// The panel writes one command per line and reads one reply per line.
type Server struct {
	m         manager
	onWindow  func()
	onOpenRGB func()
	onProfile func(name string) error
	ln        net.Listener
	path      string
}

// NewServer listens on path. The callbacks may be nil.
func NewServer(path string, m manager, onWindow, onOpenRGB func(), onProfile func(name string) error) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(path) // stale socket from a previous run
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return &Server{m: m, onWindow: onWindow, onOpenRGB: onOpenRGB, onProfile: onProfile, ln: ln, path: path}, nil
}

// Run accepts connections until Close is called.
func (s *Server) Run() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) Close() error {
	if s.ln == nil {
		return nil
	}
	return s.ln.Close()
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	encoder := json.NewEncoder(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := encoder.Encode(s.execute(line)); err != nil {
			return
		}
	}
}

func (s *Server) execute(line string) reply {
	var cmd command
	if err := json.Unmarshal([]byte(line), &cmd); err != nil {
		return reply{Error: "comando JSON inválido"}
	}
	switch cmd.Action {
	case "set", "sync":
		color, err := parseHexColor(cmd.Color)
		if err != nil {
			return reply{Error: err.Error()}
		}
		if cmd.Action == "sync" {
			if err := s.m.Sync(color); err != nil {
				return reply{Error: err.Error()}
			}
			return reply{OK: true}
		}
		if cmd.Device == "" {
			return reply{Error: "falta device"}
		}
		if err := s.m.Apply(cmd.Device, color); err != nil {
			return reply{Error: err.Error()}
		}
	case "on", "off":
		if cmd.Device == "" {
			return reply{Error: "falta device"}
		}
		if err := s.m.SetEnabled(cmd.Device, cmd.Action == "on"); err != nil {
			return reply{Error: err.Error()}
		}
	case "controller":
		if cmd.Device == "" {
			return reply{Error: "falta device"}
		}
		if cmd.Controller == "" {
			return reply{Error: "falta controller"}
		}
		if err := s.m.SetController(cmd.Device, led.Controller(cmd.Controller)); err != nil {
			return reply{Error: err.Error()}
		}
	case "window":
		if s.onWindow != nil {
			go s.onWindow()
		}
	case "openrgb":
		if s.onOpenRGB != nil {
			go s.onOpenRGB()
		}
	case "profile":
		if cmd.Profile == "" {
			return reply{Error: "falta profile"}
		}
		if s.onProfile == nil {
			return reply{Error: "profiles no disponibles"}
		}
		if err := s.onProfile(cmd.Profile); err != nil {
			return reply{Error: err.Error()}
		}
	default:
		return reply{Error: "acción desconocida: " + cmd.Action}
	}
	return reply{OK: true}
}

func parseHexColor(s string) ([3]uint8, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return [3]uint8{}, fmt.Errorf("color inválido %q", s)
	}
	var color [3]uint8
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return [3]uint8{}, fmt.Errorf("color inválido %q", s)
		}
		color[i] = uint8(v)
	}
	return color, nil
}
