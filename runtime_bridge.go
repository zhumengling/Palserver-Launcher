package main

import "sync"

// AgentEvent is shared by the Wails desktop bridge and the Linux web agent.
// Keeping events inside App removes the UI framework dependency from services.
type AgentEvent struct {
	Name string `json:"name"`
	Args []any  `json:"args"`
}

type appEventHub struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]chan AgentEvent
}

func (hub *appEventHub) subscribe() (uint64, <-chan AgentEvent) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.subscribers == nil {
		hub.subscribers = map[uint64]chan AgentEvent{}
	}
	hub.nextID++
	channel := make(chan AgentEvent, 64)
	hub.subscribers[hub.nextID] = channel
	return hub.nextID, channel
}

func (hub *appEventHub) unsubscribe(id uint64) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if channel := hub.subscribers[id]; channel != nil {
		delete(hub.subscribers, id)
		close(channel)
	}
}

func (hub *appEventHub) publish(event AgentEvent) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	for _, channel := range hub.subscribers {
		select {
		case channel <- event:
		default:
			// A slow browser must not block server lifecycle operations.
		}
	}
}

func (a *App) emit(name string, args ...any) {
	event := AgentEvent{Name: name, Args: args}
	a.events.publish(event)
	emitPlatformEvent(a, event)
}
