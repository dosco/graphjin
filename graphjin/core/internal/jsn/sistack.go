package jsn

type skipInfo struct {
	ss, se int
}

type siStack struct {
	stA [20]skipInfo
	st  []skipInfo
	top int
}

// Create a new siStack
func newSkipInfoStack() *siStack {
	s := &siStack{top: -1}
	s.st = s.stA[:0]
	return s
}

// Return the number of items in the siStack
func (s *siStack) Len() int {
	return (s.top + 1)
}

// View the top item on the siStack
func (s *siStack) Peek() *skipInfo {
	if s.top == -1 {
		return nil
	}
	return &s.st[s.top]
}

// Pop the top item of the siStack and return it
func (s *siStack) Pop() *skipInfo {
	if s.top == -1 {
		return nil
	}

	s.top--
	return &s.st[(s.top + 1)]
}

// Push a value onto the top of the siStack
func (s *siStack) Push(value skipInfo) {
	s.top++
	if len(s.st) <= s.top {
		s.st = append(s.st, value)
	} else {
		s.st[s.top] = value
	}
}
