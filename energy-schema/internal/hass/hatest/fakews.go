// Package hatest — фейковый Home Assistant WebSocket-сервер для тестов
// (только stdlib). Делает RFC 6455 upgrade на GET /api/websocket, auth-рукопожатие
// и отвечает на каждый запрос через пользовательский handle.
package hatest

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Token — единственный принимаемый access_token.
const Token = "T"

// Handler получает декодированный запрос клиента и возвращает результат
// (success:true) либо ok=false с текстом ошибки.
type Handler func(req map[string]any) (result any, ok bool, errMsg string)

// Opts — переключатели, гоняющие парсер клиента по редким веткам.
type Opts struct {
	Ping     bool // перед каждым ответом слать ping-кадр
	Fragment bool // ответ двумя кадрами: text FIN=0 + continuation FIN=1
	Before   any  // лишнее сообщение (например с чужим id) перед каждым ответом
}

// NewServer — сервер с настройками по умолчанию; закрывается через t.Cleanup.
func NewServer(t testing.TB, handle Handler) *httptest.Server {
	return NewServerOpts(t, handle, Opts{})
}

// NewServerOpts — то же с Opts. APIBase для hass.NewClient: srv.URL+"/api".
func NewServerOpts(t testing.TB, handle Handler, o Opts) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/websocket" || r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "not a websocket upgrade", http.StatusBadRequest)
			return
		}
		h := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n"))
		o.serve(conn, handle)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (o Opts) serve(conn net.Conn, handle Handler) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return writeFrame(conn, true, 0x1, b)
	}
	if send(map[string]any{"type": "auth_required"}) != nil {
		return
	}
	raw, err := readMessage(r)
	if err != nil {
		return
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(raw, &auth)
	if auth.AccessToken != Token {
		_ = send(map[string]any{"type": "auth_invalid", "message": "Invalid access token"})
		return
	}
	if send(map[string]any{"type": "auth_ok"}) != nil {
		return
	}
	for {
		raw, err := readMessage(r)
		if err != nil {
			return
		}
		var req map[string]any
		if json.Unmarshal(raw, &req) != nil {
			return
		}
		result, ok, msg := handle(req)
		resp := map[string]any{"id": req["id"], "type": "result", "success": ok}
		if ok {
			resp["result"] = result
		} else {
			resp["error"] = map[string]any{"code": "unknown_error", "message": msg}
		}
		if o.Before != nil && send(o.Before) != nil {
			return
		}
		if o.Ping && writeFrame(conn, true, 0x9, []byte("ping")) != nil {
			return
		}
		b, err := json.Marshal(resp)
		if err != nil {
			return
		}
		if o.Fragment && len(b) > 1 {
			half := len(b) / 2
			if writeFrame(conn, false, 0x1, b[:half]) != nil || writeFrame(conn, true, 0x0, b[half:]) != nil {
				return
			}
			continue
		}
		if writeFrame(conn, true, 0x1, b) != nil {
			return
		}
	}
}

// writeFrame шлёт один немаскированный кадр (сервер → клиент).
func writeFrame(w io.Writer, fin bool, opcode byte, p []byte) error {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	hdr := []byte{b0}
	n := len(p)
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 65536:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		hdr = append(append(hdr, 127), ext[:]...)
	}
	_, err := w.Write(append(hdr, p...))
	return err
}

// readMessage собирает одно сообщение клиента (кадры маскированы), пропуская
// ping/pong; close-кадр → io.EOF.
func readMessage(r *bufio.Reader) ([]byte, error) {
	var payload []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		opcode := h[0] & 0x0f
		n := int(h[1] & 0x7f)
		switch n {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(r, ext[:]); err != nil {
				return nil, err
			}
			n = int(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(r, ext[:]); err != nil {
				return nil, err
			}
			n = int(binary.BigEndian.Uint64(ext[:]))
		}
		var mask [4]byte
		masked := h[1]&0x80 != 0
		if masked {
			if _, err := io.ReadFull(r, mask[:]); err != nil {
				return nil, err
			}
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}
		if masked {
			for i := range data {
				data[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case 0x8:
			return nil, io.EOF
		case 0x9, 0xA:
			continue
		default:
			payload = append(payload, data...)
			if fin {
				return payload, nil
			}
		}
	}
}
