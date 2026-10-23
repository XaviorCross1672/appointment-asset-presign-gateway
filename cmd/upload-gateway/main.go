package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

const assetBucket = "appointment-assets"

type uploadService struct {
	storage *infraiClient
	logger  *slog.Logger
}

type uploadGrant struct {
	UploadURL    string            `json:"upload_url"`
	Method       string            `json:"method"`
	ObjectKey    string            `json:"object_key"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Notification operationalNotice `json:"notification"`
}

type operationalNotice struct {
	Event     string `json:"event"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "INFRAI_API_KEY is required")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	storage := newInfraiClient(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := storage.ensureBucket(ctx, assetBucket); err != nil {
		logger.Error("storage setup failed", "error", err)
		os.Exit(1)
	}

	service := &uploadService{storage: storage, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload-intents", service.createUploadIntent)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	addr := envOr("ADDR", ":8080")
	logger.Info("upload gateway ready", "addr", addr, "bucket", assetBucket)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func (s *uploadService) createUploadIntent(w http.ResponseWriter, r *http.Request) {
	var in uploadIntent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request"})
		return
	}
	if err := authorizeUpload(in); err != nil {
		var decision *policyError
		errors.As(err, &decision)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "upload not authorized", "detail": decision})
		return
	}

	requestID, err := randomID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not allocate request id"})
		return
	}
	mediaType := strings.SplitN(in.ContentType, ";", 2)[0]
	extension := map[string]string{"application/pdf": ".pdf", "image/jpeg": ".jpg", "image/png": ".png"}[mediaType]
	objectKey := "appointments/" + requestID + "/" + in.AssetKind + extension
	result, err := s.storage.presignPut(r.Context(), assetBucket, objectKey, mediaType, requestID, in.SizeBytes)
	if err != nil {
		status := http.StatusBadGateway
		var rejected *infraiError
		if errors.As(err, &rejected) && rejected.Status >= 400 && rejected.Status < 500 {
			status = rejected.Status
		}
		s.logger.Warn("upload authorization failed", "request_id", requestID, "status", status, "error", err)
		writeJSON(w, status, map[string]string{"error": "upload authorization failed", "request_id": requestID})
		return
	}

	s.logger.Info("upload authorized", "request_id", requestID, "asset_kind", in.AssetKind, "size_bytes", in.SizeBytes)
	writeJSON(w, http.StatusCreated, uploadGrant{
		UploadURL: result.URL,
		Method:    http.MethodPut,
		ObjectKey: objectKey,
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
		Notification: operationalNotice{
			Event:     "appointment_asset_upload_authorized",
			Message:   "A secure appointment asset upload was authorized.",
			RequestID: requestID,
		},
	})
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
