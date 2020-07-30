package util

type StackInt32 struct {
	stA [20]int32
	st  []int32
	top int
}

// Create a new StackInt32
func NewStackInt32() *StackInt32 {
	s := &StackInt32{top: -1}
	s.st = s.stA[:0]
	return s
}

// Return the number of items in the StackInt32
func (s *StackInt32) Len() int {
	return (s.top + 1)
}

// View the top item on the StackInt32
func (s *StackInt32) Peek() int32 {
	if s.top == -1 {
		return -1
	}
	return s.st[s.top]
}

// Pop the top item of the StackInt32 and return it
func (s *StackInt32) Pop() int32 {
	if s.top == -1 {
		return -1
	}

	s.top--
	return s.st[(s.top + 1)]
}

// Push a value onto the top of the StackInt32
func (s *StackInt32) Push(value int32) {
	s.top++
	if len(s.st) <= s.top {
		s.st = append(s.st, value)
	} else {
		s.st[s.top] = value
	}
}
