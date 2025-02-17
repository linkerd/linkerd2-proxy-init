package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

var (
	port            = os.Getenv("PORT")
	_, amITheProxy  = os.LookupEnv("AM_I_THE_PROXY")
	hostname, _     = os.Hostname()
	defaultResponse = fmt.Sprintf("%s:%s", hostname, port)
)

func returnHostAndPortHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Got request [%v] returning [%s]\n", r, response())
	_, _ = fmt.Fprintln(w, response())
}

func callOtherServiceHandler(w http.ResponseWriter, r *http.Request) {
	url := r.FormValue("url")
	log.Printf("Got request [%v] making HTTP call to [%s]\n", r, url)
	// This is only used in tests, so skip URL validation.
	//nolint:gosec
	downstreamResp, err := http.Get(url)
	if err != nil {
		http.Error(w, err.Error(), 500)
	} else {
		body, err := io.ReadAll(downstreamResp.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
		} else {
			response := fmt.Sprintf("me:[%s]downstream:[%s]", response(), strings.TrimSpace(string(body)))
			_, _ = fmt.Fprintln(w, response)
		}
	}
}

func response() string {
	if amITheProxy {
		return "proxy"
	}

	return defaultResponse
}

func main() {
	fmt.Printf("Starting stub HTTP server on port [%s] will serve [%s] proxy [%t]\n", port, hostname, amITheProxy)

	http.HandleFunc("/", returnHostAndPortHandler)
	http.HandleFunc("/call", callOtherServiceHandler)
	//nolint:gosec
	err := http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	if err != nil {
		panic(err)
	}
}
