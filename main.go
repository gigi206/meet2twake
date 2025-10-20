package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
)

func health(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(w, `{"status": "ok"}`)
}

func minio(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "hello\n")
}

func main() {
	port := 8090
	if env := os.Getenv("PORT"); env != "" {
		p, err := strconv.Atoi(env)
		if err != nil {
			panic(err)
		}
		port = p
	}

	http.HandleFunc("/health", health)
	http.HandleFunc("/minio", minio)

	log.Printf("Starting server on port %d...", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), handler); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
