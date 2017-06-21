package busltee

import (
	"crypto/tls"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// Config holds the runner configuration
type Config struct {
	Insecure      bool
	Timeout       float64
	Retry         int
	StreamRetry   int
	SleepDuration time.Duration
	URL           string
	Args          []string
	LogFile       string
	RequestID     string
	Verbose       bool
}

// Run creates the stdin listener and forwards logs to URI
func Run(url string, args []string, conf *Config) (exitCode int) {
	defer monitor("busltee.busltee", time.Now())
	setupLog(conf)

	reader, writer := io.Pipe()
	done := post(url, reader, conf)

	if err := run(args, writer, writer); err != nil {
		logrus.WithFields(logrus.Fields{"count#busltee.exec.error": 1}).Error(err)
		exitCode = exitStatus(err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		logrus.WithFields(logrus.Fields{"count#busltee.exec.upload.timeout": 1}).Warn("OK")
	}

	return exitCode
}

func setupLog(conf *Config) {
	if conf.Verbose {
		logrus.SetLevel(logrus.InfoLevel)
		return
	}

	logrus.SetLevel(logrus.WarnLevel)
}

func monitor(subject string, ts time.Time) {
	logrus.WithFields(logrus.Fields{"time": time.Now().Sub(ts).Seconds()}).Warnf("%s.time", subject)
}

func post(url string, reader io.Reader, conf *Config) chan struct{} {
	done := make(chan struct{})

	go func() {
		if err := stream(url, reader, conf); err != nil {
			logrus.WithFields(logrus.Fields{"count#busltee.stream.error": 1}).Error(err)
			// Prevent writes from blocking.
			io.Copy(ioutil.Discard, reader)
		} else {
			logrus.WithFields(logrus.Fields{"count#busltee.stream.success": 1}).Warn("OK")
		}
		close(done)
	}()

	return done
}

func stream(url string, stdin io.Reader, conf *Config) (err error) {
	for retries := conf.Retry; retries >= 0; retries-- {
		if err = streamNoRetry(url, stdin, conf); !isTimeout(err) {
			return err
		}
		logrus.WithFields(logrus.Fields{"count#busltee.stream.retry": 1}).Warn("OK")
	}
	return err
}

var errMissingURL = errors.New("Missing URL")

func streamNoRetry(url string, stdin io.Reader, conf *Config) error {
	defer monitor("busltee.stream", time.Now())

	if url == "" {
		logrus.WithFields(logrus.Fields{"count#busltee.stream.missingurl": 1}).Warn("OK")
		return errMissingURL
	}

	client := &http.Client{Transport: newTransport(conf)}

	// In the event that the `busl` connection doesn't work,
	// we still need to proceed with the command's execution.
	// For this reason, we wrap `stdin` in NopCloser to prevent
	// it from being closed prematurely (and thus allowing writes
	// on the other end of the pipe to work).
	req, err := http.NewRequest("POST", url, ioutil.NopCloser(stdin))
	if conf.RequestID != "" {
		req.Header.Set("Request-Id", conf.RequestID)
	}

	if err != nil {
		return err
	}

	res, err := client.Do(req)
	if res != nil {
		defer res.Body.Close()
	}
	return err
}

func newTransport(conf *Config) http.RoundTripper {
	tr := &http.Transport{}

	if conf.Timeout > 0 {
		tr.Dial = (&net.Dialer{
			KeepAlive: 30 * time.Second,
			Timeout:   time.Duration(conf.Timeout) * time.Second,
		}).Dial
	}

	if conf.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Transport{
		Transport:     tr,
		MaxRetries:    uint(conf.StreamRetry),
		SleepDuration: conf.SleepDuration,
	}
}

func run(args []string, stdout, stderr io.WriteCloser) error {
	defer stdout.Close()
	defer stderr.Close()
	defer monitor("busltee.run", time.Now())

	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	errCh, err := attachCmd(cmd, io.MultiWriter(stdout, os.Stdout), io.MultiWriter(stderr, os.Stderr))
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Catch any signals sent to busltee, and pass those along.
	deliverSignals(cmd)

	state, err := wait(cmd)

	var copyErr error
	select {
	case copyErr = <-errCh:
	case <-time.After(30 * time.Second):
	}

	if err != nil {
		return err
	} else if !state.Success() {
		return &exec.ExitError{ProcessState: state}
	}

	return copyErr
}

func attachCmd(cmd *exec.Cmd, stdout, stderr io.Writer) (<-chan error, error) {
	ch := make(chan error)
	errCh := make(chan error, 2)

	copyFunc := func(w io.Writer, r io.ReadCloser) {
		_, err := io.Copy(w, r)
		r.Close()
		errCh <- err
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	go copyFunc(stdout, stdoutPipe)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	go copyFunc(stderr, stderrPipe)

	go func() {
		var copyErr error
		for i := 0; i < 2; i++ {
			if err := <-errCh; err != nil && copyErr == nil {
				copyErr = err
			}
		}
		if copyErr != nil {
			ch <- copyErr
		}
		close(ch)
	}()

	return ch, nil
}

func deliverSignals(cmd *exec.Cmd) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc)
	go func() {
		s := <-sigc
		logrus.WithFields(logrus.Fields{"busltee.signal.deliver": s}).Info("OK")
		cmd.Process.Signal(s)
	}()
}

func wait(cmd *exec.Cmd) (*os.ProcessState, error) {
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		defer syscall.Kill(-pgid, syscall.SIGTERM)
	}

	return cmd.Process.Wait()
}

func isTimeout(err error) bool {
	e, ok := err.(net.Error)
	return ok && e.Timeout()
}

func exitStatus(err error) int {
	if exit, ok := err.(*exec.ExitError); ok {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	// Default to exit status 1 if we can't type assert the error.
	return 1
}
