package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// Event is a structured event suitable for Server-Sent Events delivery.
type Event struct {
	ID   int64           `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Hub stores event sessions and fans out events to subscribers.
type Hub struct {
	mu       sync.Mutex
	sessions map[string]*session
}

// NewHub creates an empty event hub.
func NewHub() *Hub {
	return &Hub{sessions: make(map[string]*session)}
}

// Create creates a new event stream session and returns its id.
func (h *Hub) Create() (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[id] = &session{subscribers: make(map[chan Event]struct{})}
	return id, nil
}

// Ensure creates a named session when it does not already exist.
func (h *Hub) Ensure(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.sessions[sessionID]; ok {
		return
	}
	h.sessions[sessionID] = &session{subscribers: make(map[chan Event]struct{})}
}

// SubscriberCount reports the current number of live subscribers for a session.
func (h *Hub) SubscriberCount(sessionID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	session, ok := h.sessions[sessionID]
	if !ok {
		return 0
	}
	return len(session.subscribers)
}

// Publish marshals data and publishes an event to a session.
func (h *Hub) Publish(sessionID string, eventType string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal stream event: %w", err)
	}
	h.mu.Lock()
	session, ok := h.sessions[sessionID]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("stream session %s not found", sessionID)
	}
	session.nextID++
	event := Event{ID: session.nextID, Type: eventType, Data: payload}
	session.history = append(session.history, event)
	subscribers := make([]chan Event, 0, len(session.subscribers))
	for subscriber := range session.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	h.mu.Unlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
	return nil
}

// Complete marks a session complete and closes subscriber channels.
func (h *Hub) Complete(sessionID string) {
	h.mu.Lock()
	session, ok := h.sessions[sessionID]
	if !ok || session.closed {
		h.mu.Unlock()
		return
	}
	session.closed = true
	subscribers := make([]chan Event, 0, len(session.subscribers))
	for subscriber := range session.subscribers {
		subscribers = append(subscribers, subscriber)
		delete(session.subscribers, subscriber)
	}
	h.mu.Unlock()

	for _, subscriber := range subscribers {
		close(subscriber)
	}
}

// Subscribe returns a channel that receives historical and live events.
func (h *Hub) Subscribe(ctx context.Context, sessionID string) (<-chan Event, error) {
	h.mu.Lock()
	session, ok := h.sessions[sessionID]
	if !ok {
		h.mu.Unlock()
		return nil, fmt.Errorf("stream session %s not found", sessionID)
	}
	channel := make(chan Event, len(session.history)+16)
	for _, event := range session.history {
		channel <- event
	}
	if session.closed {
		close(channel)
		h.mu.Unlock()
		return channel, nil
	}
	session.subscribers[channel] = struct{}{}
	h.mu.Unlock()

	go func() {
		<-ctx.Done()
		h.mu.Lock()
		if session, ok := h.sessions[sessionID]; ok {
			if _, ok := session.subscribers[channel]; ok {
				delete(session.subscribers, channel)
				close(channel)
			}
		}
		h.mu.Unlock()
	}()

	return channel, nil
}

type session struct {
	nextID      int64
	history     []Event
	subscribers map[chan Event]struct{}
	closed      bool
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create stream id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
