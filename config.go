package main

// Config includes all configuration variables
type Config struct {
	// The secret key is the same as is set up in the gitea webhook configuration
	SecretKey string
	Username  string
	Password  string
	Token     string
	URL       string
	Port      string
	Address   string
}
