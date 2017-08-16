package main

import (
	"time"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"errors"
	"archive/zip"
	"path/filepath"
	"io"
)

func backgroundWorker() {

	// infinite loop
	for {

		// wait on second
		time.Sleep(time.Second)

		// remove old jobs
		cleanJobs()

		// check if already working
		if runtime.runningID > 0 {
			continue
		}

		// check for waiting jobs
		if numWaitingJobs() < 1 {
			continue
		}

		// get waiting Job
		for _, jobEntry := range runtime.queue {
			if jobEntry.Status == QueueStatusWaiting {
				startJob(jobEntry)
				break
			}
		}
	}
}

func cleanJobs() {

	runtime.mtx.Lock()
	defer runtime.mtx.Unlock()

	newList := make([]Job, 0)
	for _, jobEntry := range runtime.queue {

		// remove failed jobs
		if jobEntry.Error && jobEntry.Finished > 0 && jobEntry.Finished < (time.Now().Unix()-int64(maxKeepFailedJobsTime)) {
			logger.Debug(fmt.Sprintf("removed bad job id %d (cleanup)", jobEntry.ID))
			continue
		}

		// remove finished jobs
		if jobEntry.Finished > 0 && jobEntry.Finished < (time.Now().Unix()-int64(maxKeepFinishedJobsTime)) {

			// remove zip file
			zipFile := fmt.Sprintf("%s/%d.zip", os.TempDir(), jobEntry.ID)
			err := os.Remove(zipFile)
			if err != nil {
				logger.Err(err.Error())
			}

			logger.Debug(fmt.Sprintf("removed good job id %d (cleanup)", jobEntry.ID))
			continue
		}

		newList = append(newList, jobEntry)
	}

	if len(runtime.queue) == len(newList) {
		return
	}

	runtime.queue = newList
}

func startJob(job Job) {

	runtime.mtx.Lock()
	runtime.runningID = job.ID
	runtime.mtx.Unlock()

	job.Status = QueueStatusRunning
	job.Started = time.Now().Unix()
	err := setJob(job)
	if err != nil {
		logger.Err(err.Error())
	}

	err = runJob(job)
	if err != nil {
		job.Error = true
		job.Messages = append(job.Messages, err.Error())
	}

	job.Status = QueueStatusDone
	job.Finished = time.Now().Unix()
	err = setJob(job)
	if err != nil {
		logger.Err(err.Error())
	}

	logger.Debug(fmt.Sprintf("finished job id %d", job.ID))

	runtime.mtx.Lock()
	runtime.runningID = 0
	runtime.mtx.Unlock()
}

func runJob(job Job) error {

	targetPath := os.TempDir() + "/gts_output"
	actionCounter := 0 // used to summarize the "x out of x failed"

	// check for temporary folder
	if _, err := os.Stat(targetPath); err == nil {

		// remove it anyways
		err = os.RemoveAll(targetPath)
		if err != nil {
			return err
		}
	}

	// create the temporary folder
	err := os.Mkdir(targetPath, 0755)
	if err != nil {
		return err
	}

	if job.BrowserType != BrowserTypeNone {

		actionCounter++
		err = runBrowserSaveAs(job, targetPath)
		if err != nil {
			job.Error = true
			job.Messages = append(job.Messages, "unable to save page as browser")
			setJob(job)
			logger.Err(err.Error())
		}
	}

	if job.PageresFormat != PageresFormatNone {

		actionCounter++
		err = runPageres(job, targetPath)
		if err != nil {
			job.Error = true
			job.Messages = append(job.Messages, "unable to create screenshot")
			setJob(job)
			logger.Err(err.Error())
		}
	}

	if len(job.Messages) == actionCounter {
		// all actions failed, no need to proceed
		return errors.New("all actions failed")
	}

	err = runCompression(job, targetPath)
	if err != nil {
		return err
	}

	return nil
}

func runBrowserSaveAs(job Job, targetPath string) error {

	var browser = "chromium"
	if job.BrowserType == BrowserTypeFirefox {
		browser = "firefox"
	}

	out, err := exec.Command(
		"docker",
		"run",
		"--rm",
		"-v",
		targetPath + ":/output",
		"--cap-add",
		"SYS_ADMIN",
		"gts-browser",
		"sh",
		"/opt/run.sh",
		job.URL,
		browser,
	).CombinedOutput()
	comOut := string(out)
	if err != nil {
		logger.Err(comOut)
		return err
	}

	if !strings.Contains(comOut, "Saving web page") || !strings.Contains(comOut, "Done") {
		return errors.New("unable to saving page")
	}

	return nil
}

func runPageres(job Job, targetPath string) error {

	out, err := exec.Command(
		"docker",
		"run",
		"--rm",
		"-v",
		targetPath + ":/output",
		"gts-pageres",
		"sh",
		"-c",
		`cd /output && pageres ` + job.URL + ` 1920x1080`,
	).CombinedOutput()
	comOut := string(out)
	if err != nil {
		logger.Debug(comOut)
		return err
	}

	if !strings.Contains(comOut, "Generated 1 screenshot") {
		return errors.New("unable to create screenshot")
	}

	return nil
}

func runCompression(job Job, targetPath string) error {

	zipFile := fmt.Sprintf("%s/%d.zip", os.TempDir(), job.ID)
	return zipit(targetPath, zipFile)
}

// http://blog.ralch.com/tutorial/golang-working-with-zip/
func zipit(source, target string) error {
	zipFile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	info, err := os.Stat(source)
	if err != nil {
		return nil
	}

	var baseDir string
	if info.IsDir() {
		baseDir = filepath.Base(source)
	}

	filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		if strings.TrimPrefix(path, source) == "" {
			return nil
		}

		if baseDir != "" {
			header.Name = strings.TrimPrefix(path, source + "/")
		}

		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, file)
		return err
	})

	return err
}