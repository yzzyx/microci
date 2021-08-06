package main

import "time"

// Config includes all configuration variables
type Config struct {
	Server struct {
		Port    string `fig:"port" default:"80"`
		Address string `fig:"address" default:"http://micro.ci:8080/"`

		// Address/interface to bind to (defaults to empty)
		BindAddress string `fig:"bind_address" default:""`
	}

	Jobs struct {
		Folder           string        `fig:"folder" default:"jobs"`
		MaxExecutionTime time.Duration `fig:"max_execution_time" default:"10m"`
	}

	// Gitea specific settings
	Gitea struct {
		// The secret key is the same as is set up in the gitea webhook configuration
		SecretKey string `fig:"secret_key" validate:"required"`
		Username  string `fig:"username"`
		Password  string `fig:"password"`
		Token     string `fig:"token"`
		URL       string `fig:"url" validate:"required"`
	}
}
