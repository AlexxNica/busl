package busltee

import (
	"io"
	"io/ioutil"
	"os"

	"github.com/sirupsen/logrus"
)

var out io.Writer

// OpenLogs configures the log file
func OpenLogs(logFile string) {
	out = output(logFile)

	logrus.SetOutput(out)
}

// CloseLogs closes an open log file
func CloseLogs() {
	if f, ok := out.(io.Closer); ok {
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
