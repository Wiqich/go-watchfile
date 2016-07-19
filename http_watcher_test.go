package watchfile

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestNewHTTPWriter(t *testing.T) {
	if _, err := NewHTTPWatcher("", "", 0, 0755, func(error) {}, 0); err != errNoRemoteOrLocal {
		t.Error("unexpected error:", err)
		return
	}
	if _, err := NewHTTPWatcher("http://127.0.0.1:7878", "example.txt", 0, 0755, nil, 0); err != errNoCallback {
		t.Error("unexpected error:", err)
		return
	}
	if watcher, err := NewHTTPWatcher("http://www.example.org", "example.txt", 0, 0755, func(error) {}, 0); err != nil {
		t.Error("unexpected error:", err)
		return
	} else if watcher.interval < time.Minute {
		t.Error("unexpected interval:", watcher.interval)
		return
	}
	defer func() { gmt, _ = time.LoadLocation("GMT") }()
	gmt = nil
	if _, err := NewHTTPWatcher("http://www.example.org", "example.txt", 0, 0755, func(error) {}, CheckModTime); err != errNoGMT {
		t.Error("unexpected error:", err)
		return
	}
}

func TestEnsureLocal(t *testing.T) {
	var cbErr error
	callback := func(err error) { cbErr = err }
	watcher, err := NewHTTPWatcher("http://127.0.0.1:7878", "example.txt", 0, 0755, callback, 0)
	if err != nil {
		t.Error("create watcher fail:", err.Error())
		return
	}
	os.Remove("example.txt")
	defer os.Remove("example.txt")
	// local exists without etag
	ioutil.WriteFile("example.txt", []byte{}, 0755)
	if err := watcher.EnsureLocal(); err != nil {
		t.Error("ensure local error:", err.Error())
		return
	}
	// local exists with etag
	watcher.options = CheckETag
	os.Remove(".example.txt.etag")
	defer os.Remove(".example.txt.etag")
	ioutil.WriteFile(".example.txt.etag", []byte("empty"), 0755)
	watcher.etag = ""
	if err := watcher.EnsureLocal(); err != nil {
		t.Error("ensure local error:", err.Error())
		return
	} else if watcher.etag != "empty" {
		t.Error("unexpected etag:", watcher.etag)
		return
	}
	// download
	respStatusCode = http.StatusOK
	respContent = []byte("foo")
	watcher.options = 0
	os.Remove("example.txt")
	if err := watcher.EnsureLocal(); err != nil {
		t.Error("ensuare local fail:", err.Error())
		return
	}
	if content, err := ioutil.ReadFile("example.txt"); err != nil {
		t.Error("read example.txt fail:", err.Error())
		return
	} else if string(content) != "foo" {
		t.Error("unexpected content:", string(content))
		return
	}
}

func TestDownload(t *testing.T) {
	var cbErr error
	callback := func(err error) { cbErr = err }
	watcher, err := NewHTTPWatcher("http://127.0.0.1:7878", "example.txt", 0, 0755, callback, 0)
	if err != nil {
		t.Error("create watcher fail:", err.Error())
		return
	}
	defer os.Remove("example.txt")
	defer os.Remove(".example.txt.etag")
	// invalid url
	watcher.remote = "http://[::1]%23"
	if err := watcher.download(); err == nil {
		t.Error("unexpected download success")
		return
	}
	// modtime equal
	watcher.modTime = time.Now().Truncate(time.Second)
	watcher.remote = "http://127.0.0.1:7878"
	watcher.options = CheckModTime
	respHeader = map[string]string{
		"Last-Modified": watcher.modTime.In(gmt).Format("Mon, 02 Jan 2006 15:04:05 MST"),
	}
	respContent = []byte("bar")
	respStatusCode = http.StatusOK
	if err := watcher.download(); err != errNotModified {
		t.Error("unexpected error:", err)
		return
	}
	// bad modtime
	respHeader = map[string]string{
		"Last-Modified": "BAD TIME",
	}
	if err := watcher.download(); err == nil {
		t.Error("unexpected success")
		return
	}
	// new modtime
	respHeader = map[string]string{
		"Last-Modified": watcher.modTime.Add(time.Hour).In(gmt).Format("Mon, 02 Jan 2006 15:04:05 MST"),
	}
	if err := watcher.download(); err != nil {
		t.Error("download fail:", err.Error())
		return
	}
	// bad request
	watcher.remote = "http://127.0.0.1:7888"
	if err := watcher.download(); err == nil {
		t.Error("unexpected success")
		return
	}
	// bad status code
	watcher.remote = "http://127.0.0.1:7878"
	respStatusCode = 404
	if err := watcher.download(); err == nil {
		t.Error("unexpected success")
		return
	}
	// not modified
	respStatusCode = 304
	if err := watcher.download(); err != errNotModified {
		t.Error("unexpected error:", err)
		return
	}
	respStatusCode = 200
	// check same etag
	watcher.etag = "FOOBAR"
	watcher.options = CheckETag
	respHeader = map[string]string{
		"ETag": "FOOBAR",
	}
	if err := watcher.download(); err != errNotModified {
		t.Error("unexpected error:", err)
		return
	}
	// same md5
	watcher.options = CheckMD5
	respHeader = map[string]string{}
	watcher.checksum = md5.Sum(respContent)
	if err := watcher.download(); err != errNotModified {
		t.Error("unexpected error:", err)
		return
	}
	// save file fail
	watcher.options = 0
	watcher.local = "/no/such/path"
	if err := watcher.download(); err == nil {
		t.Error("unexpected success")
		return
	}
	// save etag success
	watcher.local = "example.txt"
	os.Mkdir(".example.txt.etag", 0755)
	watcher.etag = ""
	respHeader = map[string]string{
		"ETag": "FOOBAR",
	}
	watcher.options = CheckETag
	if err := watcher.download(); err == nil {
		t.Error("unexpected success")
		return
	}
	// full success
	os.Remove(".example.txt.etag")
	watcher.options = CheckModTime | CheckETag | CheckMD5
	watcher.etag = ""
	watcher.checksum = md5.Sum([]byte("other"))
	watcher.modTime = time.Now().Add(-1 * time.Hour)
	respHeader = map[string]string{
		"ETag":          "FOOBAR",
		"Last-Modified": time.Now().In(gmt).Format("Mon, 02 Jan 2006 15:04:05 MST"),
	}
	respStatusCode = 200
	respContent = []byte("test content")
	fmt.Println(watcher.modTime)
	if err := watcher.download(); err != nil {
		t.Error("download fail:", err.Error())
		return
	}
	if content, err := ioutil.ReadFile("example.txt"); err != nil {
		t.Error("read example.txt fail:", err.Error())
		return
	} else if string(content) != "test content" {
		t.Error("unexpected content of example.txt:", string(content))
		return
	}
	if content, err := ioutil.ReadFile(".example.txt.etag"); err != nil {
		t.Error("read .example.txt.etag fail:", err.Error())
		return
	} else if string(content) != "FOOBAR" {
		t.Error("unexpected content of .example.txt.etag:", string(content))
		return
	}
}

var (
	respStatusCode int
	respHeader     map[string]string
	respContent    []byte
)

func init() {
	go http.ListenAndServe("127.0.0.1:7878", http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		for key, value := range respHeader {
			resp.Header().Set(key, value)
		}
		resp.WriteHeader(respStatusCode)
		resp.Write(respContent)
	}))
}
