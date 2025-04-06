package queue

// Queue is a generic FIFO queue that can hold any type.
type Queue[T any] struct {
	items []T
}

// New creates and returns a new Queue instance.
func New[T any]() *Queue[T] {
	return &Queue[T]{items: []T{}}
}

// Enqueue adds an element to the end of the queue.
func (q *Queue[T]) Enqueue(item T) {
	q.items = append(q.items, item)
}

// Dequeue removes and returns the front element of the queue.
// The boolean indicates whether an element was dequeued (false if the queue was empty).
func (q *Queue[T]) Dequeue() (T, bool) {
	if len(q.items) == 0 {
		var zero T
		return zero, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	return item, true
}

// Peek returns the front element without removing it from the queue.
// The boolean indicates whether an element was found (false if the queue is empty).
func (q *Queue[T]) Peek() (T, bool) {
	if len(q.items) == 0 {
		var zero T
		return zero, false
	}
	return q.items[0], true
}

// Len returns the number of elements in the queue.
func (q *Queue[T]) Len() int {
	return len(q.items)
}

// IsEmpty returns true if the queue is empty.
func (q *Queue[T]) IsEmpty() bool {
	return len(q.items) == 0
}
