package rgb

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// colorSetter is the slice of Manager the control server needs.
type colorSetter interface {
	Set(deviceID string, color [3]uint8) error
	Sync(color [3]uint8) error
}

type controlCommand struct {
	Action string `json:"action"` // "set" | "sync"
	Device string `json:"device"`
	Color  string `json:"color"`
}

type controlReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Server accepts newline-delimited JSON RGB commands over a Unix socket.
// The panel writes one command per line and reads one reply per line.
type Server struct {
	setter colorSetter
	ln     net.Listener
	path   string
}

func NewServer(path string, setter colorSetter) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(path) // stale socket from a previous run
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return &Server{setter: setter, ln: ln, path: path}, nil
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

func (s *Server) execute(line string) controlReply {
	var cmd controlCommand
	if err := json.Unmarshal([]byte(line), &cmd); err != nil {
		return controlReply{Error: "comando JSON inválido"}
	}
	color, err := parseHexColor(cmd.Color)
	if err != nil {
		return controlReply{Error: err.Error()}
	}
	switch cmd.Action {
	case "set":
		if cmd.Device == "" {
			return controlReply{Error: "falta device"}
		}
		if err := s.setter.Set(cmd.Device, color); err != nil {
			return controlReply{Error: err.Error()}
		}
	case "sync":
		if err := s.setter.Sync(color); err != nil {
			return controlReply{Error: err.Error()}
		}
	default:
		return controlReply{Error: "acción desconocida: " + cmd.Action}
	}
	return controlReply{OK: true}
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
