package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gitea "github.com/yzzyx/gitea-webhook"
)

// JobStatus contains information about the current state of a job
type JobStatus int

// All currently defined job statuses
var (
	StatusPending   JobStatus = 0
	StatusExecuting JobStatus = 1
	StatusSuccess   JobStatus = 2
	StatusError     JobStatus = 3
	StatusCancelled JobStatus = 4
	StatusTimeout   JobStatus = 5
)

// IsFinished returns true if status means that job is not active anymore
func (s JobStatus) IsFinished() bool {
	if s != StatusPending && s != StatusExecuting {
		return true
	}
	return false
}

// Job defines a single webhook event to be processed
type Job struct {
	ID         string          `json:"-"`
	Context    string          `json:"context"`
	Script     string          `json:"script"`
	Folder     string          `json:"-"`
	CommitID   string          `json:"commit_id"`
	CommitRepo string          `json:"commit_repo"`
	Type       gitea.EventType `json:"type"`
	Event      gitea.Event     `json:"event"`

	API       *gitea.API
	TargetURL string

	ctx       context.Context
	ctxCancel func()
	logFile   *os.File
	config    *Config

	Status            JobStatus `json:"status"`
	StatusDescription string    `json:"status_description"`

	statusUpdateMx     *sync.Mutex
	statusCancelUpdate func()

	mx sync.Mutex
}

func randomString() (string, error) {
	b := make([]byte, 24)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", b), nil
}

// Setup prepares the job for execution
func (j *Job) Setup() error {
	var err error

	if j.ID == "" {
		j.ID, err = randomString()
		if err != nil {
			return err
		}
	}

	if j.ctx == nil {
		j.ctx, j.ctxCancel = context.WithCancel(context.Background())
	}

	if j.statusUpdateMx == nil {
		j.statusUpdateMx = &sync.Mutex{}
	}

	j.TargetURL = strings.TrimSuffix(j.TargetURL, "/") + path.Join("/job", j.ID)
	j.Folder = filepath.Join(j.config.Jobs.Folder, j.ID)
	gitFolder := filepath.Join(j.Folder, "git")
	err = os.MkdirAll(gitFolder, 0755)
	if err != nil {
		return err
	}

	// Create and open log-file now, so that we can use it for other scripts later
	j.logFile, err = os.Create(filepath.Join(j.Folder, "logs"))
	if err != nil {
		return err
	}
	return j.Save()
}

// SetStatus updates the current status of the job, and saves it
func (j *Job) SetStatus(st JobStatus, description ...string) {
	j.mx.Lock()
	defer j.mx.Unlock()

	j.Status = st
	j.StatusDescription = strings.Join(description, " ")
	go j.PushStatus()
}

func (j *Job) PushStatus() {
	j.statusUpdateMx.Lock()
	if j.statusCancelUpdate != nil {
		j.statusCancelUpdate()
	}

	var ctx context.Context
	ctx, j.statusCancelUpdate = context.WithCancel(context.Background())
	j.statusUpdateMx.Unlock()

	status := gitea.CreateStatusOption{
		Context:     j.Context,
		TargetURL:   j.TargetURL,
		Description: j.StatusDescription,
	}

	switch j.Status {
	case StatusPending, StatusExecuting:
		status.State = gitea.CommitStatusPending
	case StatusCancelled, StatusTimeout:
		status.State = gitea.CommitStatusError
	case StatusSuccess:
		status.State = gitea.CommitStatusSuccess
	case StatusError:
		status.State = gitea.CommitStatusFailure
	}

	var err error
	for i := 0; i < 3; i++ {
		err = j.API.UpdateCommitState(j.CommitRepo, j.CommitID, status)
		if err == nil {
			return
		}

		select {
		case <-ctx.Done():
			// We've been cancelled
			return
		case <-time.After(1 * time.Second):
		}
	}
	log.Printf("UpdateCommitState(%s) failed after 3 attempts. last error: %v", status.State, err)
}

// Save job information to JSON file
func (j *Job) Save() error {
	j.mx.Lock()
	defer j.mx.Unlock()

	f, err := os.Create(filepath.Join(j.Folder, "info.json"))
	if err != nil {
		return err
	}

	err = json.NewEncoder(f).Encode(j)
	if err != nil {
		return err
	}
	return nil
}
