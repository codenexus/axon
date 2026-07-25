// Package rcon is a minimal Source RCON protocol client — just enough to
// authenticate and run a handful of admin commands against a locally
// running Minecraft server. See
// https://developer.valvesoftware.com/wiki/Source_RCON_Protocol.
package rcon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	packetTypeResponseValue = int32(0)
	packetTypeExecCommand   = int32(2)
	packetTypeAuth          = int32(3)
)

// maxPacketSize bounds a single response; generous for save-all/stop/whitelist
// replies without letting a misbehaving peer force an unbounded allocation.
const maxPacketSize = 1 << 13

type Client struct {
	conn   net.Conn
	nextID int32
}

// Dial connects to a Source RCON server at addr ("host:port").
func Dial(addr string, timeout time.Duration) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("rcon: dial: %w", err)
	}
	return &Client{conn: conn, nextID: 1}, nil
}

// SetDeadline applies a shared read/write deadline to the underlying connection.
func (c *Client) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// Authenticate performs the RCON login handshake. Minecraft's RCON
// implementation sends an empty SERVERDATA_RESPONSE_VALUE packet before the
// real SERVERDATA_AUTH_RESPONSE on success; skip it if present.
func (c *Client) Authenticate(password string) error {
	id := c.nextID
	c.nextID++
	if err := c.writePacket(id, packetTypeAuth, password); err != nil {
		return err
	}
	respID, respType, _, err := c.readPacket()
	if err != nil {
		return err
	}
	if respType == packetTypeResponseValue {
		respID, _, _, err = c.readPacket()
		if err != nil {
			return err
		}
	}
	if respID != id {
		return errors.New("rcon: authentication failed")
	}
	return nil
}

// Execute runs a single console command and returns its text response.
func (c *Client) Execute(command string) (string, error) {
	id := c.nextID
	c.nextID++
	if err := c.writePacket(id, packetTypeExecCommand, command); err != nil {
		return "", err
	}
	_, _, body, err := c.readPacket()
	return body, err
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) writePacket(id, ptype int32, body string) error {
	var payload bytes.Buffer
	binary.Write(&payload, binary.LittleEndian, id)
	binary.Write(&payload, binary.LittleEndian, ptype)
	payload.WriteString(body)
	payload.Write([]byte{0, 0})

	var packet bytes.Buffer
	binary.Write(&packet, binary.LittleEndian, int32(payload.Len()))
	packet.Write(payload.Bytes())

	_, err := c.conn.Write(packet.Bytes())
	return err
}

func (c *Client) readPacket() (id, ptype int32, body string, err error) {
	var length int32
	if err = binary.Read(c.conn, binary.LittleEndian, &length); err != nil {
		return 0, 0, "", fmt.Errorf("rcon: read length: %w", err)
	}
	if length < 10 || length > maxPacketSize {
		return 0, 0, "", fmt.Errorf("rcon: implausible packet length %d", length)
	}
	payload := make([]byte, length)
	if _, err = io.ReadFull(c.conn, payload); err != nil {
		return 0, 0, "", fmt.Errorf("rcon: read payload: %w", err)
	}
	id = int32(binary.LittleEndian.Uint32(payload[0:4]))
	ptype = int32(binary.LittleEndian.Uint32(payload[4:8]))
	body = string(bytes.TrimRight(payload[8:length-2], "\x00"))
	return id, ptype, body, nil
}
