//go:build windows

// A minimal RFC 6455 websocket client, just enough for the Chrome
// DevTools protocol: the handshake, masked client text frames and
// server text frames with continuation, ping/pong and close handling.
//
// The recorder cannot use golang.org/x/net/websocket because that
// client always sends an Origin header, which Chrome's DevTools
// websocket rejects; non-browser tools (like this one) omit Origin,
// and Chrome accepts them.
package parity

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// wsConn is one DevTools websocket connection.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader

	writeMu sync.Mutex
}

// dialWS dials a ws:// URL and completes the RFC 6455 handshake (no
// Origin header: a non-browser client).
func dialWS(rawURL string) (*wsConn, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "ws" {
		return nil, fmt.Errorf("parity: %s: only ws:// DevTools URLs are supported", rawURL)
	}
	conn, err := net.DialTimeout("tcp", parsed.Host, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dialing %s: %w", parsed.Host, err)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		_ = conn.Close()
		return nil, err
	}
	request := fmt.Sprintf(
		"GET %s HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"\r\n",
		parsed.RequestURI(), parsed.Host, base64.StdEncoding.EncodeToString(nonce))
	if _, err := conn.Write([]byte(request)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket handshake: %w", err)
	}
	if !strings.Contains(status, "101") {
		_ = conn.Close()
		return nil, fmt.Errorf("websocket handshake: %s", strings.TrimSpace(status))
	}
	// Drain the rest of the handshake headers.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("websocket handshake: %w", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return &wsConn{conn: conn, br: br}, nil
}

// send writes one masked text frame with the payload.
func (c *wsConn) send(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	head := []byte{0x81} // FIN + text opcode.
	length := len(payload)
	switch {
	case length < 126:
		head = append(head, 0x80|byte(length))
	case length <= 0xffff:
		head = append(head, 0x80|126, 0, 0)
		binary.BigEndian.PutUint16(head[len(head)-2:], uint16(length))
	default:
		head = append(head, 0x80|127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(head[len(head)-8:], uint64(length))
	}
	key := make([]byte, 4)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	head = append(head, key...)
	masked := make([]byte, length)
	for i, b := range payload {
		masked[i] = b ^ key[i%4]
	}
	if _, err := c.conn.Write(append(head, masked...)); err != nil {
		return err
	}
	return nil
}

// receive reads the next complete text message, transparently
// answering pings and skipping pongs.
func (c *wsConn) receive() ([]byte, error) {
	var message []byte
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		var head [2]byte
		if _, err := io.ReadFull(c.br, head[:]); err != nil {
			return nil, err
		}
		fin := head[0]&0x80 != 0
		opcode := head[0] & 0x0f
		masked := head[1]&0x80 != 0
		length := uint64(head[1] & 0x7f)
		switch length {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(ext[:])
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return nil, err
		}
		if masked {
			var key [4]byte
			if _, err := io.ReadFull(c.br, key[:]); err != nil {
				return nil, err
			}
			for i, b := range payload {
				payload[i] = b ^ key[i%4]
			}
		}
		switch opcode {
		case 0x1, 0x2: // text, binary
			message = append(message[:0], payload...)
			if fin {
				return message, nil
			}
		case 0x0: // continuation
			message = append(message, payload...)
			if fin {
				return message, nil
			}
		case 0x8: // close
			return nil, io.EOF
		case 0x9: // ping: answer with a pong of the same payload.
			if err := c.sendFrame(0x8a, payload); err != nil {
				return nil, err
			}
		case 0xa: // pong
		default:
			return nil, fmt.Errorf("parity: unexpected websocket opcode %#x", opcode)
		}
	}
}

// sendFrame writes one masked control frame (the pong path).
func (c *wsConn) sendFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	key := make([]byte, 4)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	head := []byte{opcode, 0x80 | byte(len(payload))}
	head = append(head, key...)
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ key[i%4]
	}
	_, err := c.conn.Write(append(head, masked...))
	return err
}

// close closes the connection.
func (c *wsConn) close() error { return c.conn.Close() }
