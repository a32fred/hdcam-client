package live

import (
	"context"
	"encoding/binary"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const maxVideoFrame = 4 << 20
const maxVideoInFlight = 4

type videoPending struct {
	id   uint64
	sent time.Time
}
type videoAck struct {
	id      uint64
	invalid bool
}

func (h *Hub) streamWebSocket(w http.ResponseWriter, r *http.Request) {
	select {
	case h.viewers <- struct{}{}:
		defer func() { <-h.viewers }()
	default:
		http.Error(w, "viewer limit reached", http.StatusServiceUnavailable)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	conn.SetReadLimit(32)
	ctx, cancel := context.WithCancel(r.Context())
	var wg sync.WaitGroup
	defer func() { cancel(); conn.CloseNow(); wg.Wait() }()
	acks := make(chan videoAck, maxVideoInFlight)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			id, parseErr := strconv.ParseUint(string(data), 10, 64)
			ack := videoAck{id: id, invalid: kind != websocket.MessageText || parseErr != nil || id == 0}
			for _, b := range data {
				if b < '0' || b > '9' {
					ack.invalid = true
				}
			}
			select {
			case acks <- ack:
			case <-ctx.Done():
				return
			}
			if ack.invalid {
				return
			}
		}
	}()
	var last uint64
	var pending []videoPending
	for {
		frame, updated, generation, changed := h.snapshot()
		if len(pending) < maxVideoInFlight && generation > last && !updated.IsZero() && time.Since(updated) < 15*time.Second && len(frame) <= maxVideoFrame {
			payload := make([]byte, 9+len(frame))
			payload[0] = 1
			binary.LittleEndian.PutUint64(payload[1:9], generation)
			copy(payload[9:], frame)
			writeCtx, done := context.WithTimeout(ctx, 3*time.Second)
			err := conn.Write(writeCtx, websocket.MessageBinary, payload)
			done()
			if err != nil {
				return
			}
			last = generation
			pending = append(pending, videoPending{id: generation, sent: time.Now()})
			continue
		}
		var timeout <-chan time.Time
		var timer *time.Timer
		if len(pending) > 0 {
			timer = time.NewTimer(time.Until(pending[0].sent.Add(15 * time.Second)))
			timeout = timer.C
		}
		// While full, changes do not affect the next action; ignore them until an ACK.
		if len(pending) == maxVideoInFlight {
			changed = nil
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-timeout:
			return
		case <-changed:
		case ack := <-acks:
			if ack.invalid || ack.id > last {
				if timer != nil {
					timer.Stop()
				}
				conn.Close(websocket.StatusPolicyViolation, "invalid acknowledgement")
				return
			}
			for len(pending) > 0 && pending[0].id <= ack.id {
				pending = pending[1:]
			}
		}
		if timer != nil {
			timer.Stop()
		}
	}
}
