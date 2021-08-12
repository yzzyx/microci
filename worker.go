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

	handleError := func(err error) {
		if err == nil {
			return
		}

		exit := &exec.ExitError{}
		// If our script retuned an error, we should inform gitea
		jobStatus := StatusError
		description := err.Error()
		if errors.As(err, &exit) {
			description = fmt.Sprintf("script failed with code %d", exit.ExitCode())
		} else if errors.Is(err, errExecCancelled) {
			description = "Job cancelled by user"
			jobStatus = StatusCancelled
		} else if errors.Is(err, errExecTimedOut) {
			description = "Job execution timed out"
			jobStatus = StatusTimeout
		}
		log.Printf("Job %s failed: %s", j.ID, description)
		j.SetStatus(jobStatus, description)
		err = j.Save()
		if err != nil {
			log.Printf("Could not save job status: %v", err)
		}

	}

	j.SetStatus(StatusExecuting, "In progress...")
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
	j.SetStatus(StatusSuccess, "Job completed successfully!")
	err = j.Save()
	if err != nil {
		log.Printf("Could not save job status: %v", err)
	}
}
