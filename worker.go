package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	gitea "github.com/yzzyx/gitea-webhook"
)

// Result contains information about the state of a job
type Result struct {
	OutputFile string
	Error      error
	Finished   bool
}

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
	Script     string          `json:"script"`
	Folder     string          `json:"-"`
	CommitID   string          `json:"commit_id"`
	CommitRepo string          `json:"commit_repo"`
	Type       gitea.EventType `json:"type"`
	Event      gitea.Event     `json:"event"`

	ctx       context.Context
	ctxCancel func()
	logFile   *os.File
	config    *Config

	Status JobStatus `json:"status"`
	Error  string    `json:"error"`

	mx sync.Mutex
}

// Worker receives webhook events and processes jobs
type Worker struct {
	api *gitea.API
	cfg *Config

	jobs      map[string]*Job
	jobsMutex *sync.RWMutex
}

func randomString() (string, error) {
	b := make([]byte, 24)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", b), nil
}

// SetStatus updates the current status of the job, and saves it
func (j *Job) SetStatus(st JobStatus, err ...string) {
	j.mx.Lock()
	defer j.mx.Unlock()

	j.Status = st
	if len(err) > 0 {
		j.Error = err[0]
	}
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

// SetupJob prepares the job for execution
func (w *Worker) SetupJob(j *Job) error {
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

	j.Folder = filepath.Join(w.cfg.Jobs.Folder, j.ID)
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

// ProcessJob tries to execute the script specified in job,
// and updates the commit status in gitea with the Result
func (w *Worker) ProcessJob(j *Job) {
	// Cleanup when we're done
	defer j.logFile.Close()

	status := gitea.CreateStatusOption{
		Context:   "",
		TargetURL: path.Join(w.cfg.Server.Address, "job", j.ID),
	}

	handleError := func(err error) {
		if err == nil {
			return
		}

		exit := &exec.ExitError{}
		// If our script retuned an error, we should inform gitea
		jobStatus := StatusError
		if errors.As(err, &exit) {
			status.Description = fmt.Sprintf("script failed with code %d", exit.ExitCode())
			status.State = gitea.CommitStatusFailure
		} else if errors.Is(err, errExecCancelled) {
			status.Description = "Job cancelled by user"
			status.State = gitea.CommitStatusError
			jobStatus = StatusCancelled
		} else if errors.Is(err, errExecTimedOut) {
			status.Description = "Job execution timed out"
			status.State = gitea.CommitStatusError
			jobStatus = StatusTimeout
		} else {
			status.Description = fmt.Sprintf("error executing script: %+v", err)
			status.State = gitea.CommitStatusError
		}
		j.SetStatus(jobStatus, status.Description)
		log.Print(status.Description)

		err = w.api.UpdateCommitState(j.CommitRepo, j.CommitID, status)
		if err != nil {
			log.Printf("UpcateCommitState(%s) retured error: %+v\n", status.State, err)
		}
	}

	status.Description = "In progress..."
	status.State = gitea.CommitStatusPending
	err := w.api.UpdateCommitState(j.CommitRepo, j.CommitID, status)
	if err != nil {
		log.Printf("UpcateCommitState(%s) retured error: %+v\n", status.State, err)
	}

	currentDir, err := os.Getwd()
	if err != nil {
		handleError(err)
		return
	}

	// Run preparation scripts
	prepareScript := "prepare-pr.sh"
	if j.Type == gitea.EventTypePush {
		prepareScript = "prepare-push.sh"
	}

	err = j.ExecScript(filepath.Join(currentDir, "scripts", prepareScript))
	if err != nil {
		handleError(err)
		return
	}

	// Run actual text-script
	err = j.ExecScript(filepath.Join(currentDir, j.Script))
	if err != nil {
		handleError(err)
		return
	}

	status.Description = "success!"
	status.State = gitea.CommitStatusSuccess
	j.SetStatus(StatusSuccess)
}

func (w *Worker) onSuccess(typ gitea.EventType, ev gitea.Event, responseWriter http.ResponseWriter, r *http.Request) {
	var scriptName string
	var branchName string

	job := &Job{
		Type:   typ,
		Event:  ev,
		config: w.cfg,
	}

	switch typ {
	case gitea.EventTypePush:
		scriptName = "push.sh"
		branchName = strings.TrimPrefix(ev.Ref, "refs/heads/")
		job.CommitRepo = ev.Repository.FullName
		job.CommitID = ev.After
	case gitea.EventTypePullRequest:
		scriptName = "pull-request.sh"
		branchName = ev.PullRequest.Base.Ref
		job.CommitRepo = ev.PullRequest.Base.Repo.FullName
		job.CommitID = ev.PullRequest.Head.SHA
	default:
		return
	}

	if !isDir(ev.Repository.FullName) {
		log.Printf("ignoring repositoriy '%s' - is not a directory", ev.Repository.FullName)
		return
	}

	scripts := []string{
		filepath.Join(ev.Repository.FullName, branchName, scriptName),
		filepath.Join(ev.Repository.FullName, scriptName),
	}

	for _, script := range scripts {
		if !isFile(script) {
			continue
		}

		job.Script = script
		err := w.SetupJob(job)
		if err != nil {
			log.Printf("SetupJob: %+v", err)
			responseWriter.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(responseWriter, "Could not process webhook: %+v", err)
			return
		}

		w.jobsMutex.Lock()
		w.jobs[job.ID] = job
		w.jobsMutex.Unlock()

		go w.ProcessJob(job)
		break
	}
}

// GetJob returns a Job structure, either from memory if it exists, or recreated from disk
func (w *Worker) GetJob(id string) (*Job, error) {
	// First, check if we have it in memory
	job := func() *Job {
		w.jobsMutex.RLock()
		defer w.jobsMutex.RUnlock()
		return w.jobs[id]
	}()
	if job != nil {
		return job, nil
	}

	// Otherwise, recreate job from disk
	jobPath := filepath.Join(w.cfg.Jobs.Folder, id)
	st, err := os.Stat(jobPath)
	if err != nil {
		return nil, err
	}

	if !st.IsDir() {
		return nil, fmt.Errorf("expected '%s' to be a directory", jobPath)
	}

	f, err := os.Open(filepath.Join(jobPath, "info.json"))
	if err != nil {
		return nil, err
	}

	job = &Job{}
	err = json.NewDecoder(f).Decode(&job)
	if err != nil {
		return nil, err
	}

	job.ID = id
	job.Folder = jobPath

	// Save in memory for later
	w.jobsMutex.Lock()
	defer w.jobsMutex.Unlock()
	w.jobs[id] = job
	return job, nil
}
