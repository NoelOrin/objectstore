package objectstore

import (
	"context"
	"sort"
	"strings"
	"sync"
)

type memoryObject struct {
	data    []byte
	mime    string
	updated int64
}

// Memory is an in-process backend for tests and ephemeral deployments.
type Memory struct {
	mu      sync.RWMutex
	objects map[string]memoryObject
	clock   func() int64
}

var _ Backend = (*Memory)(nil)

func NewMemory() *Memory {
	return &Memory{objects: make(map[string]memoryObject), clock: func() int64 { return nowMS() }}
}

func (m *Memory) Name() string { return "memory" }

func (m *Memory) Put(_ context.Context, key string, data []byte, contentType string) error {
	if key == "" {
		return nil
	}
	m.mu.Lock()
	m.objects[key] = memoryObject{data: append([]byte(nil), data...), mime: contentType, updated: m.clock()}
	m.mu.Unlock()
	return nil
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	obj, ok := m.objects[key]
	if !ok {
		return nil, "", ErrNotFound
	}
	return append([]byte(nil), obj.data...), obj.mime, nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()
	return nil
}

func (m *Memory) Stat(_ context.Context, key string) (int64, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	obj, ok := m.objects[key]
	if !ok {
		return 0, 0, ErrNotFound
	}
	return int64(len(obj.data)), obj.updated, nil
}

func (m *Memory) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.objects))
	for key := range m.objects {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *Memory) TestConnection(context.Context) error { return nil }
