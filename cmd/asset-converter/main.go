package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/infrai-examples/game-asset-format-pipeline/internal/assets"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	bucket := envOr("ASSET_BUCKET", "game-assets")
	baseURL := envOr("INFRAI_BASE_URL", assets.DefaultBaseURL)
	client := assets.NewClient(baseURL, apiKey)
	if err := client.CreateBucket(context.Background(), bucket); err != nil {
		log.Fatalf("create asset bucket: %v", err)
	}
	pipeline := assets.Pipeline{Bucket: bucket, Converter: client, Store: client}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /assets", uploadHandler(pipeline))
	addr := envOr("LISTEN_ADDR", ":8080")
	log.Printf("asset converter listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func uploadHandler(pipeline assets.Pipeline) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart request"})
			return
		}
		file, header, err := r.FormFile("image")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
			return
		}
		defer file.Close()
		result, err := pipeline.Run(r.Context(), assets.Conversion{
			Kind: assets.Kind(r.FormValue("kind")), PlayerID: r.FormValue("player_id"),
			EventID: r.FormValue("event_id"), Filename: header.Filename,
			ContentType: header.Header.Get("Content-Type"), Body: file,
		})
		if err != nil {
			status := http.StatusBadGateway
			var apiErr *assets.APIError
			if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
				status = apiErr.HTTPStatus
			} else if errors.As(err, &apiErr) {
				status = http.StatusBadGateway
			} else if _, decisionErr := assets.FormatFor(assets.Kind(r.FormValue("kind"))); decisionErr != nil {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
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
