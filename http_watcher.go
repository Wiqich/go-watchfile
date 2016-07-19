package watchfile

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	gmt, _ = time.LoadLocation("GMT")

	errNoGMT           = errors.New("check http modified time without GMT timezone support")
	errNotModified     = errors.New("not modified")
	errNoRemoteOrLocal = errors.New("empty remote url or local path")
)

type HTTPWatcher struct {
	mutex    sync.Mutex
	remote   string
	local    string
	perm     os.FileMode
	interval time.Duration
	callback func(error)
	running  bool
	stop     chan time.Time
	options  Option
	modTime  time.Time
	checksum [md5.Size]byte
	etag     string
	client   *http.Client
}

func NewHTTPWatcher(remote, local string, interval time.Duration, perm os.FileMode, callback func(error), options Option) (*HTTPWatcher, error) {
	if options.CheckModTime() && gmt == nil {
		return nil, errNoGMT
	}
	if remote == "" || local == "" {
		return nil, errNoRemoteOrLocal
	}
	if callback == nil {
		return nil, errNoCallback
	}
	if interval < time.Minute {
		interval = time.Minute
	}
	return &HTTPWatcher{
		remote:   remote,
		local:    local,
		perm:     perm,
		options:  options,
		interval: interval,
		callback: callback,
		client:   new(http.Client),
	}, nil
}

func (w *HTTPWatcher) EnsureLocal() error {
	if _, err := os.Stat(w.local); err == nil {
		if w.options.CheckETag() {
			path := filepath.Join(filepath.Dir(w.local), fmt.Sprintf(".%s.etag", filepath.Base(w.local)))
			if content, err := ioutil.ReadFile(path); err == nil {
				w.etag = string(content)
			}
		}
		return nil
	}
	return w.download()
}

func (w *HTTPWatcher) Start() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.modTime.IsZero() {
		if err := w.EnsureLocal(); err != nil {
			return err
		}
	}
	if w.running {
		return errRunning
	}
	go w.watchHTTP()
	return nil
}

func (w *HTTPWatcher) watchHTTP() {
	defer func() { w.running = false }()
ForLoop:
	for {
		select {
		case <-time.After(w.interval):
			if w.options.CheckHead() {
				if err := w.checkHead(); err == errNotModified {
					break
				} else if err != nil {
					w.callback(err)
					break
				}
			}
			if err := w.download(); err == nil {
				w.callback(nil)
			} else if err != errNotModified {
				w.callback(err)
			}
		case <-w.stop:
			break ForLoop
		}
	}
}

func (w *HTTPWatcher) Stop() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if !w.running {
		return errStopped
	}
	w.stop <- time.Now()
	return nil
}

func (w *HTTPWatcher) checkHead() error {
	// create request
	req, err := http.NewRequest("HEAD", w.remote, nil)
	if err != nil {
		return fmt.Errorf("create HEAD request fail: %s", err)
	}
	if w.options.CheckModTime() {
		req.Header.Set("If-Modified-Since", w.modTime.In(gmt).Format("Mon, 02 Jan 2006 15:04:05 MST"))
	}
	// do request
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send HEAD request fail: %s", err)
	}
	defer resp.Body.Close()

	var modTime time.Time
	// check status
	if resp.StatusCode == http.StatusNotModified {
		return errNotModified
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("bad status: %d(%s)", resp.StatusCode, resp.Status)
	}
	// check modtime
	if w.options.CheckModTime() {
		if header := resp.Header.Get("Last-Modified"); header != "" {
			modTime, err = time.Parse("Mon, 02 Jan 2006 15:04:05 MST", header)
			if err != nil {
				return fmt.Errorf("bad Last-Modified header value: %q", header)
			}
			if !modTime.After(w.modTime) {
				return errNotModified
			}
		}
	}
	// check md5
	if w.options.CheckMD5() {
		if header := resp.Header.Get("Content-MD5"); header != "" {
			checksum, err := base64.StdEncoding.DecodeString(header)
			if err != nil {
				return fmt.Errorf("bad Content-MD5 header value: %q", header)
			}
			if bytes.Equal(checksum, w.checksum[:]) {
				return errNotModified
			}
		}
	}
	// check etag
	if w.options.CheckETag() {
		if header := resp.Header.Get("ETag"); header != "" && w.etag == header {
			return errNotModified
		}
	}
	return nil
}

func (w *HTTPWatcher) download() error {
	// create request
	req, err := http.NewRequest("GET", w.remote, nil)
	if err != nil {
		return fmt.Errorf("create GET request fail: %s", err)
	}
	if w.options.CheckModTime() {
		req.Header.Set("If-Modified-Since", w.modTime.In(gmt).Format("Mon, 02 Jan 2006 15:04:05 MST"))
	}
	// do request
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send GET request fail: %s", err)
	}
	defer resp.Body.Close()

	// check phase
	// check status
	if resp.StatusCode == http.StatusNotModified {
		return errNotModified
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("bad status: %d(%s)", resp.StatusCode, resp.Status)
	}
	// check modTime
	var modTime time.Time
	if w.options.CheckModTime() {
		modTime = time.Now()
		if header := resp.Header.Get("Last-Modified"); header != "" {
			modTime, err = time.Parse("Mon, 02 Jan 2006 15:04:05 MST", header)
			if err != nil {
				return fmt.Errorf("bad Last-Modified header value: %q", header)
			} else if !modTime.After(w.modTime) {
				return errNotModified
			}
		}
	}
	// check etag
	var etag string
	if w.options.CheckETag() {
		if etag = resp.Header.Get("ETag"); etag != "" && w.etag == etag {
			return errNotModified
		}
	}
	// read content
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body fail: %s", err.Error())
	}
	// check md5
	var checksum [md5.Size]byte
	if w.options.CheckMD5() {
		if checksum = md5.Sum(content); checksum == w.checksum {
			return errNotModified
		}
	}

	// save phase
	// save file
	if err := ioutil.WriteFile(w.local, content, w.perm); err != nil {
		return err
	}
	if w.options.CheckMD5() {
		w.checksum = checksum
	}
	if w.options.CheckModTime() {
		w.modTime = modTime
		os.Chtimes(w.local, modTime, modTime)
	}
	if w.options.CheckETag() {
		w.etag = etag
		etagPath := filepath.Join(filepath.Dir(w.local), fmt.Sprintf(".%s.etag", filepath.Base(w.local)))
		if err := ioutil.WriteFile(etagPath, []byte(etag), w.perm); err != nil {
			return fmt.Errorf("save etag file %q fail: %s", etagPath, err.Error())
		}
	}
	return nil
}
