package util

type StackInf struct {
	stA [20]interface{}
	st  []interface{}
	top int
}

// Create a new StackInf
func NewStackInf() *StackInf {
	s := &StackInf{top: -1}
	s.st = s.stA[:0]
	return s
}

// Return the number of items in the StackInf
func (s *StackInf) Len() int {
	return (s.top + 1)
}

// View the top item on the StackInf
func (s *StackInf) Peek() interface{} {
	if s.top == -1 {
		return -1
	}
	return s.st[s.top]
}

// Pop the top item of the StackInf and return it
func (s *StackInf) Pop() interface{} {
	if s.top == -1 {
		return -1
	}

	s.top--
	return s.st[(s.top + 1)]
}

// Push a value onto the top of the StackInf
func (s *StackInf) Push(value interface{}) {
	s.top++
	if len(s.st) <= s.top {
		s.st = append(s.st, value)
	} else {
		s.st[s.top] = value
	}
}
