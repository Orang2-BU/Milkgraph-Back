package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"milkgraph/internal/store"
)

const replayLimit = 1000
const clientBuffer = 128
const writeTimeout = 5 * time.Second

type client struct {
	events chan store.Event
	done   chan struct{}
	after  int64
}

type Hub struct {
	db      *store.DB
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
	ready   chan struct{}
	mu      sync.Mutex
	clients map[*client]struct{}
	cursor  int64
}

func New(db *store.DB) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{db: db, ctx: ctx, cancel: cancel, ready: make(chan struct{}), clients: make(map[*client]struct{})}
}

// Run returns immediately; one shared poller serves every connected client.
func (h *Hub) Run(ctx context.Context) {
	h.once.Do(func() {
		go func() {
			select {
			case <-ctx.Done():
				h.Close()
			case <-h.ctx.Done():
			}
		}()
		go h.poll()
	})
}
func (h *Hub) Close() { h.cancel() }

func (h *Hub) poll() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	initialized := false
	for {
		if h.ctx.Err() != nil {
			return
		}
		if !initialized {
			cursor, err := h.db.LatestEventID(h.ctx)
			if err == nil {
				h.mu.Lock()
				h.cursor = cursor
				h.mu.Unlock()
				initialized = true
				close(h.ready)
			}
		}
		if initialized {
			for {
				h.mu.Lock()
				cursor := h.cursor
				h.mu.Unlock()
				events, err := h.db.Events(h.ctx, cursor, 1000)
				if err != nil {
					break
				}
				for _, e := range events {
					h.publish(e)
				}
				if len(events) < 1000 || h.ctx.Err() != nil {
					break
				}
			}
		}
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Hub) publish(e store.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cursor = e.ID
	for c := range h.clients {
		if e.ID <= c.after {
			continue
		}
		select {
		case c.events <- e:
		default:
			// Disconnect instead of hiding a gap; Last-Event-ID replays durable history.
			delete(h.clients, c)
			close(c.done)
		}
	}
}
func (h *Hub) subscribe() *client {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := &client{events: make(chan store.Event, clientBuffer), done: make(chan struct{}), after: h.cursor}
	h.clients[c] = struct{}{}
	return c
}
func (h *Hub) unsubscribe(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.done)
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := r.Header.Get("Last-Event-ID")
	var after int64
	if raw != "" {
		var err error
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			http.Error(w, "invalid Last-Event-ID", http.StatusBadRequest)
			return
		}
	}
	h.Run(h.ctx)
	initialCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-h.ready:
	case <-initialCtx.Done():
		http.Error(w, "event stream unavailable", http.StatusServiceUnavailable)
		return
	case <-h.ctx.Done():
		http.Error(w, "event stream closed", http.StatusServiceUnavailable)
		return
	}
	// Subscribe before taking the replay snapshot, so the replay/live boundary has no gap.
	c := h.subscribe()
	defer h.unsubscribe(c)
	snapshot, err := h.db.LatestEventID(initialCtx)
	if err != nil {
		http.Error(w, "event stream unavailable", http.StatusServiceUnavailable)
		return
	}
	if raw == "" {
		after = snapshot
	}
	if after > snapshot {
		http.Error(w, "Last-Event-ID is ahead of server", http.StatusBadRequest)
		return
	}
	var replay []store.Event
	if after < snapshot {
		replay, err = h.db.Events(initialCtx, after, replayLimit)
		if err != nil {
			http.Error(w, "event replay unavailable", http.StatusServiceUnavailable)
			return
		}
		if len(replay) == replayLimit && replay[len(replay)-1].ID < snapshot {
			http.Error(w, "replay exceeds 1000 events; refresh state", http.StatusConflict)
			return
		}
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(data string) error {
		if err := rc.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if _, err := fmt.Fprint(w, data); err != nil {
			return err
		}
		return rc.Flush()
	}
	send := func(e store.Event) error {
		body, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return write(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Kind, body))
	}
	defer rc.SetWriteDeadline(time.Time{})
	for _, e := range replay {
		if e.ID > snapshot {
			break
		}
		select {
		case <-c.done:
			return
		default:
		}
		if err = send(e); err != nil {
			return
		}
		after = e.ID
	}
	if len(replay) == 0 {
		if err = write(": connected\n\n"); err != nil {
			return
		}
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.ctx.Done():
			return
		case <-c.done:
			return
		case e := <-c.events:
			if e.ID <= after {
				continue
			}
			select {
			case <-c.done:
				return
			default:
			}
			if err = send(e); err != nil {
				return
			}
			after = e.ID
		case <-heartbeat.C:
			if err = write(": heartbeat\n\n"); err != nil {
				return
			}
		}
	}
}
