package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/heroku/busl/busltee"
	"github.com/heroku/rollbar"
	flag "github.com/ogier/pflag"
)

type cmdConfig struct {
	RollbarEnvironment string
	RollbarToken       string
	LogFields          busltee.LogFields
}

func main() {
	cmdConf, publisherConf, err := parseFlags()
	if err != nil {
		usage()
		os.Exit(1)
	}

	if cmdConf.RollbarToken != "" {
		rollbar.SetToken(cmdConf.RollbarToken)
		rollbar.SetEnvironment(cmdConf.RollbarEnvironment)
		rollbar.SetServerRoot("github.com/heroku/busl")
	}

	busltee.ConfigureLogs(publisherConf.LogFile, cmdConf.LogFields)
	defer busltee.CloseLogs()

	if exitCode := busltee.Run(publisherConf.URL, publisherConf.Args, publisherConf); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS] <url> -- <command>\n", os.Args[0])
	flag.PrintDefaults()
}

func parseFlags() (*cmdConfig, *busltee.Config, error) {
	publisherConf := &busltee.Config{}
	cmdConf := &cmdConfig{LogFields: make(busltee.LogFields)}

	cmdConf.RollbarEnvironment = os.Getenv("ROLLBAR_ENVIRONMENT")
	cmdConf.RollbarToken = os.Getenv("ROLLBAR_TOKEN")

	// Connection related flags
	flag.BoolVarP(&publisherConf.Insecure, "insecure", "k", false, "allows insecure SSL connections")
	flag.IntVar(&publisherConf.Retry, "retry", 5, "max retries for connect timeout errors")
	flag.IntVar(&publisherConf.StreamRetry, "stream-retry", 60, "max retries for streamer disconnections")
	flag.Float64Var(&publisherConf.Timeout, "connect-timeout", 1, "max number of seconds to connect to busl URL")

	// Logging related flags
	flag.StringVar(&publisherConf.LogFile, "log-file", "", "log file")
	flag.StringVar(&publisherConf.RequestID, "request-id", "", "request id")
	flag.Var(&cmdConf.LogFields, "log-field", "List of additional logging fields, of the format key=value")

	if flag.Parse(); len(flag.Args()) < 2 {
		return nil, nil, errors.New("insufficient args")
	}

	publisherConf.URL = flag.Arg(0)
	publisherConf.Args = flag.Args()[1:]

	return cmdConf, publisherConf, nil
}
