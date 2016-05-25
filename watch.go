package watchfile

import (
	"os"
	"sync"
	"time"
)

type Watcher struct {
	mutex    sync.Mutex
	path     string
	interval time.Duration
	callback func(string)
	stop     bool
}

func NewWatcher(path string, inteval time.Duration, callback func(string)) *Watch {
	if interval < time.Second {
		interval = time.Second
	}
	return &Watcher{
		path:     path,
		interval: interval,
		callback: callback,
	}
}

func (w *Watcher) Start() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	var ts time.Time
	w.stop = false
	for !w.stop {
		info, err := os.Stat(path)
		if err == nil {
			ts = info.ModTime()
			break
		}
		time.Sleep(interval)
	}
	for !w.Stop {
		time.Sleep(interval)
		info, err := os.Stat(path)
		if err == nil && info.ModTime().After(ts) {
			callback(path)
			ts = info.ModTime()
		}
	}
}

func (w *Watcher) Stop() {
	w.stop = true
}
