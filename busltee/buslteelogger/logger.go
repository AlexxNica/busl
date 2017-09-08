package buslteelogger

import (
	"github.com/sirupsen/logrus"
)

// Info logs with an info level
func Info(args ...interface{}) {
	logFields().Info(args...)
}

// Error logs with an error level
func Error(args ...interface{}) {
	logFields().Error(args...)
}

// Fatalf formats the error and logs it with a fatal level
func Fatalf(s string, v ...interface{}) {
	logFields().Fatalf(s, v...)
}

// Fatal logs with a fatal level
func Fatal(args ...interface{}) {
	logFields().Fatal(args...)
}

// WithFields gives a new logging entry with additional fields
func WithFields(f logrus.Fields) *logrus.Entry {
	return logFields().WithFields(f)
}

// The default logging fields
func logFields() *logrus.Entry {
	return logrus.WithFields(logrus.Fields{})
}
