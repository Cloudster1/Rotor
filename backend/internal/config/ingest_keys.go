package config

import "time"

// Ingest keys a service sends spans with (#258). Migration 01560 writes the same numbers into its checks and trigger,
// and a test holds them to these.
const (
	// MaxIngestKeys bounds the keys of an installation, since each is a standing credential to review.
	MaxIngestKeys = 50
	// MaxIngestKeyNameChars bounds a key's name, as a destination's.
	MaxIngestKeyNameChars = 64
	// IngestKeySecretBytes is the randomness of a key: 256 bits, so a fast hash of it is safe to keep.
	IngestKeySecretBytes = 32
	// IngestKeyHintChars are the key's last characters kept to tell keys apart.
	IngestKeyHintChars = 4
	// IngestKeyLastUsedEvery is how often a key's use is noted, so a busy exporter writes once a minute at most.
	IngestKeyLastUsedEvery = time.Minute
)
