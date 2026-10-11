package assets

import (
	"bytes"
	"context"
	"io"
	"testing"
)

type fakeBackend struct {
	format      string
	objectKey   string
	contentType string
}

func (f *fakeBackend) Convert(_ context.Context, _ io.Reader, _, format string) ([]byte, error) {
	f.format = format
	return []byte("converted"), nil
}

func (f *fakeBackend) Put(_ context.Context, _, key string, _ []byte, contentType, _ string) error {
	f.objectKey = key
	f.contentType = contentType
	return nil
}

func TestPipelineChoosesFormatAndStoragePrefix(t *testing.T) {
	tests := []struct {
		name       string
		kind       Kind
		playerID   string
		eventID    string
		wantFormat string
		wantPrefix string
	}{
		{"player upload uses webp", PlayerAsset, "player-42", "", "webp", "player_asset/player-42/"},
		{"event art uses avif", LiveEvent, "", "season-9", "avif", "live_event/season-9/"},
		{"moderation preview uses webp", ModerationItem, "player-42", "", "webp", "moderation_queue/player-42/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := &fakeBackend{}
			pipeline := Pipeline{Bucket: "game-assets", Converter: backend, Store: backend}
			result, err := pipeline.Run(context.Background(), Conversion{
				Kind: tt.kind, PlayerID: tt.playerID, EventID: tt.eventID,
				Filename: "banner.png", ContentType: "image/png", Body: bytes.NewBufferString("png"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Format != tt.wantFormat || backend.format != tt.wantFormat {
				t.Fatalf("format = %q, backend format = %q; want %q", result.Format, backend.format, tt.wantFormat)
			}
			if len(backend.objectKey) < len(tt.wantPrefix) || backend.objectKey[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Fatalf("object key = %q; want prefix %q", backend.objectKey, tt.wantPrefix)
			}
			if backend.contentType != "image/"+tt.wantFormat {
				t.Fatalf("content type = %q", backend.contentType)
			}
		})
	}
}
