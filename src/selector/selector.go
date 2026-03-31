package selector

// Compiled is the immutable selector snapshot used by runtime dispatch.
// It keeps routing expanded so the hot path only performs a single map lookup.
type Compiled[T any] struct {
	Name          string
	ValuesByKey   map[string][]T
	DefaultValues []T
}

func NewCompiled[T any](name string, matchKeyCapacity int) *Compiled[T] {
	if matchKeyCapacity < 0 {
		matchKeyCapacity = 0
	}
	return &Compiled[T]{
		Name:        name,
		ValuesByKey: make(map[string][]T, matchKeyCapacity),
	}
}

func NewDefaultOnlyCompiled[T any](name string, values []T) *Compiled[T] {
	cs := NewCompiled[T](name, 0)
	cs.DefaultValues = append([]T(nil), values...)
	return cs
}

func (s *Compiled[T]) Match(key string) []T {
	if s == nil {
		return nil
	}
	if values, ok := s.ValuesByKey[key]; ok {
		return values
	}
	return s.DefaultValues
}
