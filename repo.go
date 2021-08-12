package main

import "sync"

// Queue describes a job queue belonging to a project
// It is uniquely identified by name, which can be
// either a branch name, or a pull-request id, and
// the context it belongs to (e.g. "lint", "test", "default" etc.)
type Queue struct {
	Name    string
	Context string
	Jobs    []*Job
}

// Repository identifies a repository on which jobs can be performed
type Repository struct {
	Name   string
	Queues []*Queue

	mx *sync.Mutex
}

// GetQueue returns a queue matching the supplied named and context
func (r *Repository) GetQueue(name, context string) *Queue {
	r.mx.Lock()
	defer r.mx.Unlock()

	for _, q := range r.Queues {
		if q.Context == context && q.Name == name {
			return q
		}
	}

	q := &Queue{
		Name:    name,
		Context: context,
		Jobs:    nil,
	}

	r.Queues = append(r.Queues, q)
	return q
}
