package main

import (
	"bufio"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi"
)

var errNotFound = errors.New("not found")

type View struct {
	cfg       *Config
	templates *template.Template
	worker    *Worker
}

func NewViewHandler(cfg *Config, worker *Worker) (*View, error) {
	templates, err := template.ParseGlob("templates/*")
	if err != nil {
		return nil, err
	}

	h := &View{
		cfg:       cfg,
		templates: templates,
		worker:    worker,
	}
	return h, nil
}

func ViewWrapper(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err != nil {
			if errors.Is(err, errNotFound) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			log.Printf("cannot execute view: %+v", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

// CancelJob aborts a specific job
func (v *View) CancelJob(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")

	job, err := v.worker.GetJob(id)
	if err != nil {
		return err
	}

	if job.ctxCancel != nil {
		job.ctxCancel()
	}

	http.Redirect(w, r, "/job/"+id, http.StatusFound)
	return nil
}

// GetJob handles all requests to "/job/{id}"
func (v *View) GetJob(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")

	vars := struct {
		Title    string
		JobTitle string
		Status   int
	}{}

	job, err := v.worker.GetJob(id)
	if err != nil {
		return err
	}

	vars.Title = fmt.Sprintf("job %s", id)
	vars.JobTitle = id

	if job.Result == nil {
		return errors.New("Result is nil")
	}
	if !job.Result.Finished {
		vars.Status = 3
	} else if job.Result.Error != nil {
		vars.Status = 2
	} else {
		vars.Status = 1
	}

	err = v.templates.ExecuteTemplate(w, "job.html", vars)
	if err != nil {
		return err
	}

	// Job does not contain footer, because we might not have all data available
	defer func() {
		v.templates.ExecuteTemplate(w, "footer.html", vars)
	}()

	flush := func() {}
	if f, ok := w.(http.Flusher); ok {
		flush = func() {
			f.Flush()
		}
	}

	f, err := os.Open(filepath.Join(job.Folder, "logs"))
	if err != nil {
		return err
	}

	rowFormat := `<div><div class="num">%d</div><span>%s</span></div>`
	stderrFormat := `<div><div class="num">%d</div><span class="error">%s</span></div>`

	scanner := bufio.NewReader(f)
	line := 1
	currentLineContents := "" // Keep track of unfinished lines

	printLine := func(s string) {
		if s == "" {
			return
		}

		if strings.HasPrefix(s, "[[stderr]]") {
			s = strings.TrimPrefix(s, "[[stderr]]")
			fmt.Fprintf(w, stderrFormat, line, s)
		} else {
			fmt.Fprintf(w, rowFormat, line, s)
		}

	}

	// Parse output line by line, so that we can present it nicely
	scanLine := func() {
		for {
			str, err := scanner.ReadString('\n')
			if err != nil {
				currentLineContents += str
				break
			}

			printLine(currentLineContents + str)
			currentLineContents = ""
			line++
		}
	}

	scanLine()

	// If process is still running, keep reading from output file
	for !job.Result.Finished {
		flush()
		select {
		case <-r.Context().Done():
			return nil
		case <-time.After(500 * time.Millisecond):
			break
		}

		prevLine := line
		scanLine()

		if line > prevLine {
			flush()
		}
	}

	// Print last line, if it does not end in newline
	printLine(currentLineContents)
	return nil
}
