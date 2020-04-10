package jsn

type skipInfo struct {
	ss, se int
}

type stack struct {
	stA [20]skipInfo
	st  []skipInfo
	top int
}

// Create a new stack
func newStack() *stack {
	s := &stack{top: -1}
	s.st = s.stA[:0]
	return s
}

// Return the number of items in the stack
func (s *stack) Len() int {
	return (s.top + 1)
}

// View the top item on the stack
func (s *stack) Peek() *skipInfo {
	if s.top == -1 {
		return nil
	}
	return &s.st[s.top]
}

// Pop the top item of the stack and return it
func (s *stack) Pop() *skipInfo {
	if s.top == -1 {
		return nil
	}

	s.top--
	return &s.st[(s.top + 1)]
}

// Push a value onto the top of the stack
func (s *stack) Push(value skipInfo) {
	s.top++
	if len(s.st) <= s.top {
		s.st = append(s.st, value)
	} else {
		s.st[s.top] = value
	}
}
