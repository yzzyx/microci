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
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	httptransport "github.com/go-git/go-git/v5/plumbing/transport/http"
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
	ID         string      `json:"-"`
	Script     string      `json:"script"`
	Folder     string      `json:"-"`
	CommitID   string      `json:"commit_id"`
	CommitRepo string      `json:"commit_repo"`
	Event      gitea.Event `json:"event"`

	ctx       context.Context `json:"-"`
	ctxCancel func()          `json:"-"`

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
	return base64.StdEncoding.EncodeToString(b), nil
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

	return j.Save()
}

// CloneAndMerge clones the repository specified in job, and merges with target branch if neccessary
func (w *Worker) CloneAndMerge(j *Job) error {
	var auth httptransport.BasicAuth
	if w.cfg.Gitea.Username == "" {
		auth.Username = "use_token"
		auth.Password = w.cfg.Gitea.Token
	} else {
		auth.Username = w.cfg.Gitea.Username
		auth.Password = w.cfg.Gitea.Password
	}

	gitFolder := filepath.Join(j.Folder, "git")
	_, err := git.PlainCloneContext(j.ctx, gitFolder, false,
		&git.CloneOptions{
			URL:           j.Event.Repository.HtmlURL,
			Auth:          &auth,
			ReferenceName: plumbing.ReferenceName(j.Event.Ref),
			SingleBranch:  true,
		})

	if err != nil {
		return err
	}

	// FIXME - for pull requests, merge before executing script
	return nil
}

// ProcessJob tries to execute the script specified in job,
// and updates the commit status in gitea with the Result
func (w *Worker) ProcessJob(j *Job) {
	status := gitea.CreateStatusOption{
		Context:     "",
		Description: "test?",
		State:       gitea.CommitStatusPending,
		TargetURL:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}

	err := w.api.UpdateCommitState(j.CommitRepo, j.CommitID, status)
	if err != nil {
		log.Printf("UpcateCommitState(%s) retured error: %+v\n", status.State, err)
	}

	err = j.ExecScript(w.cfg)
	if err != nil {

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
	} else {
		status.Description = "success!"
		status.State = gitea.CommitStatusSuccess
		j.SetStatus(StatusSuccess)
	}

	err = w.api.UpdateCommitState(j.CommitRepo, j.CommitID, status)
	if err != nil {
		log.Printf("UpcateCommitState(%s) retured error: %+v\n", status.State, err)
	}
}

func (w *Worker) onSuccess(typ gitea.EventType, ev gitea.Event, responseWriter http.ResponseWriter, r *http.Request) {
	var scriptName string
	var branchName string

	job := &Job{}

	switch typ {
	case gitea.EventTypePush:
		scriptName = "push.sh"
		branchName = strings.TrimPrefix(ev.Ref, "refs/heads/")
		job.CommitRepo = ev.Repository.FullName
		job.CommitID = ev.After
	case gitea.EventTypePullRequest:
		scriptName = "pull-request.sh"
		branchName = ev.PullRequest.Base.Ref
		job.CommitRepo = ev.PullRequest.Head.Repo.FullName
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
