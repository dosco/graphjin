package jsn

type intStack struct {
	stA [20]int
	st  []int
	top int
}

// Create a new intStack
func newIntStack() *intStack {
	s := &intStack{top: -1}
	s.st = s.stA[:0]
	return s
}

// Return the number of items in the intStack
func (s *intStack) Len() int {
	return (s.top + 1)
}

// View the top item on the intStack
func (s *intStack) Peek() int {
	if s.top == -1 {
		return -1
	}
	return s.st[s.top]
}

// Pop the top item of the intStack and return it
func (s *intStack) Pop() int {
	if s.top == -1 {
		return -1
	}

	s.top--
	return s.st[(s.top + 1)]
}

// Push a value onto the top of the intStack
func (s *intStack) Push(value int) {
	s.top++
	if len(s.st) <= s.top {
		s.st = append(s.st, value)
	} else {
		s.st[s.top] = value
	}
}
