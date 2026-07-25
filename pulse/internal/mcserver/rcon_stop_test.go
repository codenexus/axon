package mcserver

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startFakeRCONServerForTest mimics just enough of Minecraft's RCON
// handshake to prove gracefulStop takes the RCON path, without needing a
// real Minecraft server — same cheap-stand-in philosophy as the rest of
// this package's tests.
func startFakeRCONServerForTest(t *testing.T, password string, received chan<- string) string {
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
			id, ptype, body, err := readRawRCONPacket(conn)
			if err != nil {
				return
			}
			switch ptype {
			case 3: // SERVERDATA_AUTH
				if body == password {
					authenticated = true
					writeRawRCONPacket(conn, id, 0, "")
					writeRawRCONPacket(conn, id, 2, "")
				} else {
					writeRawRCONPacket(conn, -1, 2, "")
				}
			case 2: // SERVERDATA_EXECCOMMAND
				if !authenticated {
					writeRawRCONPacket(conn, -1, 2, "")
					continue
				}
				received <- body
				writeRawRCONPacket(conn, id, 0, "ok:"+body)
			}
		}
	}()
	return ln.Addr().String()
}

func readRawRCONPacket(conn net.Conn) (id, ptype int32, body string, err error) {
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

func writeRawRCONPacket(conn net.Conn, id, ptype int32, body string) {
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

func TestGracefulStopUsesRCONWhenConfigured(t *testing.T) {
	received := make(chan string, 2)
	addr := startFakeRCONServerForTest(t, "secret", received)
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}

	dir := t.TempDir()
	props := "enable-rcon=true\nrcon.port=" + portStr + "\nrcon.password=secret\n"
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(props), 0o644); err != nil {
		t.Fatalf("write server.properties: %v", err)
	}

	cmd := exec.Command("sh", "-c", "sleep 5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stand-in process: %v", err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-exited
	})

	if err := gracefulStop(dir, cmd.Process.Pid); err != nil {
		t.Fatalf("gracefulStop: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-received:
		case <-time.After(2 * time.Second):
			t.Fatalf("expected 2 RCON commands, only saw %d", i)
		}
	}

	select {
	case <-exited:
		t.Fatal("stand-in process exited — expected RCON path (no fallback signal) to leave it running")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestGracefulStopFallsBackWithoutRCON(t *testing.T) {
	dir := t.TempDir() // no server.properties present

	cmd := exec.Command("sh", "-c", "sleep 5")
	setProcAttrs(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stand-in process: %v", err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()

	if err := gracefulStop(dir, cmd.Process.Pid); err != nil {
		t.Fatalf("gracefulStop: %v", err)
	}

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("expected fallback terminate() to stop the process")
	}
}
