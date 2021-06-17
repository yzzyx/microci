package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

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

	config := Config{
		SecretKey: os.Getenv("MICROCI_GITEA_SECRETKEY"),
		Username:  os.Getenv("MICROCI_GITEA_USERNAME"),
		Password:  os.Getenv("MICROCI_GITEA_PASSWORD"),
		Token:     os.Getenv("MICROCI_GITEA_TOKEN"),
		URL:       os.Getenv("MICROCI_GITEA_URL"),
		Port:      os.Getenv("MICROCI_PORT"),
		Address:   os.Getenv("MICROCI_ADDRESS"),
	}

	if config.SecretKey == "" ||
		(config.Username == "" && config.Token == "") ||
		config.URL == "" {
		usage()
		os.Exit(1)
	}

	if config.Port == "" {
		config.Port = "80"
	}

	worker := Worker{
		cfg: &config,
		api: &gitea.API{
			URL:      config.URL,
			Token:    config.Token,
			Username: config.Username,
			Password: config.Password,
		},
	}

	// onSuccess will be called if a request to /webhook has been successfully validated
	// Expect requests to be made to "/webhook"
	http.HandleFunc("/webhook", gitea.Handler(config.SecretKey, worker.onSuccess))

	server := http.Server{
		Addr: fmt.Sprintf("%s:%s", config.Address, config.Port),
	}

	go func() {
		log.Printf("Listening to requests on: http://%s:%s/webhook", config.Address, config.Port)
		err := server.ListenAndServe()
		if err != nil {
			log.Printf("ListenAndServe: %+v", err)
		}
		cancel()
	}()

	<-ctx.Done()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := server.Shutdown(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error shutting down server:", err)
	}
}
