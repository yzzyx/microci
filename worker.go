package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// Job defines a single webhook event to be processed
type Job struct {
	ID         string
	Script     string
	Folder     string
	CommitID   string
	CommitRepo string
	Event      gitea.Event

	ctx       context.Context
	ctxCancel func()

	result *Result
}

// Worker receives webhook events and processes jobs
type Worker struct {
	api *gitea.API
	cfg *Config
}

func randomString() (string, error) {
	b := make([]byte, 24)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
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

	j.Folder = filepath.Join("jobs", j.ID)
	gitFolder := filepath.Join(j.Folder, "git")
	err = os.MkdirAll(gitFolder, 0755)
	if err != nil {
		return err
	}
	return nil
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
// and updates the commit status in gitea with the result
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

	err = j.ExecScript()
	if err != nil {
		exit := &exec.ExitError{}
		// If our script retuned an error, we should inform gitea
		if errors.As(err, &exit) {
			status.Description = fmt.Sprintf("script failed with code %d", exit.ExitCode())
			status.State = gitea.CommitStatusFailure
		} else {
			status.Description = fmt.Sprintf("error executing script: %+v", err)
			status.State = gitea.CommitStatusError
		}
		log.Print(status.Description)
	} else {
		status.Description = "success!"
		status.State = gitea.CommitStatusSuccess
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

		go w.ProcessJob(job)
		break
	}
}
