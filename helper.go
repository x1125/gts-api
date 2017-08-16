package main

import (
	"net"
	"net/http"
	"strings"
	"net/url"
	"errors"
)

func numWaitingJobs() int {

	if len(runtime.queue) < 1 {
		return 0
	}

	waitingJobs := 0
	for _, jobEntry := range runtime.queue {
		if jobEntry.Status == QueueStatusWaiting {
			waitingJobs++
		}
	}

	return waitingJobs
}

func IPInUndoneJobs(ip string) bool {

	if len(runtime.queue) < 1 {
		return false
	}

	for _, jobEntry := range runtime.queue {
		if jobEntry.ip == ip && jobEntry.Status != QueueStatusDone {
			return true
		}
	}

	return false
}

func validateUrl(uri string) error {

	if len(uri) < 1 {
		return errors.New("missing url")
	}

	u, err := url.Parse(uri)
	if err != nil {
		return err
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("invalid url scheme")
	}

	if u.Hostname() == "localhost" {
		return errors.New("invalid hostname (localhost not allowed)")
	}

	if validIPAddressRegex.Match([]byte(u.Hostname())) {
		return errors.New("invalid hostname (ip address not allowed)")
	}

	if !validHostnameRegex.Match([]byte(u.Hostname())) {
		return errors.New("invalid hostname (not RFC 1123)")
	}

	// TODO: curl fetch, follow redirects, check for text/html mime

	ip, err := net.ResolveIPAddr("ip", u.Hostname())
	if err != nil {
		return err
	}

	if ipWithinPrivateRange(ip.IP) {
		return errors.New("invalid ip address (within private range)")
	}

	return nil
}

func ipWithinPrivateRange(ip net.IP) bool {

	privateIPRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	for _, privateIPRange := range privateIPRanges {
		_, cidrnet, err := net.ParseCIDR(privateIPRange)
		if err != nil {
			logger.Alert(err.Error())
			return true
		}

		if cidrnet.Contains(ip) {
			return true
		}
	}

	return false
}

func remoteAddr(r *http.Request) string {
	// TODO: this will break when working behind nginx e.g.
	return strings.Split(r.RemoteAddr, ":")[0]
}

func getJob(id int) (*Job, error) {

	runtime.mtx.Lock()
	defer runtime.mtx.Unlock()

	for _, jobEntry := range runtime.queue {
		if jobEntry.ID == id {
			return &jobEntry, nil
		}
	}

	return nil, errors.New("job not found")
}

func setJob(job Job) error {

	runtime.mtx.Lock()
	defer runtime.mtx.Unlock()

	for index, jobEntry := range runtime.queue {
		if jobEntry.ID == job.ID {
			runtime.queue[index] = job
			return nil
		}
	}

	return errors.New("job not found")
}