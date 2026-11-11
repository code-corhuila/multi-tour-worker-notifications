package usecase

// seen is a bounded set of recent event ids kept in memory (piece plan D-03): a worker has no database, so a repeat after a
// restart can send a second e-mail, the cost accepted in the plan. The oldest id leaves when the set is full. It has no lock of
// its own: Notify.Handle holds one around every use.
type seen struct {
	limit int
	order []string
	set   map[string]struct{}
}

func newSeen(limit int) *seen { return &seen{limit: limit, set: make(map[string]struct{}, limit)} }

func (s *seen) has(id string) bool {
	_, ok := s.set[id]
	return ok
}

func (s *seen) add(id string) {
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
