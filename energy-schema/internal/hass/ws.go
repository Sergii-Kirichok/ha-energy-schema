package hass

// Minimal, dependency-free Home Assistant WebSocket client. We only need it for
// one thing the REST API can't give us: long-term statistics (daily energy),
// which survive the short recorder retention. Implements just enough of RFC 6455
// (client-masked text frames, fragmentation, control-frame skip) over stdlib.

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"time"
)

// wsConn wraps a raw connection with a buffered reader for frame parsing.
type wsConn struct {
	c net.Conn
	r *bufio.Reader
}

// wsDial performs the HTTP upgrade handshake and returns a ready wsConn.
func wsDial(apiBase string) (*wsConn, error) {
	u, err := url.Parse(apiBase)
	if err != nil {
		return nil, err
	}
	host := u.Host
	var conn net.Conn
	d := &net.Dialer{Timeout: 10 * time.Second}
	if u.Scheme == "https" {
		if u.Port() == "" {
			host = u.Hostname() + ":443"
		}
		conn, err = tls.DialWithDialer(d, "tcp", host, &tls.Config{ServerName: u.Hostname()})
	} else {
		if u.Port() == "" {
			host = u.Hostname() + ":80"
		}
		conn, err = d.Dial("tcp", host)
	}
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
	keyb := make([]byte, 16)
	_, _ = rand.Read(keyb)
	key := base64.StdEncoding.EncodeToString(keyb)
	req := "GET /api/websocket HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err = conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	// consume the 101 response headers up to the blank line
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	return &wsConn{c: conn, r: br}, nil
}

func (w *wsConn) close() { _ = w.c.Close() }

// writeText sends one masked text frame (our messages fit in a single frame).
func (w *wsConn) writeText(data []byte) error {
	var hdr []byte
	n := len(data)
	switch {
	case n < 126:
		hdr = []byte{0x81, byte(0x80 | n)}
	case n < 65536:
		hdr = []byte{0x81, 0x80 | 126, byte(n >> 8), byte(n)}
	default:
		hdr = make([]byte, 10)
		hdr[0], hdr[1] = 0x81, 0x80|127
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = data[i] ^ mask[i%4]
	}
	if _, err := w.c.Write(append(append(hdr, mask[:]...), masked...)); err != nil {
		return err
	}
	return nil
}

func (w *wsConn) writeJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.writeText(b)
}

// readMessage assembles one full application message, skipping control frames
// (ping/pong) and erroring on close.
func (w *wsConn) readMessage() ([]byte, error) {
	var payload []byte
	for {
		h := make([]byte, 2)
		if _, err := io.ReadFull(w.r, h); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		opcode := h[0] & 0x0f
		masked := h[1]&0x80 != 0
		n := int(h[1] & 0x7f)
		switch n {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(w.r, ext); err != nil {
				return nil, err
			}
			n = int(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(w.r, ext); err != nil {
				return nil, err
			}
			n = int(binary.BigEndian.Uint64(ext))
		}
		var mask [4]byte
		if masked { // servers shouldn't mask, but handle it
			if _, err := io.ReadFull(w.r, mask[:]); err != nil {
				return nil, err
			}
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(w.r, data); err != nil {
			return nil, err
		}
		if masked {
			for i := range data {
				data[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case 0x8: // close
			return nil, fmt.Errorf("ws closed by server")
		case 0x9, 0xA: // ping/pong — ignore
			continue
		default: // 0x0 continuation, 0x1 text, 0x2 binary
			payload = append(payload, data...)
			if fin {
				return payload, nil
			}
		}
	}
}

// DailyProduction returns recent whole-day energy totals (kWh, oldest→newest,
// at most `days` of them) for a cumulative total_increasing energy sensor,
// read from HA long-term statistics. The oldest stats row carries the running
// baseline (change == lifetime sum) and is dropped; HA omits the partial today.
func (c *Client) DailyProduction(entity string, days int) ([]float64, error) {
	w, err := wsDial(c.APIBase)
	if err != nil {
		return nil, err
	}
	defer w.close()
	// handshake: auth_required -> auth -> auth_ok
	if _, err = w.readMessage(); err != nil {
		return nil, err
	}
	if err = w.writeJSON(map[string]string{"type": "auth", "access_token": c.Token}); err != nil {
		return nil, err
	}
	authResp, err := w.readMessage()
	if err != nil {
		return nil, err
	}
	var auth struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(authResp, &auth)
	if auth.Type != "auth_ok" {
		return nil, fmt.Errorf("ws auth failed: %s", auth.Type)
	}
	start := time.Now().AddDate(0, 0, -(days + 2)).UTC().Format("2006-01-02T15:04:05")
	if err = w.writeJSON(map[string]interface{}{
		"id":            1,
		"type":          "recorder/statistics_during_period",
		"start_time":    start,
		"statistic_ids": []string{entity},
		"period":        "day",
		"types":         []string{"change"},
	}); err != nil {
		return nil, err
	}
	// read until our id=1 result arrives
	var resp struct {
		ID      int  `json:"id"`
		Success bool `json:"success"`
		Result  map[string][]struct {
			Start  float64  `json:"start"`
			Change *float64 `json:"change"`
		} `json:"result"`
	}
	for {
		msg, err := w.readMessage()
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(msg, &resp); err == nil && resp.ID == 1 {
			break
		}
	}
	if !resp.Success {
		return nil, fmt.Errorf("statistics request failed for %s", entity)
	}
	rows := resp.Result[entity]
	out := make([]float64, 0, len(rows))
	for i, r := range rows {
		if i == 0 || r.Change == nil { // first row = cumulative baseline
			continue
		}
		v := *r.Change
		if v < 0 || v > 400 { // sanity: ignore baseline/reset artifacts
			continue
		}
		out = append(out, v)
	}
	if len(out) > days {
		out = out[len(out)-days:]
	}
	return out, nil
}
