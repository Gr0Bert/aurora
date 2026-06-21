package agent

import (
	"capcompute"
	"context"
	"sync"
)

type inMemorySessionStore[ID comparable, K capcompute.SessionKey[ID]] struct {
	mu       sync.Mutex
	sessions map[ID]*capcompute.Session[K]
}

func newInMemorySessionStore[ID comparable, K capcompute.SessionKey[ID]]() *inMemorySessionStore[ID, K] {
	return &inMemorySessionStore[ID, K]{
		sessions: make(map[ID]*capcompute.Session[K]),
	}
}

func (s *inMemorySessionStore[ID, K]) LoadSession(_ context.Context, sessionID ID) (*capcompute.Session[K], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, capcompute.ErrSessionRequired
	}
	return session, nil
}

func (s *inMemorySessionStore[ID, K]) SaveSession(_ context.Context, sessionID ID, session *capcompute.Session[K]) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = session
	return nil
}
