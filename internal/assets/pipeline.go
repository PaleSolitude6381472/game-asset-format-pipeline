package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type Kind string

const (
	PlayerAsset    Kind = "player_asset"
	LiveEvent      Kind = "live_event"
	ModerationItem Kind = "moderation_queue"
)

type Conversion struct {
	Kind        Kind
	PlayerID    string
	EventID     string
	Filename    string
	ContentType string
	Body        io.Reader
}

type Result struct {
	ObjectKey   string `json:"object_key"`
	Format      string `json:"format"`
	ContentType string `json:"content_type"`
}

type Converter interface {
	Convert(context.Context, io.Reader, string, string) ([]byte, error)
}

type ObjectStore interface {
	Put(context.Context, string, string, []byte, string, string) error
}

type Pipeline struct {
	Bucket    string
	Converter Converter
	Store     ObjectStore
}

func FormatFor(kind Kind) (string, error) {
	switch kind {
	case LiveEvent:
		return "avif", nil
	case PlayerAsset, ModerationItem:
		return "webp", nil
	default:
		return "", fmt.Errorf("unknown asset kind %q", kind)
	}
}

func (p Pipeline) Run(ctx context.Context, in Conversion) (Result, error) {
	format, err := FormatFor(in.Kind)
	if err != nil {
		return Result{}, err
	}
	if in.Filename == "" || in.Body == nil {
		return Result{}, fmt.Errorf("filename and image are required")
	}

	converted, err := p.Converter.Convert(ctx, in.Body, in.Filename, format)
	if err != nil {
		return Result{}, err
	}
	key := objectKey(in, format)
	contentType := "image/" + format
	idempotencyKey := stableID(p.Bucket + "/" + key)
	if err := p.Store.Put(ctx, p.Bucket, key, converted, contentType, idempotencyKey); err != nil {
		return Result{}, err
	}
	return Result{ObjectKey: key, Format: format, ContentType: contentType}, nil
}

func objectKey(in Conversion, format string) string {
	owner := in.PlayerID
	if in.Kind == LiveEvent {
		owner = in.EventID
	}
	if owner == "" {
		owner = "unassigned"
	}
	return fmt.Sprintf("%s/%s/%s.%s", in.Kind, clean(owner), stableID(in.Filename)[:16], format)
}

func clean(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "-")
	return strings.ReplaceAll(value, "..", "-")
}

func stableID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
