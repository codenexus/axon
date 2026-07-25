package rcon

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// startFakeRCONServer mimics just enough of Minecraft's RCON handshake to
// exercise Client without needing a real Minecraft server — same
// cheap-stand-in philosophy as mcserver's process tests.
func startFakeRCONServer(t *testing.T, password string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		authenticated := false
		for {
			id, ptype, body, err := readTestPacket(conn)
			if err != nil {
				return
			}
			switch ptype {
			case packetTypeAuth:
				if body == password {
					authenticated = true
					writeTestPacket(conn, id, packetTypeResponseValue, "")
					writeTestPacket(conn, id, 2, "")
				} else {
					writeTestPacket(conn, -1, 2, "")
				}
			case packetTypeExecCommand:
				if !authenticated {
					writeTestPacket(conn, -1, 2, "")
					continue
				}
				writeTestPacket(conn, id, packetTypeResponseValue, "ok:"+body)
			}
		}
	}()
	return ln.Addr().String()
}

func readTestPacket(conn net.Conn) (id, ptype int32, body string, err error) {
	var length int32
	if err = binary.Read(conn, binary.LittleEndian, &length); err != nil {
		return 0, 0, "", err
	}
	payload := make([]byte, length)
	if _, err = io.ReadFull(conn, payload); err != nil {
		return 0, 0, "", err
	}
	id = int32(binary.LittleEndian.Uint32(payload[0:4]))
	ptype = int32(binary.LittleEndian.Uint32(payload[4:8]))
	body = strings.TrimRight(string(payload[8:length-2]), "\x00")
	return id, ptype, body, nil
}

func writeTestPacket(conn net.Conn, id, ptype int32, body string) {
	buf := make([]byte, 0, 10+len(body))
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(id))
	buf = append(buf, tmp...)
	binary.LittleEndian.PutUint32(tmp, uint32(ptype))
	buf = append(buf, tmp...)
	buf = append(buf, body...)
	buf = append(buf, 0, 0)

	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(buf)))
	conn.Write(lenBuf)
	conn.Write(buf)
}

func dialTestClient(t *testing.T, addr string) *Client {
	t.Helper()
	client, err := Dial(addr, 2*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	client.SetDeadline(time.Now().Add(2 * time.Second))
	return client
}

func TestAuthenticateSuccess(t *testing.T) {
	addr := startFakeRCONServer(t, "secret")
	client := dialTestClient(t, addr)
	if err := client.Authenticate("secret"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAuthenticateFailure(t *testing.T) {
	addr := startFakeRCONServer(t, "secret")
	client := dialTestClient(t, addr)
	if err := client.Authenticate("wrong"); err == nil {
		t.Fatal("expected authentication error")
	}
}

func TestExecute(t *testing.T) {
	addr := startFakeRCONServer(t, "secret")
	client := dialTestClient(t, addr)
	if err := client.Authenticate("secret"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	resp, err := client.Execute("save-all")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp != "ok:save-all" {
		t.Fatalf("unexpected response: %q", resp)
	}
}
