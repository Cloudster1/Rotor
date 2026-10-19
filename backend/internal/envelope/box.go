// Package envelope seals a run's data at rest with a data key of the run's own, which OpenBao transit makes and
// wraps (#41): a copy of the database holds ciphertext and keys that only OpenBao opens.
package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// Where sealed values live, each bound into its ciphertext so a value moved to another column does not open.
const (
	ColumnInputs     = "run.inputs"
	ColumnOutput     = "run.output"
	ColumnEvent      = "run_event.payload"
	ColumnControl    = "run_control.message"
	ColumnCheckpoint = "run_checkpoint.checkpoint"
	ColumnMetadata   = "run_checkpoint.metadata"
	ColumnWrite      = "run_checkpoint_write.value"
	// An approval's sealed values are each bound to its interrupt, which is unique within its run (#91).
	ColumnApprovalMessage  = "approval.message"
	ColumnApprovalPreview  = "approval.preview"
	ColumnApprovalValues   = "approval.form_values"
	ColumnApprovalApproved = "approval.approved_values"
)

const (
	// version leads every sealed value, so a later format can be told apart.
	version byte = 1
	// aadLabel starts the additional data, so these ciphertexts mean nothing to any other use of the key.
	aadLabel = "rotor/run-data"
	keyBytes = config.DataKeyBits / 8
)

// ErrDamaged is a sealed value that does not open: changed, moved to another row or run, or sealed with another key.
var ErrDamaged = errors.New("the run's data could not be opened; it may have been changed outside Rotor")

// Box seals and opens one run's values with the run's data key.
type Box struct {
	run  uuid.UUID
	aead cipher.AEAD
}

// NewBox makes the box of a run from its unwrapped data key.
func NewBox(run uuid.UUID, key []byte) (*Box, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("envelope: a data key is %d bytes, not %d", keyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	return &Box{run: run, aead: aead}, nil
}

// Seal encrypts plain for the column and row it is stored in; the same column and row open it again.
func (b *Box) Seal(column, row string, plain []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	// crypto/rand.Read never returns an error since Go 1.24; it crashes the program instead.
	_, _ = rand.Read(nonce)
	out := make([]byte, 0, 1+len(nonce)+len(plain)+b.aead.Overhead())
	out = append(out, version)
	out = append(out, nonce...)
	return b.aead.Seal(out, nonce, plain, b.additional(column, row))
}

// Open decrypts a value Seal made for the same run, column and row.
func (b *Box) Open(column, row string, sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < 1+n+b.aead.Overhead() || sealed[0] != version {
		return nil, ErrDamaged
	}
	plain, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], b.additional(column, row))
	if err != nil {
		return nil, ErrDamaged
	}
	return plain, nil
}

// OpenOr opens a value that may be absent, such as a run's output before it ends: nil stays nil.
func (b *Box) OpenOr(column, row string, sealed []byte) ([]byte, error) {
	if sealed == nil {
		return nil, nil
	}
	return b.Open(column, row, sealed)
}

// additional binds a value to the format, the run, the column and the row, each ended by a zero byte.
func (b *Box) additional(column, row string) []byte {
	var out []byte
	out = append(append(out, aadLabel...), 0, version, 0)
	out = append(append(out, b.run[:]...), 0)
	out = append(append(out, column...), 0)
	return append(out, row...)
}
