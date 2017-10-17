package busltee

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// LogFields stores all the custom fields
type LogFields map[string]string

func (l *LogFields) String() string {
	return fmt.Sprintf("%q", *l)
}

// Set is used by the flag package to set new values
func (l *LogFields) Set(value string) error {
	s := strings.SplitN(value, "=", 2)
	if len(s) != 2 {
		return fmt.Errorf("unexpected log field %q. Format expected: key=value", value)
	}
	(*l)[s[0]] = s[1]
	return nil
}

type logger struct {
	out           io.Writer
	defaultFields logrus.Fields
}

var l *logger

// ConfigureLogs configures the log file
func ConfigureLogs(logFile string, fields LogFields) {
	lf := logrus.Fields{}
	for k, v := range fields {
		lf[k] = v
	}

	l = &logger{output(logFile), lf}
	logrus.SetFormatter(logrus.StandardLogger().Formatter)
	logrus.SetOutput(l.out)
}

// CloseLogs closes an open log file
func CloseLogs() {
	if f, ok := l.out.(io.Closer); ok {
		f.Close()
	}
}

func output(logFile string) io.Writer {
	if logFile == "" {
		return ioutil.Discard
	}
	file, err := os.OpenFile(logFile, os.O_RDWR|os.O_APPEND, 0660)
	if err != nil {
		return ioutil.Discard
	}
	return file
}

func logInfo(args ...interface{}) {
	logFields().Info(args...)
}

func logError(args ...interface{}) {
	logFields().Error(args...)
}

func logFatalf(s string, v ...interface{}) {
	logFields().Fatalf(s, v...)
}

func logFatal(args ...interface{}) {
	logFields().Fatal(args...)
}

func logWithFields(f logrus.Fields) *logrus.Entry {
	return logFields().WithFields(f)
}

// The default logging fields
func logFields() *logrus.Entry {
	if l == nil {
		// Logging is not configured
		return logrus.WithFields(logrus.Fields{})
	}
	return logrus.WithFields(l.defaultFields)
}

var urlRE = regexp.MustCompile("(?i)\\b((?:[a-z][\\w-]+:(?:/{1,3}|[a-z0-9%])|www\\d{0,3}[.]|[a-z0-9.\\-]+[.][a-z]{2,4}/)(?:[^\\s()<>]+|\\(([^\\s()<>]+|(\\([^\\s()<>]+\\)))*\\))+(?:\\(([^\\s()<>]+|(\\([^\\s()<>]+\\)))*\\)|[^\\s`!()\\[\\]{};:'\".,<>?«»“”‘’]))")

type scrubber struct {
	logrus.Formatter
}

func (s *scrubber) Format(entry *logrus.Entry) ([]byte, error) {
	p, err := s.Formatter.Format(entry)
	p = urlRE.ReplaceAllFunc(p, scrubURL)
	return p, err
}

func scrubURL(url []byte) []byte {
	if i := bytes.IndexRune(url, '?'); i >= 0 {
		return append(url[:i], []byte("?...")...)
	}
	return url
}
