package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var clouderyURL, clouderyToken string
var minioClient *minio.Client

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
	if clouderyToken == "" {
		log.Fatalf("Missing CLOUDERY_TOKEN")
	}

	prepareMinIOClient()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/minio", minioHandler)
	mux.HandleFunc("/transcript", transcriptHandler)

	log.Printf("Starting server on port %d...", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func prepareMinIOClient() {
	minioURL := os.Getenv("MINIO_URL")
	minioUser := os.Getenv("MINIO_USER")
	minioPassword := os.Getenv("MINIO_PASSWORD")
	minioInsecure := os.Getenv("MINIO_INSECURE")

	if minioURL == "" || minioUser == "" || minioPassword == "" {
		log.Fatal("MINIO_URL, MINIO_USER and MINIO_PASSWORD must be defined")
	}
	u, err := url.Parse(minioURL)
	if err != nil {
		log.Fatalf("Invalid MINIO_URL: %s", err)
	}

	useSSL := true
	skipVerify := false
	if minioInsecure != "" {
		insecure, err := strconv.ParseBool(minioInsecure)
		if err != nil {
			log.Fatalf("Invalid MINIO_INSECURE: %s", err)
		}
		skipVerify = insecure
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
	}

	client, err := minio.New(u.Host, &minio.Options{
		Creds:     credentials.NewStaticV4(minioUser, minioPassword, ""),
		Secure:    useSSL,
		Transport: transport,
	})
	if err != nil {
		log.Fatalf("Erreur lors de la création du client MinIO: %v", err)
	}
	minioClient = client
}

func sendError(code int, res http.ResponseWriter, err error) {
	res.WriteHeader(code)
	fmt.Printf("Error: %s\n", err)
	log.Printf("Error: %s\n", err)
}

func healthHandler(res http.ResponseWriter, req *http.Request) {
	fmt.Fprintln(res, `{"status": "ok"}`)
}

type webhookRequest struct {
	EventName string
	Key       string
}

func minioHandler(res http.ResponseWriter, req *http.Request) {
	var webhook webhookRequest
	if err := json.NewDecoder(req.Body).Decode(&webhook); err != nil {
		sendError(http.StatusBadRequest, res, err)
		return
	}
	if webhook.EventName != "s3:ObjectCreated:Put" || !strings.HasSuffix(webhook.Key, ".json") {
		res.WriteHeader(http.StatusNoContent)
		return
	}

	roomID, err := getRoomID(webhook.Key)
	if err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}

	sub, err := getSubFromRoomID(roomID)
	if err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}

	instance, err := findInstanceBySub(sub)
	if err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}

	token, err := getDriveToken(instance)
	if err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}

	key := strings.TrimSuffix(webhook.Key, ".json")
	content, err := getFromMinIO(key)
	if err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}
	defer content.Close()

	if err := saveContent(instance, token, filepath.Base(key), content); err != nil {
		sendError(http.StatusInternalServerError, res, err)
		return
	}

	res.WriteHeader(http.StatusNoContent)
}

func getRoomID(key string) (string, error) {
	obj, err := getFromMinIO(key)
	if err != nil {
		return "", err
	}
	defer obj.Close()
	var data map[string]any
	if err := json.NewDecoder(obj).Decode(&data); err != nil {
		return "", err
	}
	roomID, _ := data["room_id"].(string)
	if roomID == "" {
		return "", errors.New("no room_id")
	}
	return roomID, nil
}

func getFromMinIO(key string) (io.ReadCloser, error) {
	ctx := context.Background()
	parts := strings.SplitN(key, "/", 2)
	bucket := parts[0]
	objectName := parts[1]
	return minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
}

const query = `
SELECT u.sub
FROM meet_user u
JOIN meet_resource_access ra ON u.id = ra.user_id
WHERE ra.resource_id = $1
ORDER BY ra.created_at;
`

func getSubFromRoomID(roomID string) (string, error) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("POSTGRES_URL"))
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)

	var sub string
	err = conn.QueryRow(context.Background(), query, roomID).Scan(&sub)
	if err != nil {
		return "", err
	}
	return sub, nil
}

func saveContent(instance, token, filename string, content io.Reader) error {
	q := &url.Values{}
	q.Add("Type", "file")
	q.Add("Name", filename)
	// TODO content-length
	u := &url.URL{
		Scheme:   "https",
		Host:     instance,
		Path:     "/files/io.cozy.files.root-dir",
		RawQuery: q.Encode(),
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), content)
	if err != nil {
		return fmt.Errorf("cannot make request to stack: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot call stack: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected response from the stack: %d", res.StatusCode)
	}
	return nil
}

type transcriptRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Email   string `json:"email"`
	Sub     string `json:"sub"`
}

func transcriptHandler(res http.ResponseWriter, req *http.Request) {
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

	res.WriteHeader(http.StatusNoContent)
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
	defer res.Body.Close()
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
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected response from the stack: %d", res.StatusCode)
	}
	return nil
}
