package buslteelogger

import (
	"io"
	"io/ioutil"
	"os"

	"github.com/sirupsen/logrus"
)

type logger struct {
	out           io.Writer
	defaultFields logrus.Fields
}

var l *logger

// OpenLogs configures the log file
func OpenLogs(logFile string) {
	l = &logger{output(logFile), logrus.Fields{}}
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
