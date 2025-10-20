package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
)

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
	http.HandleFunc("/minio", minio)
	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}
