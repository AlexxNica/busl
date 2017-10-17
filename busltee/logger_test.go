package busltee

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestScrubs(t *testing.T) {
	ConfigureLogs("", LogFields{})
	var b bytes.Buffer
	logrus.SetOutput(&b)

	logWithFields(logrus.Fields{
		"url": "https://example.com/stream?token=secret",
	}).Warn()

	if strings.Contains(string(b.Bytes()), "secret") {
		t.Fatalf("expected URL to not include \"secret\"")
	}
}
