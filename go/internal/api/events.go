package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/reeper316099/Lorekeeper/go/internal/store"
)

// streamedEvents are the store events forwarded to the browser over server-sent events.
var streamedEvents = []string{store.EventChange, store.EventExternalChange, store.EventFileError, store.EventFileRestored, store.EventJob}

const pingEvery = 25 * time.Second

func (s *Server) events(w http.ResponseWriter, r *http.Request, _ map[string]string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fmt.Errorf("streaming unsupported"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	out := make(chan []byte, 64)
	send := func(event string, data any) {
		payload := map[string]any{"version": s.Store.Version()}
		if data != nil {
			if b, err := json.Marshal(data); err == nil {
				var extra map[string]any
				if json.Unmarshal(b, &extra) == nil {
					for k, v := range extra {
						payload[k] = v
					}
				}
			}
		}
		b, _ := json.Marshal(payload)
		msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))
		select {
		case out <- msg:
		default: // a stalled client must not block the store; it will resync on the next event
		}
	}
	off := s.Store.On(func(e store.Event) {
		for _, name := range streamedEvents {
			if e.Name == name {
				send(name, e.Data)
				return
			}
		}
	})
	defer off()

	hello, _ := json.Marshal(map[string]any{"version": s.Store.Version()})
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", hello)
	fl.Flush()

	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case msg := <-out:
			if _, err := w.Write(msg); err != nil {
				return
			}
			fl.Flush()
		case <-ping.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			fl.Flush()
		case <-r.Context().Done():
			return
		case <-s.closing:
			return
		}
	}
}
