package graph

type Stack struct {
	stA [20]int32
	st  []int32
	top int
}

// Create a new Stack
func NewStack() *Stack {
	s := &Stack{top: -1}
	s.st = s.stA[:0]
	return s
}

// Return the number of items in the Stack
func (s *Stack) Len() int {
	return (s.top + 1)
}

// View the top item on the Stack
func (s *Stack) Peek() int32 {
	if s.top == -1 {
		return -1
	}
	return s.st[s.top]
}

// Pop the top item of the Stack and return it
func (s *Stack) Pop() int32 {
	if s.top == -1 {
		return -1
	}

	s.top--
	return s.st[(s.top + 1)]
}

// Push a value onto the top of the Stack
func (s *Stack) Push(value int32) {
	s.top++
	if len(s.st) <= s.top {
		s.st = append(s.st, value)
	} else {
		s.st[s.top] = value
	}
}
