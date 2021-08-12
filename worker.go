package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	gitea "github.com/yzzyx/gitea-webhook"
)

// Worker receives webhook events and processes jobs
type Worker struct{}

func NewWorker(ch <-chan *Job) {
	w := &Worker{}

	for job := range ch {
		if job == nil {
			return
		}
		w.ProcessJob(job)
	}
}

// ProcessJob tries to execute the script specified in job,
// and updates the commit status in gitea with the Result
func (w *Worker) ProcessJob(j *Job) {
	// Cleanup when we're done
	defer j.logFile.Close()

	log.Printf("Processing job %s", j.ID)

	status := gitea.CreateStatusOption{
		Context:   j.Context,
		TargetURL: j.TargetURL,
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
		log.Printf("Job %s failed: %s", j.ID, status.Description)
		j.SetStatus(jobStatus, status.Description)
		err = j.Save()
		if err != nil {
			log.Printf("Could not save job status: %v", err)
		}

		err = j.API.UpdateCommitState(j.CommitRepo, j.CommitID, status)
		if err != nil {
			log.Printf("UpcateCommitState(%s) retured error: %+v", status.State, err)
		}
	}

	status.Description = "In progress..."
	status.State = gitea.CommitStatusPending
	err := j.API.UpdateCommitState(j.CommitRepo, j.CommitID, status)
	if err != nil {
		log.Printf("UpcateCommitState(%s) retured error: %+v", status.State, err)
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

	fmt.Fprintf(j.logFile, "[[microci-section]]Prepare git branch\n")
	err = j.ExecScript(filepath.Join(currentDir, "scripts", prepareScript))
	if err != nil {
		handleError(err)
		return
	}

	// Run actual text-script
	fmt.Fprintf(j.logFile, "[[microci-section]]Run %s\n", j.Script)
	err = j.ExecScript(filepath.Join(currentDir, j.Script))
	if err != nil {
		handleError(err)
		return
	}

	log.Printf("Job %s completed successfully!", j.ID)
	status.Description = "success!"
	status.State = gitea.CommitStatusSuccess
	err = j.API.UpdateCommitState(j.CommitRepo, j.CommitID, status)
	if err != nil {
		log.Printf("UpcateCommitState(%s) retured error: %+v", status.State, err)
	}

	j.SetStatus(StatusSuccess)
	err = j.Save()
	if err != nil {
		log.Printf("Could not save job status: %v", err)
	}
}
