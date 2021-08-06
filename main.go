package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/kkyr/fig"
	gitea "github.com/yzzyx/gitea-webhook"
)

func isFile(p string) bool {
	st, err := os.Stat(p)
	if err != nil {
		return false
	}

	return !st.IsDir()

}

func isDir(p string) bool {
	st, err := os.Stat(p)
	if err != nil {
		return false
	}

	return st.IsDir()
}

func main() {
	ctx := context.Background()

	// trap Ctrl+C and call cancel on the context
	ctx, cancel := context.WithCancel(ctx)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	defer func() {
		signal.Stop(c)
		cancel()
	}()
	go func() {
		select {
		case <-c:
			cancel()
		case <-ctx.Done():
		}
	}()

	config := Config{}
	err := fig.Load(&config,
		fig.File("config.yaml"),
		fig.UseEnv("MICROCI"),
		fig.Dirs("."))

	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not load config: %v\n", err)
		os.Exit(1)
	}

	if config.Gitea.Username == "" && config.Gitea.Token == "" {
		fmt.Fprintf(os.Stderr, "One of 'gitea.username' or 'gitea.token' must be specified in config\n")
		os.Exit(1)

	}
	if config.Gitea.URL == "" {
		usage()
		os.Exit(1)
	}

	worker := Worker{
		jobsMutex: &sync.RWMutex{},
		jobs:      map[string]*Job{},
		cfg:       &config,
		api: &gitea.API{
			URL:      config.Gitea.URL,
			Token:    config.Gitea.Token,
			Username: config.Gitea.Username,
			Password: config.Gitea.Password,
		},
	}

	view, err := NewViewHandler(&config, &worker)
	if err != nil {
		log.Printf("Cannot initialize viewhandler: %+v", err)
		return
	}

	router := chi.NewRouter()
	router.Use(middleware.Logger)

	// WebhookEvent will be called if a request to /webhook/gitea has been successfully validated
	router.Handle("/webhook/gitea", gitea.Handler(config.Gitea.SecretKey, worker.WebhookEvent))
	router.Get("/job/{id}", ViewWrapper(view.GetJob))
	router.Get("/job/{id}/cancel", ViewWrapper(view.CancelJob))

	server := http.Server{
		Handler: router,
		Addr:    fmt.Sprintf("%s:%s", config.Server.BindAddress, config.Server.Port),
	}

	go func() {
		log.Printf("Listening to requests on %s:%s", config.Server.BindAddress, config.Server.Port)
		err := server.ListenAndServe()
		if err != nil {
			log.Printf("ListenAndServe: %+v", err)
		}
		cancel()
	}()

	<-ctx.Done()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = server.Shutdown(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error shutting down server:", err)
	}
}
