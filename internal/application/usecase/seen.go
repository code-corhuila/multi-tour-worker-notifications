package usecase

import "sync"

// seen is a bounded set of recent event ids kept in memory (piece plan D-03): a worker has no database, so a repeat after a
// restart can send a second e-mail, the cost accepted in the plan. The oldest id leaves when the set is full.
type seen struct {
	mu    sync.Mutex
	limit int
	order []string
	set   map[string]struct{}
}

func newSeen(limit int) *seen { return &seen{limit: limit, set: make(map[string]struct{}, limit)} }

func (s *seen) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.set[id]
	return ok
}

func (s *seen) add(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.set[id]; ok {
		return
	}
	if len(s.order) == s.limit {
		delete(s.set, s.order[0])
		s.order = s.order[1:]
	}
	s.order = append(s.order, id)
	s.set[id] = struct{}{}
}
