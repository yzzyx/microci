package main

import (
	"bufio"
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type ViewHandler struct {
	templates *template.Template
	output    *Output
	cancelJob func()
}

func NewViewHandler() (*ViewHandler, error) {
	templates, err := template.ParseGlob("templates/*")
	if err != nil {
		return nil, err
	}

	h := &ViewHandler{
		templates: templates,
	}
	return h, nil
}

func (h *ViewHandler) cancelHandler(w http.ResponseWriter, r *http.Request) {
	if h.cancelJob != nil && h.output != nil && !h.output.Status.IsFinished() {
		h.cancelJob()
	}

	http.Redirect(w, r, "/view", http.StatusFound)
}

func (h *ViewHandler) execHandler(w http.ResponseWriter, r *http.Request) {

	if h.output != nil && h.output.Status.IsFinished() != true {
		fmt.Fprintf(w, "still running...")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.cancelJob = cancel

	out, err := ExecScript(ctx, "/tmp/slowscript", struct{}{})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	h.output = out
	fmt.Fprintf(w, "Started program, writing to file %s\n", out.OutputFile)
}

func (h *ViewHandler) viewHandler(w http.ResponseWriter, r *http.Request) {
	if h.output == nil {
		fmt.Fprintf(w, "no process")
		return
	}

	vars := struct {
		Title    string
		JobTitle string
		Status   JobStatus
	}{}

	vars.JobTitle = h.output.OutputFile
	vars.Status = h.output.Status

	err := h.templates.ExecuteTemplate(w, "header.html", vars)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("cannot execute template: %+v", err)
		return
	}

	err = h.templates.ExecuteTemplate(w, "job.html", vars)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("cannot execute template: %+v", err)
		return
	}

	defer func() {
		err = h.templates.ExecuteTemplate(w, "footer.html", vars)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("cannot execute template: %+v", err)
			return
		}
	}()

	flush := func() {}
	if f, ok := w.(http.Flusher); ok {
		flush = func() {
			f.Flush()
		}
	}

	f, err := os.Open(h.output.OutputFile)
	if err != nil {
		fmt.Fprintf(w, "Cannot open: %s\n", err)
		return
	}

	rowformat := `<div><div class="num">%d</div><span>%s</span></div>`
	stderrFormat := `<div><div class="num">%d</div><span class="error">%s</span></div>`

	scanner := bufio.NewReader(f)
	line := 1
	currentLineContents := "" // Keep track of unfinished lines

	scanLine := func() {
		for {
			str, err := scanner.ReadString('\n')
			if err != nil {
				currentLineContents += str
				break
			}

			contents := currentLineContents + str
			if strings.HasPrefix(contents, "[[stderr]]") {
				contents = strings.TrimPrefix(contents, "[[stderr]]")
				fmt.Fprintf(w, stderrFormat, line, contents)
			} else {
				fmt.Fprintf(w, rowformat, line, contents)
			}
			currentLineContents = ""
			line++
		}
	}

	scanLine()
	for !h.output.Status.IsFinished() {
		flush()
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
			break
		}

		prevLine := line
		scanLine()

		if line > prevLine {
			flush()
		}
	}

}
