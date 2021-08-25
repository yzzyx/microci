package job

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

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
			description = "job cancelled"
			jobStatus = StatusCancelled
		} else if errors.Is(err, errExecTimedOut) {
			description = "job execution timed out"
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

	// Run preparation scripts
	prepareScript := "prepare-pr.sh"
	if j.Type == gitea.EventTypePush {
		prepareScript = "prepare-push.sh"
	}

	script, err := filepath.Abs(filepath.Join(j.Config.ResourceDir, "scripts", prepareScript))
	if err != nil {
		handleError(err)
		return
	}

	fmt.Fprintf(j.logFile, "[[microci-section]]Prepare git branch\n")
	err = j.ExecScript(script)
	if err != nil {
		handleError(err)
		return
	}

	// Run actual text-script
	trimmedPath := strings.TrimPrefix(strings.TrimPrefix(j.Script, j.Config.Scripts.Folder), "/")
	fmt.Fprintf(j.logFile, "[[microci-section]]Run %s\n", trimmedPath)
	script, err = filepath.Abs(j.Script)
	if err != nil {
		handleError(err)
		return
	}

	err = j.ExecScript(script)
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
