package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// LogLine is emitted by a log tail stream.
type LogLine struct {
	RunID     int64     `json:"run_id"`
	HostRunID int64     `json:"host_run_id"`
	HostID    int64     `json:"host_id"`
	HostName  string    `json:"host_name"`
	Line      string    `json:"line"`
	At        time.Time `json:"at"`
}

// TailTarget describes one host log file to stream.
type TailTarget struct {
	RunID     int64
	HostRunID int64
	HostID    int64
	HostName  string
	Path      string
}

// LogTailer tails log files only while one or more subscribers are attached.
type LogTailer struct {
	interval time.Duration

	mu      sync.Mutex
	tailers map[string]*tailState
}

// NewLogTailer creates an on-demand polling log tailer.
func NewLogTailer(interval time.Duration) *LogTailer {
	if interval <= 0 {
		interval = 350 * time.Millisecond
	}
	return &LogTailer{interval: interval, tailers: make(map[string]*tailState)}
}

// Subscribe returns historical and live log lines for a target while keeping polling demand-driven.
func (t *LogTailer) Subscribe(ctx context.Context, target TailTarget) (<-chan Event, error) {
	if target.Path == "" {
		return nil, fmt.Errorf("tail target has no log path")
	}
	channel := make(chan Event, 64)

	t.mu.Lock()
	state := t.tailers[target.Path]
	if state == nil {
		state = &tailState{target: target, subscribers: make(map[chan Event]struct{}), stop: make(chan struct{})}
		t.tailers[target.Path] = state
	}
	state.target = target
	state.subscribers[channel] = struct{}{}
	if !state.running {
		state.running = true
		go t.poll(target.Path, state)
	}
	t.mu.Unlock()

	go func() {
		<-ctx.Done()
		t.mu.Lock()
		if current := t.tailers[target.Path]; current != nil {
			if _, ok := current.subscribers[channel]; ok {
				delete(current.subscribers, channel)
				close(channel)
			}
			if len(current.subscribers) == 0 && current.running {
				close(current.stop)
				delete(t.tailers, target.Path)
			}
		}
		t.mu.Unlock()
	}()

	return channel, nil
}

func (t *LogTailer) poll(path string, state *tailState) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	defer func() {
		t.mu.Lock()
		state.running = false
		t.mu.Unlock()
	}()

	for {
		select {
		case <-state.stop:
			return
		case <-ticker.C:
			t.readNew(path, state)
		}
	}
}

func (t *LogTailer) readNew(path string, state *tailState) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Seek(state.offset, io.SeekStart); err != nil {
		return
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		state.offset += int64(len(scanner.Bytes())) + 1
		line := LogLine{RunID: state.target.RunID, HostRunID: state.target.HostRunID, HostID: state.target.HostID, HostName: state.target.HostName, Line: scanner.Text(), At: time.Now().UTC()}
		payload, err := json.Marshal(line)
		if err != nil {
			continue
		}
		state.nextID++
		event := Event{ID: state.nextID, Type: "log.line", Data: payload}
		t.mu.Lock()
		subscribers := make([]chan Event, 0, len(state.subscribers))
		for subscriber := range state.subscribers {
			subscribers = append(subscribers, subscriber)
		}
		t.mu.Unlock()
		for _, subscriber := range subscribers {
			select {
			case subscriber <- event:
			default:
			}
		}
	}
}

type tailState struct {
	target      TailTarget
	offset      int64
	nextID      int64
	subscribers map[chan Event]struct{}
	stop        chan struct{}
	running     bool
}
