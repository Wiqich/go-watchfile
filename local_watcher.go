package watchfile

import (
	"crypto/md5"
	"errors"
	"io/ioutil"
	"os"
	"sync"
	"time"
)

var (
	errRunning    = errors.New("watcher is running")
	errStopped    = errors.New("watcher is stopped")
	errNoPath     = errors.New("no path")
	errNoCallback = errors.New("no callback")
)

type LocalWatcher struct {
	mutex    sync.Mutex
	path     string
	interval time.Duration
	callback func(error)
	running  bool
	stop     chan time.Time
	options  Option
	modTime  time.Time
	checksum [md5.Size]byte
}

func NewLocalWatcher(path string, interval time.Duration, callback func(error), options Option) (*LocalWatcher, error) {
	if path == "" {
		return nil, errNoPath
	}
	if callback == nil {
		return nil, errNoCallback
	}
	if interval < time.Second {
		interval = time.Second
	}
	options |= CheckModTime
	return &LocalWatcher{
		path:     path,
		interval: interval,
		callback: callback,
		stop:     make(chan time.Time),
		options:  options,
	}, nil
}

func (w *LocalWatcher) Start() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.running {
		return errRunning
	}
	info, err := os.Stat(w.path)
	if err != nil {
		return err
	}
	w.modTime = info.ModTime()
	if w.options.CheckMD5() {
		content, err := ioutil.ReadFile(w.path)
		if err != nil {
			return err
		}
		w.checksum = md5.Sum(content)
	}
	w.running = true
	go w.watchLocal()
	return nil
}

func (w *LocalWatcher) watchLocal() {
	defer func() { w.running = false }()
ForLoop:
	for {
		select {
		case <-time.After(w.interval):
			info, err := os.Stat(w.path)
			if err != nil {
				w.callback(err)
				break
			}
			if !info.ModTime().After(w.modTime) {
				break
			}
			var checksum [md5.Size]byte
			if w.options.CheckMD5() {
				content, err := ioutil.ReadFile(w.path)
				if err != nil {
					w.callback(err)
					break
				}
				checksum = md5.Sum(content)
				if checksum == w.checksum {
					break
				}
			}
			w.modTime = info.ModTime()
			w.checksum = checksum
			w.callback(nil)
		case <-w.stop:
			break ForLoop
		}
	}
}

func (w *LocalWatcher) Stop() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if !w.running {
		return errStopped
	}
	w.stop <- time.Now()
	return nil
}
