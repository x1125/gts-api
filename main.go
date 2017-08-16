package main

import (
	"errors"
	"flag"
	"log"
	"log/syslog"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"github.com/rs/cors"
)

var (
	listenAddr = "127.0.0.1"
	listenPort = "12810"
)

const (
	maxQueue                = 10
	maxKeepFailedJobsTime   = 5 // in seconds
	maxKeepFinishedJobsTime = 60 // in seconds
	maxResolutionWidth      = 1920
	maxResolutionHeight     = 1200
)

const (
	QueueStatusWaiting = iota
	QueueStatusRunning
	QueueStatusDone
)

const (
	BrowserTypeNone = iota
	BrowserTypeFirefox
	BrowserTypeChromium
)

const (
	BrowserFixDisabled = iota
	BrowserFixEnabled
)

const (
	PageresFormatNone = iota
	PageresFormatPng
	PageresFormatJpg
)

type Runtime struct {
	runningID int
	queue     []Job
	mtx       *sync.Mutex
}

type Job struct {
	ID  int    `json:"id"`
	ip  string `json:"-"`
	URL string `json:"url"`

	Status   int   `json:"status"`
	Created  int64 `json:"created"`
	Started  int64 `json:"started"`
	Finished int64 `json:"finished"`

	Error    bool     `json:"error"`
	Messages []string `json:"messages"`

	BrowserType   int `json:"browser_type"`
	BrowserFix    int `json:"browser_fix"`
	PageresFormat int `json:"pageres_format"`
}

var (
	logger  *syslog.Writer
	runtime Runtime

	// https://stackoverflow.com/a/106223
	validIPAddressRegex = regexp.MustCompile(`^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])$`)
	validHostnameRegex  = regexp.MustCompile(`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])$`)
)

func main() {

	flag.StringVar(&listenAddr, "listenAddr", listenAddr, "")
	flag.StringVar(&listenPort, "listenPort", listenPort, "")

	flag.Parse()

	var err error
	// use "journalctl -f -t gts" to view logs
	logger, err = syslog.New(syslog.LOG_DEBUG, "gts")

	// init runtime
	runtime = Runtime{
		queue: make([]Job, 0),
		mtx:   &sync.Mutex{},
	}

	// check for required docker images
	out, err := exec.Command("docker", "images").Output()
	if err != nil {
		log.Fatal(`Error running "docker images": ` + err.Error() + " (Docker daemon not running?)")
	}
	if !strings.Contains(string(out), "gts-browser") {
		log.Fatal(errors.New("missing gts-browser docker image"))
	}
	if !strings.Contains(string(out), "gts-pageres") {
		log.Fatal(errors.New("missing gts-pageres docker image"))
	}

	// start background worker
	go backgroundWorker()

	// define routes
	mux := http.NewServeMux()
	mux.Handle("/status/queue", restHandler(statusQueueHandler))
	mux.Handle("/status/job", restHandler(statusJobHandler))
	mux.Handle("/job/new", restHandler(jobNewHandler))
	mux.HandleFunc("/job/download", jobDownloadHandler)

	// using CORS
	handler := cors.Default().Handler(mux)

	// start webserver
	err = http.ListenAndServe(listenAddr+":"+listenPort, handler)
	if err != nil {
		log.Fatal(err)
	}
}
