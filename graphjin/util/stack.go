package util

type (
	Stack struct {
		top    *StackNode
		length int
	}
	StackNode struct {
		value interface{}
		prev  *StackNode
	}
)

// Create a new Stack
func NewStack() *Stack {
	return &Stack{nil, 0}
}

// Return the number of items in the Stack
func (this *Stack) Len() int {
	return this.length
}

// View the top item on the Stack
func (this *Stack) Peek() interface{} {
	if this.length == 0 {
		return nil
	}
	return this.top.value
}

// Pop the top item of the Stack and return it
func (this *Stack) Pop() interface{} {
	if this.length == 0 {
		return nil
	}

	n := this.top
	this.top = n.prev
	this.length--
	return n.value
}

// Push a value onto the top of the Stack
func (this *Stack) Push(value interface{}) {
	n := &StackNode{value, this.top}
	this.top = n
	this.length++
}
