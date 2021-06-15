package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	gitea "github.com/yzzyx/gitea-webhook"
)

func onSuccess(api *gitea.API) func(typ gitea.EventType, ev gitea.Event, w http.ResponseWriter, r *http.Request) {
	fn := func(typ gitea.EventType, ev gitea.Event, w http.ResponseWriter, r *http.Request) {
		var scriptName string
		var branchName string
		var commitRepo string
		var commitID string

		switch typ {
		case gitea.EventTypePush:
			scriptName = "push.sh"
			branchName = strings.TrimPrefix(ev.Ref, "refs/heads/")
			commitRepo = ev.Repository.FullName
			commitID = ev.After
		case gitea.EventTypePullRequest:
			scriptName = "pull-request.sh"
			branchName = ev.PullRequest.Base.Ref
			commitRepo = ev.PullRequest.Head.Repo.FullName
			commitID = ev.PullRequest.Head.SHA
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
			status := gitea.CreateStatusOption{
				Context:     "",
				Description: "test?",
				State:       gitea.CommitStatusPending,
				TargetURL:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			}

			err := api.UpdateCommitState(commitRepo, commitID, status)
			if err != nil {
				log.Printf("UpcateCommitState(%s) retured error: %+v\n", status.State, err)
			}

			// TODO: keep track of context
			err = ExecScript(context.Background(), script, ev)
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

			err = api.UpdateCommitState(commitRepo, commitID, status)
			if err != nil {
				log.Printf("UpcateCommitState(%s) retured error: %+v\n", status.State, err)
			}
		}
	}
	return fn
}
