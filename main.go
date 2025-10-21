package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid/v5"
	pgxfrs "github.com/jackc/pgx-gofrs-uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var clouderyURL, clouderyToken string
var minioClient *minio.Client

func main() {
	debug := false
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		debug = true
		slog.SetLogLoggerLevel(slog.LevelDebug)
	case "warn":
		slog.SetLogLoggerLevel(slog.LevelWarn)
	case "error":
		slog.SetLogLoggerLevel(slog.LevelError)
	default:
		slog.SetLogLoggerLevel(slog.LevelInfo)
	}

	port := 8090
	if env := os.Getenv("PORT"); env != "" {
		p, err := strconv.Atoi(env)
		if err != nil {
			slog.Error("Failed to parse the PORT", "error", err)
			os.Exit(1)
		}
		port = p
	}

	clouderyURL = "https://manager.cozycloud.cc/"
	if u := os.Getenv("CLOUDERY_URL"); u != "" {
		clouderyURL = u
	}
	clouderyToken = os.Getenv("CLOUDERY_TOKEN")
	if clouderyToken == "" {
		slog.Error("Missing CLOUDERY_TOKEN")
		os.Exit(1)
	}

	prepareMinIOClient(debug)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/minio", minioHandler)
	mux.HandleFunc("/transcript", transcriptHandler)

	handler := withLog(mux)

	log.Printf("Starting server on port %d...", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), handler); err != nil {
		slog.Error("Server failed to start", "error", err)
		os.Exit(1)
	}
}

func prepareMinIOClient(debug bool) {
	minioURL := os.Getenv("MINIO_URL")
	minioUser := os.Getenv("MINIO_USER")
	minioPassword := os.Getenv("MINIO_PASSWORD")
	minioInsecure := os.Getenv("MINIO_INSECURE")

	if minioURL == "" || minioUser == "" || minioPassword == "" {
		slog.Error("MINIO_URL, MINIO_USER and MINIO_PASSWORD must be defined")
		os.Exit(1)
	}
	u, err := url.Parse(minioURL)
	if err != nil {
		slog.Error("Invalid MINIO_URL", "error", err)
		os.Exit(1)
	}

	useSSL := true
	skipVerify := false
	if minioInsecure != "" {
		insecure, err := strconv.ParseBool(minioInsecure)
		if err != nil {
			slog.Error("Invalid MINIO_INSECURE", "error", err)
			os.Exit(1)
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
		slog.Error("Erreur lors de la création du client MinIO", "error", err)
	}
	minioClient = client

	if debug {
		client.TraceOn(os.Stderr)
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		slog.Info("Request",
			"status", wrapped.statusCode,
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start),
		)
	})
}

func sendError(code int, res http.ResponseWriter, err error) {
	res.WriteHeader(code)
	fmt.Printf("Error: %s\n", err)
	slog.Warn("Error", "error", err)
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

	sub, err := getSubFromRoomID(*roomID)
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

	slog.Info("recording saved", "sub", sub)
	res.WriteHeader(http.StatusNoContent)
}

func getRoomID(key string) (*uuid.UUID, error) {
	obj, err := getFromMinIO(key)
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	var data map[string]any
	if err := json.NewDecoder(obj).Decode(&data); err != nil {
		return nil, err
	}
	roomID, _ := data["room_id"].(string)
	if roomID == "" {
		return nil, errors.New("no room_id")
	}
	decoded, err := base64.RawStdEncoding.DecodeString(roomID)
	if err != nil {
		return nil, fmt.Errorf("invalid room_id: %w", err)
	}
	id, err := uuid.FromBytes(decoded)
	if err != nil {
		return nil, fmt.Errorf("invalid room_id: %w", err)
	}
	return &id, nil
}

func getFromMinIO(key string) (io.ReadCloser, error) {
	ctx := context.Background()
	parts := strings.SplitN(key, "/", 2)
	bucket := parts[0]
	objectName := parts[1]
	slog.Debug("getFromMinIO",
		"bucket", bucket,
		"objectName", objectName)
	obj, err := minioClient.GetObject(ctx, bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("Cannot get from minIO: %s", err)
	}
	return obj, nil
}

const query = `
SELECT u.sub
FROM meet_user u
JOIN meet_resource_access ra ON u.id = ra.user_id
WHERE ra.resource_id = $1
ORDER BY ra.created_at;
`

func getSubFromRoomID(roomID uuid.UUID) (string, error) {
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(os.Getenv("POSTGRES_URL"))
	if err != nil {
		return "", fmt.Errorf("Cannot parse PG config: %s", err)
	}

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxfrs.Register(conn.TypeMap())
		return nil
	}

	conn, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return "", fmt.Errorf("Cannot connect to PG: %s", err)
	}
	defer conn.Close()

	var sub string
	err = conn.QueryRow(context.Background(), query, roomID).Scan(&sub)
	if err != nil {
		return "", fmt.Errorf("Cannot query: %s", err)
	}
	slog.Debug("getSubFromRoomID",
		"roomID", roomID.String(),
		"sub", sub)
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
	slog.Debug("Save recording",
		"instance", instance,
		"name", filename,
		"status", res.StatusCode)
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

	slog.Info("transcript saved", "sub", transcript.Sub)
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
	slog.Debug("Get token for",
		"instance", instance,
		"status", res.StatusCode)
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
	slog.Debug("Save transcript",
		"instance", instance,
		"name", transcript.Title,
		"status", res.StatusCode)
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected response from the stack: %d", res.StatusCode)
	}
	return nil
}
