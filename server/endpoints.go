package server

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"

	"github.com/heroku/busl/broker"
	"github.com/heroku/busl/util"
)

func (s *Server) createStream(w http.ResponseWriter, r *http.Request) {
	registrar := broker.NewRedisRegistrar()

	if err := registrar.Register(key(r)); err != nil {
		http.Error(w, "Unable to create stream. Please try again.", http.StatusServiceUnavailable)
		util.CountWithData("put.create.fail", 1, "error=%s", err)
		handleError(w, r, err)
		return
	}
	util.Count("put.create.success")
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK")
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	writer, err := broker.NewWriter(key(r))
	if err != nil {
		handleError(w, r, err)
		return
	}

	body := bufio.NewReader(r.Body)
	defer r.Body.Close()

	wl, err := broker.Len(writer)
	if err != nil {
		handleError(w, r, err)
		return
	}
	if wl > 0 {
		_, err = body.Discard(int(wl))
		if err != nil {
			handleError(w, r, err)
			return
		}
	}

	_, err = io.Copy(writer, body)

	if err == io.ErrUnexpectedEOF {
		util.CountWithData("server.pub.read.eoferror", 1, "msg=%q request_id=%q", err, r.Header.Get("Request-Id"))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	netErr, ok := err.(net.Error)
	if ok && netErr.Timeout() {
		util.CountWithData("server.pub.read.timeout", 1, "msg=%q request_id=%q", err, r.Header.Get("Request-Id"))
		handleError(w, r, netErr)
		return
	}

	if err != nil {
		log.Printf("%#v", err)
		http.Error(w, "Unhandled error, please try again.", http.StatusInternalServerError)
		logError(r, err)
		return
	}

	util.CountWithData("server.pub.read.end", 1, "request_id=%q", r.Header.Get("Request-Id"))
	writer.Close()
	// Asynchronously upload the output to our defined storage backend.
	go storeOutput(key(r), requestURI(r), s.StorageBaseURL(r))
}

func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	rd, err := s.newReader(w, r)
	if rd != nil {
		defer rd.Close()
	}
	if err != nil {
		handleError(w, r, err)
		return
	}
	n, err := io.Copy(newWriteFlusher(w), rd)

	netErr, ok := err.(net.Error)
	if ok && netErr.Timeout() {
		util.CountWithData("server.sub.read.timeout", 1, "msg=%q request_id=%q", err, r.Header.Get("Request-Id"))
		return
	}

	if err != nil {
		logError(r, err)
	}
	if n == 0 {
		handleError(w, r, errNoContent)
		return
	}

	util.CountWithData("server.sub.read.finish", 1, "msg=%q request_id=%q", err, r.Header.Get("Request-Id"))
}

func (s *Server) closeStream(w http.ResponseWriter, r *http.Request) {
	writer, err := broker.NewWriter(key(r))
	if err != nil {
		handleError(w, r, err)
		return
	}

	util.CountWithData("server.close", 1, "request_id=%q", r.Header.Get("Request-Id"))
	err = writer.Close()
	if err != nil {
		handleError(w, r, err)
		return
	}
	// Asynchronously upload the output to our defined storage backend.
	go storeOutput(key(r), requestURI(r), s.StorageBaseURL(r))
}
