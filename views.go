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

var gOutput *Result

type ViewHandler struct {
	templates *template.Template
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

func execHandler(w http.ResponseWriter, r *http.Request) {

	if gOutput != nil && gOutput.Finished != true {
		fmt.Fprintf(w, "still running...")
		return
	}

	out, err := ExecScript(context.Background(), "/tmp/slowscript", struct{}{})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	gOutput = out
	fmt.Fprintf(w, "Started program, writing to file %s\n", out.OutputFile)
}

func (h *ViewHandler) viewHandler(w http.ResponseWriter, r *http.Request) {
	if gOutput == nil {
		fmt.Fprintf(w, "no process")
		return
	}

	vars := struct {
		Title    string
		JobTitle string
		Status   int
	}{}

	vars.JobTitle = gOutput.OutputFile

	if !gOutput.Finished {
		vars.Status = 3
	} else if gOutput.Error != nil {
		vars.Status = 2
	} else {
		vars.Status = 1
	}

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

	f, err := os.Open(gOutput.OutputFile)
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
	for !gOutput.Finished {
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
