package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	gitea "github.com/yzzyx/gitea-webhook"
)

// Manager keeps track of all CI workers
type Manager struct {
	api *gitea.API
	cfg *Config
	url *url.URL // URL of microci server

	workerCh chan *Job

	repos      []*Repository
	reposMutex *sync.Mutex
	jobs       map[string]*Job
	jobsMutex  *sync.RWMutex
}

func NewManager(cfg *Config) (*Manager, error) {
	u, err := url.Parse(cfg.Server.Address)
	if err != nil {
		return nil, fmt.Errorf("could not parse URL in setting 'server.address': %w", err)
	}

	m := &Manager{
		jobsMutex: &sync.RWMutex{},
		jobs:      map[string]*Job{},
		cfg:       cfg,
		url:       u,
		api: &gitea.API{
			URL:      cfg.Gitea.URL,
			Token:    cfg.Gitea.Token,
			Username: cfg.Gitea.Username,
			Password: cfg.Gitea.Password,
		},
	}

	m.workerCh = make(chan *Job)

	if cfg.Jobs.Workers <= 0 {
		return nil, fmt.Errorf("invalid number of workers (%d), must be atleast one", cfg.Jobs.Workers)
	}

	// Start workers
	for i := 0; i < cfg.Jobs.Workers; i++ {
		go NewWorker(m.workerCh)
	}

	return m, nil
}

func (m *Manager) Shutdown() {
	close(m.workerCh)

	m.jobsMutex.Lock()
	defer m.jobsMutex.Unlock()

	// Cancel active jobs
	for _, j := range m.jobs {
		if j.ctxCancel != nil {
			j.ctxCancel()
		}
	}
}

func (m *Manager) GetRepo(name string) *Repository {
	m.reposMutex.Lock()
	defer m.reposMutex.Unlock()

	for k := range m.repos {
		if m.repos[k].Name == name {
			return m.repos[k]
		}
	}

	repo := &Repository{
		Name:   name,
		Queues: nil,
	}

	m.repos = append(m.repos, repo)
	return repo
}

// WebhookEvent is called when a webhook has successfully been authenticated
func (m *Manager) WebhookEvent(typ gitea.EventType, ev gitea.Event, responseWriter http.ResponseWriter, r *http.Request) {
	var scriptName string
	var branchName string

	job := &Job{
		Type:      typ,
		Event:     ev,
		API:       m.api,
		config:    m.cfg,
		Context:   m.cfg.Jobs.DefaultContext,
		TargetURL: m.url.String(),
	}

	// Default script is 'default.sh'.
	// If a script is specified in the webhook URL as a parameter,
	// we will try to use that instead.
	scriptName = "default.sh"
	if s := r.URL.Query().Get("script"); s != "" {
		scriptName = s
	}

	// Set the context to report back to gitea
	if s := r.URL.Query().Get("context"); s != "" {
		job.Context = s
	}

	var queueName string
	switch typ {
	case gitea.EventTypePush:
		branchName = strings.TrimPrefix(ev.Ref, "refs/heads/")
		queueName = branchName
		job.CommitRepo = ev.Repository.FullName
		job.CommitID = ev.After
	case gitea.EventTypePullRequest:
		branchName = ev.PullRequest.Base.Ref
		queueName = fmt.Sprintf("PR #%d", ev.PullRequest.ID)
		job.CommitRepo = ev.PullRequest.Base.Repo.FullName
		job.CommitID = ev.PullRequest.Head.SHA
	default:
		return
	}

	if !isDir(job.CommitRepo) {
		log.Printf("ignoring repositoriy '%s' - is not a directory", ev.Repository.FullName)
		return
	}

	repo := m.GetRepo(job.CommitRepo)
	q := repo.GetQueue(queueName, job.Context)

	// Try to find the most specific version of the script available in the following order
	//  - Branch-specific scripts
	//  - Repository-wide scripts
	//  - Global scripts (in folder "global")
	scripts := []string{
		filepath.Join(ev.Repository.FullName, branchName, scriptName),
		filepath.Join(ev.Repository.FullName, scriptName),
		filepath.Join("global", scriptName),
	}

	for _, script := range scripts {
		if !isFile(script) {
			continue
		}

		job.Script = script
		err := job.Setup()
		if err != nil {
			log.Printf("SetupJob: %+v", err)
			responseWriter.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(responseWriter, "Could not process webhook: %+v", err)
			return
		}

		m.jobsMutex.Lock()
		m.jobs[job.ID] = job
		m.jobsMutex.Unlock()

		// Add job to manager queue
		m.workerCh <- job
		break
	}
}

// GetJob returns a Job structure, either from memory if it exists, or recreated from disk
func (m *Manager) GetJob(id string) (*Job, error) {
	// First, check if we have it in memory
	job := func() *Job {
		m.jobsMutex.RLock()
		defer m.jobsMutex.RUnlock()
		return m.jobs[id]
	}()
	if job != nil {
		return job, nil
	}

	// Otherwise, recreate job from disk
	jobPath := filepath.Join(m.cfg.Jobs.Folder, id)
	st, err := os.Stat(jobPath)
	if err != nil {
		return nil, err
	}

	if !st.IsDir() {
		return nil, fmt.Errorf("expected '%s' to be a directory", jobPath)
	}

	f, err := os.Open(filepath.Join(jobPath, "info.json"))
	if err != nil {
		return nil, err
	}

	job = &Job{}
	err = json.NewDecoder(f).Decode(&job)
	if err != nil {
		return nil, err
	}

	job.ID = id
	job.Folder = jobPath

	// If job is still in status pending, it means that the
	// server was killed before it finished, so we'll consider it cancelled
	if job.Status == StatusPending {
		job.Status = StatusCancelled
	}

	// Save in memory for later
	m.jobsMutex.Lock()
	defer m.jobsMutex.Unlock()
	m.jobs[id] = job
	return job, nil
}
