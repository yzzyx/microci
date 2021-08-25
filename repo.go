package main

import (
	"sync"

	"github.com/yzzyx/microci/job"
)

// Queue describes a job queue belonging to a project
// It is uniquely identified by name, which can be
// either a branch name, or a pull-request id, and
// the context it belongs to (e.g. "lint", "test", "default" etc.)
type Queue struct {
	Name    string
	Context string
	jobs    []*job.Job

	mx *sync.RWMutex
}

// Repository identifies a repository on which jobs can be performed
type Repository struct {
	Name   string
	Queues []*Queue

	mx *sync.Mutex
}

// AddJob adds a new job to the top of the job list
func (q *Queue) AddJob(j *job.Job) {
	q.mx.Lock()
	defer q.mx.Unlock()
	q.jobs = append([]*job.Job{j}, q.jobs...)
}

// GetJob returns a specific job
func (q *Queue) GetJob(id string) *job.Job {
	q.mx.RLock()
	defer q.mx.RUnlock()

	for k := range q.jobs {
		if q.jobs[k].ID == id {
			return q.jobs[k]
		}
	}

	return nil
}

// GetLastJob returns the last available job in the queue
func (q *Queue) GetLastJob() *job.Job {
	q.mx.RLock()
	defer q.mx.RUnlock()

	if q.jobs == nil {
		return nil
	}
	return q.jobs[0]
}

// NewRepository returns a newly initialized repository
func NewRepository(name string) *Repository {
	return &Repository{
		Name:   name,
		Queues: nil,
		mx:     &sync.Mutex{},
	}
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
		jobs:    nil,
		mx:      &sync.RWMutex{},
	}

	r.Queues = append(r.Queues, q)
	return q
}
