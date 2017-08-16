package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"os"
	"io"
)

func statusQueueHandler(w http.ResponseWriter, r *http.Request) (interface{}, error) {

	var queue = numWaitingJobs()
	if runtime.runningID > 0 {
		queue++
	}

	return struct {
		Queue                   int `json:"queue"`
		MaxQueue                int `json:"max_queue"`
		MaxKeepFailedJobsTime   int `json:"max_keep_failed_jobs_time"`
		MaxKeepFinishedJobsTime int `json:"max_keep_finished_jobs_time"`
	}{
		queue,
		maxQueue,
		maxKeepFailedJobsTime,
		maxKeepFinishedJobsTime,
	}, nil
}

func statusJobHandler(w http.ResponseWriter, r *http.Request) (interface{}, error) {

	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	id, err := strconv.Atoi(r.Form.Get("id"))
	if err != nil {
		return nil, err
	}

	if id < 1 {
		return nil, errors.New("missing id")
	}

	job := Job{}
	for _, jobEntry := range runtime.queue {
		if jobEntry.ID == id {
			job = jobEntry
			break
		}
	}

	if job.ID < 1 {
		return nil, errors.New("job id not found")
	}

	return job, nil
}

func jobNewHandler(w http.ResponseWriter, r *http.Request) (interface{}, error) {

	// generate unique key (TODO: look for better method)
	id := time.Now().Nanosecond()

	if err := r.ParseForm(); err != nil {
		return nil, err
	}

	browserType, _ := strconv.Atoi(r.Form.Get("browser_type"))
	if browserType != BrowserTypeNone && browserType != BrowserTypeFirefox && browserType != BrowserTypeChromium {
		return nil, errors.New("invalid browser_type")
	}

	browserFix, _ := strconv.Atoi(r.Form.Get("browser_fix"))
	if browserFix != BrowserFixDisabled && browserFix != BrowserFixEnabled {
		return nil, errors.New("invalid browser_fix")
	}

	pageresFormat, _ := strconv.Atoi(r.Form.Get("pageres_format"))
	if pageresFormat != PageresFormatNone && pageresFormat != PageresFormatPng && pageresFormat != PageresFormatJpg {
		return nil, errors.New("invalid pageres_format")
	}

	if browserType == BrowserTypeNone && pageresFormat == PageresFormatNone {
		return nil, errors.New("neither browser nor pageres chosen; nothing to do")
	}

	uri := r.Form.Get("url")
	if err := validateUrl(uri); err != nil {
		return nil, err
	}

	runtime.mtx.Lock()
	defer runtime.mtx.Unlock()

	if numWaitingJobs() >= maxQueue {
		return nil, errors.New(fmt.Sprintf("queue limit (%d) was reached", maxQueue))
	}

	if IPInUndoneJobs(remoteAddr(r)) {
		return nil, errors.New(fmt.Sprintf("only one concurrent job per ip address allowed"))
	}

	runtime.queue = append(runtime.queue, Job{
		ID:      id,
		ip:      remoteAddr(r),
		URL:     uri,
		Status:  QueueStatusWaiting,
		Created: time.Now().Unix(),

		BrowserType:   browserType,
		BrowserFix:    browserFix,
		PageresFormat: pageresFormat,

		Messages: make([]string, 0),
	})

	logger.Debug(fmt.Sprintf("added job id %d", id))

	return struct {
		Session int `json:"session"`
	}{
		id,
	}, nil
}

func jobDownloadHandler(w http.ResponseWriter, r *http.Request) {

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, err := strconv.Atoi(r.Form.Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if id < 1 {
		http.Error(w, "missing id", http.StatusInternalServerError)
		return
	}

	job := Job{}
	for _, jobEntry := range runtime.queue {
		if jobEntry.ID == id {
			job = jobEntry
			break
		}
	}

	if job.ID < 1 {
		http.Error(w, "job id not found", http.StatusInternalServerError)
		return
	}

	if job.Finished <= (time.Now().Unix() - maxKeepFinishedJobsTime) {
		http.Error(w, "job expired", http.StatusInternalServerError)
		return
	}

	zipFile := fmt.Sprintf("%s/%d.zip", os.TempDir(), job.ID)
	file, err := os.Open(zipFile)
	if err != nil {
		logger.Err(err.Error())
		http.Error(w, "job got lost; try again", http.StatusInternalServerError)
		return
	}
	fileStat, err := os.Stat(zipFile)
	if err != nil {
		logger.Err(err.Error())
		http.Error(w, "job got lost; try again", http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Description", "File Transfer")
	w.Header().Add("Content-Transfer-Encoding", "binary")
	w.Header().Add("Content-Disposition", "attachment; filename=" + fmt.Sprintf("%d.zip", job.ID))
	w.Header().Add("Content-Type", "application/zip")
	w.Header().Add("Content-Length", fmt.Sprintf("%d", fileStat.Size()))
	io.Copy(w, file)
}
