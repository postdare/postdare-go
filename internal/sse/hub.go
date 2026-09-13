package sse

import (
	"strconv"
	"sync"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[chan string]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: map[string]map[chan string]struct{}{}}
}

func (h *Hub) Subscribe(topic string) (chan string, func()) {
	ch := make(chan string, 64)
	h.mu.Lock()
	if h.clients[topic] == nil {
		h.clients[topic] = map[chan string]struct{}{}
	}
	h.clients[topic][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if subscribers, ok := h.clients[topic]; ok {
			delete(subscribers, ch)
			if len(subscribers) == 0 {
				delete(h.clients, topic)
			}
		}
		h.mu.Unlock()
		close(ch)
	}
}

func (h *Hub) Publish(topic string, line string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients[topic] {
		select {
		case ch <- line:
		default:
		}
	}
}

func DeployTopic(taskID uint64) string {
	return "deploy:" + strconv.FormatUint(taskID, 10)
}

// BoardTopic names the stream a board view subscribes to for issue changes.
func BoardTopic(boardID uint64) string {
	return "board:" + strconv.FormatUint(boardID, 10)
}
