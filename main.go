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

	secretKey := os.Getenv("MICROCI_GITEA_SECRETKEY")
	username := os.Getenv("MICROCI_GITEA_USERNAME")
	password := os.Getenv("MICROCI_GITEA_PASSWORD")
	token := os.Getenv("MICROCI_GITEA_TOKEN")
	url := os.Getenv("MICROCI_GITEA_URL")
	port := os.Getenv("MICROCI_PORT")
	address := os.Getenv("MICROCI_ADDRESS")

	if secretKey == "" ||
		(username == "" && token == "") ||
		url == "" {
		usage()
		os.Exit(1)
	}

	if port == "" {
		port = "80"
	}

	// The secret key is the same as is set up in the gitea webhook configuration
	api := &gitea.API{
		URL:      url,
		Token:    token,
		Username: username,
		Password: password,
	}

	// onSuccess will be called if a request to /webhook has been successfully validated
	// Expect requests to be made to "/webhook"
	http.HandleFunc("/webhook", gitea.Handler(secretKey, onSuccess(api)))

	h, err := NewViewHandler()
	if err != nil {
		log.Printf("Cannot initialize viewhandler: %+v", err)
		return
	}

	http.HandleFunc("/exec", execHandler)
	http.HandleFunc("/view", h.viewHandler)
	http.HandleFunc("/cancel", h.cancelHandler)

	server := http.Server{
		Addr: fmt.Sprintf("%s:%s", address, port),
	}

	go func() {
		log.Printf("Listening to requests on: http://%s:%s/webhook", address, port)
		err := server.ListenAndServe()
		if err != nil {
			log.Printf("ListenAndServe: %+v", err)
		}
		cancel()
	}()

	select {
	case <-ctx.Done():
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = server.Shutdown(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error shutting down server:", err)
	}
}
