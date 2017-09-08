package buslteelogger

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// LogFields stores all the custom fields
type LogFields []string

func (l *LogFields) String() string {
	return fmt.Sprintf("%q", *l)
}

// Set is used by the flag package to set new values
func (l *LogFields) Set(value string) error {
	s := strings.Split(value, "=")
	if len(s) != 2 {
		return fmt.Errorf("unexpected log field %q. Format expected: key=value", value)
	}
	*l = append(*l, value)
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
	for _, v := range fields {
		s := strings.Split(v, "=")
		lf[s[0]] = s[1]
	}

	l = &logger{output(logFile), lf}
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
