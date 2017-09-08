package busltee

import (
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/heroku/busl/busltee/buslteelogger"
	"github.com/sirupsen/logrus"
)

var ErrTooManyRetries = errors.New("Reached max retries")

type Transport struct {
	retries       uint
	MaxRetries    uint
	Transport     http.RoundTripper
	SleepDuration time.Duration

	bufferName string
	cond       *sync.Cond
	mutex      *sync.Mutex
	closed     bool
}

func (t *Transport) RoundTrip(req *http.Request) (res *http.Response, err error) {
	tmpFile, err := ioutil.TempFile("", "busltee_buffer")
	if err != nil {
		return nil, err
	}
	t.bufferName = tmpFile.Name()
	t.cond = sync.NewCond(&nopLocker{})
	t.mutex = &sync.Mutex{}

	go func() {
		defer tmpFile.Close()
		defer t.Close()

		tee := io.TeeReader(req.Body, &broadcastingWriter{tmpFile, t.cond})
		_, err := ioutil.ReadAll(tee)
		if err != nil {
			buslteelogger.Fatal(err)
		}
	}()

	if t.Transport == nil {
		t.Transport = &http.Transport{}
	}
	if t.SleepDuration == 0 {
		t.SleepDuration = time.Second
	}
	return t.tries(req)
}

func (t *Transport) tries(req *http.Request) (*http.Response, error) {
	res, err := t.runRequest(req)

	if err != nil || res.StatusCode/100 != 2 {
		if t.retries < t.MaxRetries {
			time.Sleep(t.SleepDuration)
			t.retries += 1
			return t.tries(req)
		}
	} else {
		t.retries = 0
	}
	return res, err
}

func (t *Transport) runRequest(req *http.Request) (*http.Response, error) {
	var statusCode int
	bodyReader, err := t.newBodyReader()
	if err != nil {
		return nil, err
	}

	newReq, err := http.NewRequest(req.Method, req.URL.String(), bodyReader)
	newReq.Header = req.Header

	buslteelogger.WithFields(logrus.Fields{
		"count#busltee.streamer.start": 1,
		"request_id":                   req.Header.Get("Request-Id"),
		"url":                          req.URL,
	}).Warn()
	res, err := t.Transport.RoundTrip(newReq)
	newReq.Body.Close()
	if res != nil {
		statusCode = res.StatusCode
	}
	buslteelogger.WithFields(logrus.Fields{
		"count#busltee.streamer.end": 1,
		"request_id":                 req.Header.Get("Request-Id"),
		"url":                        req.URL,
		"err":                        err,
		"status":                     statusCode,
	}).Warn()
	return res, err
}

func (t *Transport) newBodyReader() (io.ReadCloser, error) {
	reader, err := os.Open(t.bufferName)
	if err != nil {
		return nil, err
	}
	return &bodyReader{reader, t, false}, nil
}

func (t *Transport) Close() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.closed = true
	t.cond.Broadcast()
	return nil
}

func (t *Transport) isClosed() bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.closed
}

type nopLocker struct{}

func (*nopLocker) Lock()   {}
func (*nopLocker) Unlock() {}

type broadcastingWriter struct {
	io.Writer
	cond *sync.Cond
}

func (w *broadcastingWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.cond.Broadcast()
	return n, err
}

type bodyReader struct {
	io.ReadCloser
	t      *Transport
	closed bool
}

func (b *bodyReader) Close() error {
	err := b.ReadCloser.Close()
	if err == nil {
		b.closed = true
	}
	return err
}

func (b *bodyReader) Read(p []byte) (int, error) {
	for {
		n, err := b.ReadCloser.Read(p)
		if err == io.EOF && !b.isClosed() {
			err = nil
		}

		if n > 0 || err != nil {
			return n, err
		}

		b.t.cond.Wait()
	}
}

func (b *bodyReader) isClosed() bool {
	return b.closed || b.t.isClosed()
}
