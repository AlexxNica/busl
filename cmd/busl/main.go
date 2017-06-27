package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/heroku/busl/server"
	"github.com/heroku/rollbar"
)

var nonWordCharacters = regexp.MustCompile(`\W`)

type cmdConfig struct {
	RollbarEnvironment string
	RollbarToken       string

	HTTPPort         string
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
}

func main() {
	fmt.Println("Starting busl...")
	cmdConf, httpConf, err := parseFlags()
	if err != nil {
		os.Exit(1)
	}

	if cmdConf.RollbarToken != "" {
		rollbar.SetToken(cmdConf.RollbarToken)
		rollbar.SetEnvironment(cmdConf.RollbarEnvironment)
		rollbar.SetServerRoot("github.com/heroku/busl")
	}

	_, err = strconv.Atoi(cmdConf.HTTPPort)
	if err != nil {
		log.Printf("%s: $PORT must be an integer value.\n", os.Args[0])
		os.Exit(1)
	}

	s := server.NewServer(httpConf)
	s.ReadTimeout = cmdConf.HTTPReadTimeout
	s.WriteTimeout = cmdConf.HTTPWriteTimeout
	s.Start(cmdConf.HTTPPort, awaitSignals(syscall.SIGURG))
}

func parseFlags() (*cmdConfig, *server.Config, error) {
	httpConf := &server.Config{}
	cmdConf := &cmdConfig{}

	cmdConf.RollbarEnvironment = os.Getenv("ROLLBAR_ENVIRONMENT")
	cmdConf.RollbarToken = os.Getenv("ROLLBAR_TOKEN")

	cmdConf.HTTPPort = os.Getenv("PORT")
	flag.DurationVar(&cmdConf.HTTPReadTimeout, "httpReadTimeout", time.Hour, "Timeout for HTTP request reading")
	flag.DurationVar(&cmdConf.HTTPWriteTimeout, "httpWriteTimeout", time.Hour, "Timeout for HTTP request writing")

	httpConf.Credentials = os.Getenv("CREDS")
	httpConf.EnforceHTTPS = os.Getenv("ENFORCE_HTTPS") == "1"
	flag.DurationVar(&httpConf.HeartbeatDuration, "subscribeHeartbeatDuration", time.Second*10, "Heartbeat interval for HTTP stream subscriptions.")
	httpConf.StorageBaseURL = getStorageBaseURL

	flag.Parse()

	return cmdConf, httpConf, nil
}

func getStorageBaseURL(r *http.Request) string {
	prefix := strings.ToUpper(nonWordCharacters.ReplaceAllString(r.Host, "_"))
	if v := os.Getenv(fmt.Sprintf("%v_STORAGE_BASE_URL", prefix)); v != "" {
		return v
	}

	return os.Getenv("STORAGE_BASE_URL")
}

func awaitSignals(signals ...os.Signal) <-chan struct{} {
	s := make(chan os.Signal, 1)
	signal.Notify(s, signals...)
	log.Printf("signals.await signals=%v\n", signals)

	received := make(chan struct{})
	go func() {
		log.Printf("signals.received signal=%v\n", <-s)
		close(received)
	}()

	return received
}
