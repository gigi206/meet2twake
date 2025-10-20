package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

var clouderyURL, clouderyToken string

func main() {
	port := 8090
	if env := os.Getenv("PORT"); env != "" {
		p, err := strconv.Atoi(env)
		if err != nil {
			log.Fatalf("Failed to parse the PORT: %v", err)
		}
		port = p
	}

	clouderyURL = "https://manager.cozycloud.cc/"
	if u := os.Getenv("CLOUDERY_URL"); u != "" {
		clouderyURL = u
	}
	clouderyToken = os.Getenv("CLOUDERY_TOKEN")

	mux := http.NewServeMux()
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/minio", minio)
	mux.HandleFunc("/transcript", transcript)

	log.Printf("Starting server on port %d...", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func sendError(code int, res http.ResponseWriter, err error) {
	res.WriteHeader(code)
	fmt.Printf("Error: %s\n", err)
	log.Printf("Error: %s\n", err)
}

func health(res http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(res, `{"status": "ok"}`)
}

func minio(res http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(res, "hello\n")
}

type transcriptRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Email   string `json:"email"`
	Sub     string `json:"sub"`
}

func transcript(res http.ResponseWriter, req *http.Request) {
	var transcript transcriptRequest
	if err := json.NewDecoder(req.Body).Decode(&transcript); err != nil {
		sendError(http.StatusBadRequest, res, err)
		return
	}

	instance, err := findInstanceBySub(transcript.Sub)
	if err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}

	token, err := getDriveToken(instance)
	if err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}

	if err := saveTranscript(instance, token, transcript); err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}

	res.WriteHeader(http.StatusOK)
	fmt.Fprintf(res, "hello\n")
}

func findInstanceBySub(sub string) (string, error) {
	instance := fmt.Sprintf("%s.twake.linagora.com", sub)
	return instance, nil
}

type driveTokenResponse struct {
	Token string `json:"token"`
}

func getDriveToken(instance string) (string, error) {
	u, err := url.Parse(clouderyURL)
	if err != nil {
		return "", fmt.Errorf("invalid clouderyURL: %w", err)
	}
	u.Path = "/api/public/instances/" + instance + "/drive_token"
	req, err := http.NewRequest(http.MethodPost, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("cannot make request to cloudery: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+clouderyToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot call cloudery: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected response from the cloudery: %d", res.StatusCode)
	}
	var body driveTokenResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("unexpected response from the cloudery: %w", err)
	}
	return body.Token, nil
}

func saveTranscript(instance, token string, transcript transcriptRequest) error {
	q := &url.Values{}
	q.Add("Type", "file")
	q.Add("Name", transcript.Title+".cozy-note")
	q.Add("Content-Type", "text/vnd.cozy.note+markdown")
	u := &url.URL{
		Scheme:   "https",
		Host:     instance,
		Path:     "/files/io.cozy.files.root-dir",
		RawQuery: q.Encode(),
	}
	body := strings.NewReader(transcript.Content)
	req, err := http.NewRequest(http.MethodPost, u.String(), body)
	if err != nil {
		return fmt.Errorf("cannot make request to stack: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot call stack: %w", err)
	}
	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected response from the stack: %d", res.StatusCode)
	}
	return nil
}
