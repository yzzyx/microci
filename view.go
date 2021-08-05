package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi"
)

type View struct {
	cfg *Config
}

// Job handles all requests to "/job/{id}"
func (v *View) GetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	jobPath := filepath.Join(v.cfg.Folders.Jobs, id)
	st, err := os.Stat(jobPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("could not stat directory: %v", err)
		return
	}

	if !st.IsDir() {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("expected '%s' to be a directory", jobPath)
		return
	}

}
