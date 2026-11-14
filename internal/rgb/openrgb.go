// Package rgb controls the lighting of the registered RGB devices.
//
// Razer devices are driven through the OpenRGB SDK server (binary "ORGB"
// protocol over TCP, default 127.0.0.1:6742) because openrazer exposes only
// battery data. The Fantech keyboard is driven directly over its 2.4 GHz
// receiver because OpenRGB does not detect it.
package rgb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// OpenRGB SDK packet IDs (see Documentation/OpenRGBSDK.md).
const (
	pktRequestControllerCount = 0
	pktRequestControllerData  = 1
	pktAck                    = 10
	pktRequestProtocolVersion = 40
	pktSetClientName          = 50
	pktDeviceListUpdated      = 100
	pktUpdateLEDs             = 1050
	pktUpdateZoneLEDs         = 1051
	pktSetCustomMode          = 1100
	pktUpdateMode             = 1101
	pktGetActiveProfile       = 156

	headerSize     = 16
	clientName     = "device-hub"
	maxProtocol    = 4 // we parse up to protocol 4 (brightness fields + segments)
	dialTimeout    = 3 * time.Second
	requestTimeout = 3 * time.Second
)

// Client is a connection to an OpenRGB SDK server.
type Client struct {
	conn    net.Conn
	version uint32
}

// Controller is one RGBController reported by the server.
type Controller struct {
	Index      uint32
	Name       string
	NumLEDs    int
	Zones      []Zone
	Modes      []Mode
	Colors     [][3]uint8
	ActiveMode int
}

// Mode is one lighting mode of a controller.
type Mode struct {
	Index         int
	Name          string
	Value         int32
	Flags         uint32
	SpeedMin      uint32
	SpeedMax      uint32
	BrightnessMin uint32
	BrightnessMax uint32
	ColorsMin     uint32
	ColorsMax     uint32
	Speed         uint32
	Brightness    uint32
	Direction     uint32
	ColorMode     uint32
	Colors        [][3]uint8
}

// Zone is a separately addressable LED region of a controller.
type Zone struct {
	Name    string
	NumLEDs int
}

// Dial connects to the OpenRGB SDK server at addr and negotiates the protocol.
func Dial(addr string) (*Client, error) { return DialMax(addr, maxProtocol) }

// DialMax connects and negotiates up to max. Use 6 for profile queries: the
// controller data layout above protocol 4 is not parsed yet.
func DialMax(addr string, max uint32) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("openrgb: connect %s: %w", addr, err)
	}
	c := &Client{conn: conn}

	// Negotiate protocol version. A version-0 server sends no reply.
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	if err := c.writePacket(0, pktRequestProtocolVersion, u32(max)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("openrgb: protocol version: %w", err)
	}
	data, err := c.readUntil(pktRequestProtocolVersion)
	switch {
	case err == nil && len(data) >= 4:
		c.version = min(max, binary.LittleEndian.Uint32(data))
	case err != nil:
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			c.version = 0
		} else {
			conn.Close()
			return nil, fmt.Errorf("openrgb: protocol version: %w", err)
		}
	default:
		c.version = 0
	}
	_ = conn.SetDeadline(time.Time{})

	// Name the client so it shows up in the OpenRGB GUI.
	if err := c.writePacket(0, pktSetClientName, append([]byte(clientName), 0)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("openrgb: client name: %w", err)
	}
	return c, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Controllers requests every controller description from the server.
func (c *Client) Controllers() ([]Controller, error) {
	_ = c.conn.SetDeadline(time.Now().Add(requestTimeout))
	defer c.conn.SetDeadline(time.Time{})

	if err := c.writePacket(0, pktRequestControllerCount, nil); err != nil {
		return nil, fmt.Errorf("openrgb: controller count: %w", err)
	}
	data, err := c.readUntil(pktRequestControllerCount)
	if err != nil {
		return nil, fmt.Errorf("openrgb: controller count: %w", err)
	}
	if len(data) < 4 {
		return nil, errors.New("openrgb: short controller count reply")
	}
	count := binary.LittleEndian.Uint32(data)
	if count > 1024 {
		return nil, fmt.Errorf("openrgb: implausible controller count %d", count)
	}

	out := make([]Controller, 0, count)
	for i := uint32(0); i < count; i++ {
		var req []byte
		if c.version >= 1 {
			req = u32(c.version)
		}
		if err := c.writePacket(i, pktRequestControllerData, req); err != nil {
			return nil, fmt.Errorf("openrgb: controller %d: %w", i, err)
		}
		raw, err := c.readUntil(pktRequestControllerData)
		if err != nil {
			return nil, fmt.Errorf("openrgb: controller %d: %w", i, err)
		}
		ctrl, err := parseController(raw, c.version)
		if err != nil {
			return nil, fmt.Errorf("openrgb: parse controller %d: %w", i, err)
		}
		ctrl.Index = i
		out = append(out, ctrl)
	}
	return out, nil
}

// SetCustomMode switches a controller to its most granular (Direct) mode.
func (c *Client) SetCustomMode(devID uint32) error {
	return c.writePacket(devID, pktSetCustomMode, nil)
}

// UpdateMode applies one mode (with its colours) to a controller. It is the
// only way the SDK exposes to latch a non-Direct mode, such as the Static mode
// the Leviathan firmware needs.
func (c *Client) UpdateMode(devID uint32, mode Mode) error {
	body := encodeMode(mode, c.version)
	data := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(data[0:], uint32(8+len(body)))
	binary.LittleEndian.PutUint32(data[4:], uint32(mode.Index))
	copy(data[8:], body)
	return c.writePacket(devID, pktUpdateMode, data)
}

// ActiveProfile returns the profile the server reports as active, or "" when
// none is active. It requires protocol 6, so dial with DialMax(addr, 6).
func (c *Client) ActiveProfile() (string, error) {
	_ = c.conn.SetDeadline(time.Now().Add(requestTimeout))
	defer c.conn.SetDeadline(time.Time{})

	if err := c.writePacket(0, pktGetActiveProfile, nil); err != nil {
		return "", fmt.Errorf("openrgb: active profile: %w", err)
	}
	data, err := c.readUntil(pktGetActiveProfile)
	if err != nil {
		return "", fmt.Errorf("openrgb: active profile: %w", err)
	}
	return strings.TrimRight(string(data), "\x00"), nil
}

// UpdateLEDs pushes one color per LED for the whole controller.
func (c *Client) UpdateLEDs(devID uint32, colors [][3]uint8) error {
	inner := make([]byte, 2+4*len(colors))
	binary.LittleEndian.PutUint16(inner, uint16(len(colors)))
	putColors(inner[2:], colors)
	return c.writeColors(devID, pktUpdateLEDs, inner)
}

// UpdateZoneLEDs pushes one color per LED for a single zone.
func (c *Client) UpdateZoneLEDs(devID, zone uint32, colors [][3]uint8) error {
	inner := make([]byte, 4+2+4*len(colors))
	binary.LittleEndian.PutUint32(inner, zone)
	binary.LittleEndian.PutUint16(inner[4:], uint16(len(colors)))
	putColors(inner[6:], colors)
	return c.writeColors(devID, pktUpdateZoneLEDs, inner)
}

// writeColors prepends the inner data_size field required by the color packets.
func (c *Client) writeColors(devID, pktID uint32, inner []byte) error {
	data := make([]byte, 4+len(inner))
	binary.LittleEndian.PutUint32(data, uint32(4+len(inner)))
	copy(data[4:], inner)
	return c.writePacket(devID, pktID, data)
}

func putColors(dst []byte, colors [][3]uint8) {
	for i, col := range colors {
		off := 4 * i
		dst[off] = col[0]
		dst[off+1] = col[1]
		dst[off+2] = col[2]
		dst[off+3] = 0
	}
}

func (c *Client) writePacket(devID, pktID uint32, data []byte) error {
	buf := make([]byte, headerSize+len(data))
	copy(buf[0:4], "ORGB")
	binary.LittleEndian.PutUint32(buf[4:], devID)
	binary.LittleEndian.PutUint32(buf[8:], pktID)
	binary.LittleEndian.PutUint32(buf[12:], uint32(len(data)))
	copy(buf[headerSize:], data)
	_, err := c.conn.Write(buf)
	return err
}

// readUntil reads packets until one with the wanted ID arrives, skipping
// unrelated unsolicited packets (ACK, device-list updates).
func (c *Client) readUntil(want uint32) ([]byte, error) {
	for {
		_, id, data, err := c.readPacket()
		if err != nil {
			return nil, err
		}
		if id == want {
			return data, nil
		}
	}
}

func (c *Client) readPacket() (devID, pktID uint32, data []byte, err error) {
	header := make([]byte, headerSize)
	if _, err = io.ReadFull(c.conn, header); err != nil {
		return 0, 0, nil, err
	}
	if string(header[0:4]) != "ORGB" {
		return 0, 0, nil, fmt.Errorf("openrgb: bad magic %q", header[0:4])
	}
	devID = binary.LittleEndian.Uint32(header[4:])
	pktID = binary.LittleEndian.Uint32(header[8:])
	size := binary.LittleEndian.Uint32(header[12:])
	if size > 0 {
		data = make([]byte, size)
		if _, err = io.ReadFull(c.conn, data); err != nil {
			return 0, 0, nil, err
		}
	}
	return devID, pktID, data, nil
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// parser reads little-endian fields out of a controller data block, recording
// the first truncation error instead of panicking.
type parser struct {
	buf []byte
	pos int
	err error
}

func (p *parser) need(n int) bool {
	if p.err != nil {
		return false
	}
	if p.pos+n > len(p.buf) {
		p.err = io.ErrUnexpectedEOF
		return false
	}
	return true
}

func (p *parser) u16() uint16 {
	if !p.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(p.buf[p.pos:])
	p.pos += 2
	return v
}

func (p *parser) u32() uint32 {
	if !p.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(p.buf[p.pos:])
	p.pos += 4
	return v
}

func (p *parser) i32() int32 { return int32(p.u32()) }

func (p *parser) skip(n int) {
	if n <= 0 || !p.need(n) {
		return
	}
	p.pos += n
}

func (p *parser) str() string {
	n := int(p.u16())
	if n <= 0 || !p.need(n) {
		return ""
	}
	s := string(p.buf[p.pos : p.pos+n])
	p.pos += n
	return strings.TrimRight(s, "\x00")
}

func parseController(raw []byte, version uint32) (Controller, error) {
	p := &parser{buf: raw}
	_ = p.u32() // data_size
	_ = p.i32() // device type
	ctrl := Controller{Name: p.str()}

	// Metadata: vendor only exists from protocol 1.
	if version >= 1 {
		_ = p.str()
	}
	_ = p.str() // description
	_ = p.str() // version
	_ = p.str() // serial
	_ = p.str() // location

	numModes := int(p.u16())
	ctrl.ActiveMode = int(p.i32())
	ctrl.Modes = make([]Mode, 0, numModes)
	for i := 0; i < numModes; i++ {
		mode := parseMode(p, version)
		mode.Index = i
		ctrl.Modes = append(ctrl.Modes, mode)
	}

	numZones := int(p.u16())
	for i := 0; i < numZones; i++ {
		ctrl.Zones = append(ctrl.Zones, parseZone(p, version))
	}

	numLEDs := int(p.u16())
	for i := 0; i < numLEDs; i++ {
		_ = p.str() // LED name
		if version < 6 {
			_ = p.u32() // LED value
		}
	}
	ctrl.NumLEDs = numLEDs

	numColors := int(p.u16())
	for i := 0; i < numColors; i++ {
		if !p.need(4) {
			break
		}
		ctrl.Colors = append(ctrl.Colors, [3]uint8{p.buf[p.pos], p.buf[p.pos+1], p.buf[p.pos+2]})
		p.pos += 4
	}

	if p.err != nil {
		return Controller{}, p.err
	}
	return ctrl, nil
}

func parseMode(p *parser, version uint32) Mode {
	m := Mode{Name: p.str()}
	if version < 6 {
		m.Value = p.i32()
	}
	m.Flags = p.u32()
	m.SpeedMin = p.u32()
	m.SpeedMax = p.u32()
	if version >= 3 {
		m.BrightnessMin = p.u32()
		m.BrightnessMax = p.u32()
	}
	m.ColorsMin = p.u32()
	m.ColorsMax = p.u32()
	m.Speed = p.u32()
	if version >= 3 {
		m.Brightness = p.u32()
	}
	m.Direction = p.u32()
	m.ColorMode = p.u32()
	numColors := int(p.u16())
	for i := 0; i < numColors; i++ {
		if !p.need(4) {
			break
		}
		m.Colors = append(m.Colors, [3]uint8{p.buf[p.pos], p.buf[p.pos+1], p.buf[p.pos+2]})
		p.pos += 4
	}
	return m
}

// encodeMode serializes a Mode for UpdateMode/SaveMode.
func encodeMode(m Mode, version uint32) []byte {
	var b bytes.Buffer
	putStr(&b, m.Name)
	if version < 6 {
		putU32(&b, uint32(m.Value))
	}
	putU32(&b, m.Flags)
	putU32(&b, m.SpeedMin)
	putU32(&b, m.SpeedMax)
	if version >= 3 {
		putU32(&b, m.BrightnessMin)
		putU32(&b, m.BrightnessMax)
	}
	putU32(&b, m.ColorsMin)
	putU32(&b, m.ColorsMax)
	putU32(&b, m.Speed)
	if version >= 3 {
		putU32(&b, m.Brightness)
	}
	putU32(&b, m.Direction)
	putU32(&b, m.ColorMode)
	putU16(&b, uint16(len(m.Colors)))
	for _, col := range m.Colors {
		b.Write([]byte{col[0], col[1], col[2], 0})
	}
	return b.Bytes()
}

func putStr(b *bytes.Buffer, s string) {
	putU16(b, uint16(len(s)+1))
	b.WriteString(s)
	b.WriteByte(0)
}

func putU16(b *bytes.Buffer, v uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], v)
	b.Write(buf[:])
}

func putU32(b *bytes.Buffer, v uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	b.Write(buf[:])
}

func parseZone(p *parser, version uint32) Zone {
	z := Zone{Name: p.str()}
	_ = p.i32() // zone type
	_ = p.u32() // leds_min
	_ = p.u32() // leds_max
	z.NumLEDs = int(p.u32())

	matrixSize := int(p.u16())
	if matrixSize > 0 {
		_ = p.u32() // height
		_ = p.u32() // width
		p.skip(matrixSize * 4)
	}

	if version >= 4 {
		numSegments := int(p.u16())
		for i := 0; i < numSegments; i++ {
			_ = p.str() // segment name
			_ = p.i32() // segment type
			_ = p.u32() // start index
			_ = p.u32() // LED count
		}
	}
	return z
}
