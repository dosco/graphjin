package psql

type IntStack struct {
	stA [20]int32
	st  []int32
	top int
}

// Create a new IntStack
func NewIntStack() *IntStack {
	s := &IntStack{top: -1}
	s.st = s.stA[:0]
	return s
}

// Return the number of items in the IntStack
func (s *IntStack) Len() int {
	return (s.top + 1)
}

// View the top item on the IntStack
func (s *IntStack) Peek() int32 {
	if s.top == -1 {
		return -1
	}
	return s.st[s.top]
}

// Pop the top item of the IntStack and return it
func (s *IntStack) Pop() int32 {
	if s.top == -1 {
		return -1
	}

	s.top--
	return s.st[(s.top + 1)]
}

// Push a value onto the top of the IntStack
func (s *IntStack) Push(value int32) {
	s.top++
	if len(s.st) <= s.top {
		s.st = append(s.st, value)
	} else {
		s.st[s.top] = value
	}
}
