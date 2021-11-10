package main

import (
	"bufio"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/yzzyx/microci/ansi"
	"github.com/yzzyx/microci/config"
	"github.com/yzzyx/microci/job"
)

var errNotFound = errors.New("not found")

// View is the base structure for views
type View struct {
	cfg       *config.Config
	templates *template.Template
	manager   *Manager
}

// NewViewHandler returns a new View-handler based on the supplied config and manager
func NewViewHandler(cfg *config.Config, manager *Manager) (*View, error) {
	templates, err := template.ParseGlob(filepath.Join(cfg.ResourceDir, "templates/*"))
	if err != nil {
		return nil, err
	}

	h := &View{
		cfg:       cfg,
		templates: templates,
		manager:   manager,
	}
	return h, nil
}

// ViewWrapper wraps a view and handles errors returned by views
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

	job, err := v.manager.GetJob(id)
	if err != nil {
		return err
	}

	job.Cancel()
	http.Redirect(w, r, "/job/"+id, http.StatusFound)
	return nil
}

// GetArtifact returns a specific artifact from a job
func (v *View) GetArtifact(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	name := chi.URLParam(r, "name")

	job, err := v.manager.GetJob(id)
	if err != nil {
		return err
	}

	f, err := os.Open(filepath.Join(job.Folder, "artifacts", name))
	if err != nil {
		return err
	}

	mimetype := mime.TypeByExtension(name)
	if mimetype != "" {
		w.Header().Add("Content-type", mimetype)
	}

	_, err = io.Copy(w, f)
	if err != nil {
		return err
	}
	return nil
}

// GetJob handles all requests to "/job/{id}"
func (v *View) GetJob(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")

	vars := struct {
		Title     string
		Job       *job.Job
		URL       *url.URL
		Artifacts []os.FileInfo
	}{
		URL: r.URL,
	}

	j, err := v.manager.GetJob(id)
	if err != nil {
		return err
	}

	vars.Title = fmt.Sprintf("j %s", id)
	vars.Job = j

	artifactFolder, err := os.Open(filepath.Join(j.Folder, "artifacts"))
	if err == nil {
		defer artifactFolder.Close()
		vars.Artifacts, err = artifactFolder.Readdir(-1)
		if err != nil {
			return err
		}
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

	if j.Status == job.StatusPending {
		return nil
	}

	f, err := os.Open(filepath.Join(j.Folder, "logs"))
	if err != nil {
		return err
	}

	sectionStart := `<div class="section">
	<input id="section-toggle-%[1]d" type=checkbox class="section-toggle"%[3]s>
	<label for="section-toggle-%[1]d" class="section-label">%[2]s</label>
	<div class="section-contents">`
	sectionEnd := `</div></div>`

	rowFormat := `<div><div class="num">%d</div><span>%s</span></div>`
	stderrFormat := `<div><div class="num">%d</div><span class="error">%s</span></div>`

	scanner := bufio.NewReader(f)
	line := 1
	currentLineContents := "" // Keep track of unfinished lines

	escapeTags := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	sectionId := 1
	printLine := func(s string) {
		if s == "" {
			return
		}

		s = strings.TrimRight(s, "\n\r")
		s = escapeTags.Replace(s)
		if strings.HasPrefix(s, "[[microci-section]]") {
			s = strings.TrimPrefix(s, "[[microci-section]]")

			// We do not check the "git-setup" section, since it's
			// usually not of interest.
			checked := ""
			if sectionId > 1 {
				fmt.Fprintf(w, sectionEnd)
				checked = " checked"
			}
			fmt.Fprintf(w, sectionStart, sectionId, s, checked)
			sectionId++
			line = 0 // reset line-counter
		} else if strings.HasPrefix(s, "[[stderr]]") {
			s = strings.TrimPrefix(s, "[[stderr]]")
			fmt.Fprintf(w, stderrFormat, line, ansi.ToHTML(s))
		} else {
			fmt.Fprintf(w, rowFormat, line, ansi.ToHTML(s))
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
	for !j.Status.IsFinished() {
		flush()
		select {
		case <-r.Context().Done():
			return nil
		case <-time.After(500 * time.Millisecond):
			break
		}

		prevLine := line
		scanLine()

		if line != prevLine {
			flush()
		}
	}

	// Print last line, if it does not end in newline
	printLine(currentLineContents)

	// Close the current section
	fmt.Fprintf(w, sectionEnd)
	return nil
}
