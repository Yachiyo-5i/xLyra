package backup

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	envelopeMagic     = "XLYRA-BACKUP"
	envelopeVersion   = 2
	argonTime         = 3
	argonMemory       = 64 * 1024
	argonThreads      = 4
	argonKeyLength    = 32
	envelopeSaltBytes = 16
	noncePrefixBytes  = 8
	backupChunkSize   = 1 << 20
)

func encryptStream(passphrase string, src io.Reader, dst io.Writer) error {
	gcm, noncePrefix, err := streamCipher(passphrase)
	if err != nil {
		return err
	}
	if err := writeEnvelopeHeader(dst, noncePrefix.salt, noncePrefix.prefix); err != nil {
		return err
	}

	buf := make([]byte, backupChunkSize)
	var index uint32
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			nonce := chunkNonce(noncePrefix.prefix, index)
			ciphertext := gcm.Seal(nil, nonce, buf[:n], nil)
			if err := writeChunk(dst, ciphertext); err != nil {
				return err
			}
			index++
		}
		if readErr == io.EOF {
			return writeChunk(dst, nil)
		}
		if readErr != nil {
			return fmt.Errorf("read backup plaintext: %w", readErr)
		}
	}
}

func decryptStream(passphrase string, src io.Reader, dst io.Writer) error {
	header, err := readEnvelopeHeader(src)
	if err != nil {
		return err
	}
	gcm, err := gcmFromPassphrase(passphrase, header.salt)
	if err != nil {
		return err
	}

	reader := bufio.NewReader(src)
	var index uint32
	for {
		ciphertext, err := readChunk(reader)
		if err != nil {
			return err
		}
		if ciphertext == nil {
			return nil
		}
		nonce := chunkNonce(header.noncePrefix, index)
		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return fmt.Errorf("decrypt backup payload: %w", err)
		}
		if _, err := dst.Write(plaintext); err != nil {
			return fmt.Errorf("write backup plaintext: %w", err)
		}
		index++
	}
}

type streamCipherContext struct {
	salt   []byte
	prefix []byte
}

type envelopeHeader struct {
	salt        []byte
	noncePrefix []byte
}

func streamCipher(passphrase string) (cipher.AEAD, streamCipherContext, error) {
	passphrase = strings.TrimSpace(passphrase)
	if passphrase == "" {
		return nil, streamCipherContext{}, fmt.Errorf("backup passphrase is required")
	}
	salt := make([]byte, envelopeSaltBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, streamCipherContext{}, fmt.Errorf("generate salt: %w", err)
	}
	noncePrefix := make([]byte, noncePrefixBytes)
	if _, err := io.ReadFull(rand.Reader, noncePrefix); err != nil {
		return nil, streamCipherContext{}, fmt.Errorf("generate nonce: %w", err)
	}
	gcm, err := gcmFromPassphrase(passphrase, salt)
	if err != nil {
		return nil, streamCipherContext{}, err
	}
	return gcm, streamCipherContext{salt: salt, prefix: noncePrefix}, nil
}

func gcmFromPassphrase(passphrase string, salt []byte) (cipher.AEAD, error) {
	passphrase = strings.TrimSpace(passphrase)
	if passphrase == "" {
		return nil, fmt.Errorf("backup passphrase is required")
	}
	key := deriveBackupKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return gcm, nil
}

func writeEnvelopeHeader(dst io.Writer, salt []byte, noncePrefix []byte) error {
	if _, err := dst.Write([]byte(envelopeMagic)); err != nil {
		return fmt.Errorf("write backup header: %w", err)
	}
	if err := binary.Write(dst, binary.BigEndian, uint16(envelopeVersion)); err != nil {
		return fmt.Errorf("write backup header: %w", err)
	}
	if _, err := dst.Write(salt); err != nil {
		return fmt.Errorf("write backup header: %w", err)
	}
	if _, err := dst.Write(noncePrefix); err != nil {
		return fmt.Errorf("write backup header: %w", err)
	}
	return nil
}

func readEnvelopeHeader(src io.Reader) (envelopeHeader, error) {
	magic := make([]byte, len(envelopeMagic))
	if _, err := io.ReadFull(src, magic); err != nil {
		return envelopeHeader{}, fmt.Errorf("read backup header: %w", err)
	}
	if string(magic) != envelopeMagic {
		return envelopeHeader{}, fmt.Errorf("unsupported backup format")
	}
	var version uint16
	if err := binary.Read(src, binary.BigEndian, &version); err != nil {
		return envelopeHeader{}, fmt.Errorf("read backup header: %w", err)
	}
	if version != envelopeVersion {
		return envelopeHeader{}, fmt.Errorf("unsupported backup format version %d", version)
	}
	salt := make([]byte, envelopeSaltBytes)
	if _, err := io.ReadFull(src, salt); err != nil {
		return envelopeHeader{}, fmt.Errorf("read backup header: %w", err)
	}
	noncePrefix := make([]byte, noncePrefixBytes)
	if _, err := io.ReadFull(src, noncePrefix); err != nil {
		return envelopeHeader{}, fmt.Errorf("read backup header: %w", err)
	}
	return envelopeHeader{salt: salt, noncePrefix: noncePrefix}, nil
}

func writeChunk(dst io.Writer, ciphertext []byte) error {
	if err := binary.Write(dst, binary.BigEndian, uint32(len(ciphertext))); err != nil {
		return fmt.Errorf("write backup chunk: %w", err)
	}
	if len(ciphertext) == 0 {
		return nil
	}
	if _, err := dst.Write(ciphertext); err != nil {
		return fmt.Errorf("write backup chunk: %w", err)
	}
	return nil
}

func readChunk(src io.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(src, binary.BigEndian, &size); err != nil {
		return nil, fmt.Errorf("read backup chunk: %w", err)
	}
	if size == 0 {
		return nil, nil
	}
	ciphertext := make([]byte, size)
	if _, err := io.ReadFull(src, ciphertext); err != nil {
		return nil, fmt.Errorf("read backup chunk: %w", err)
	}
	return ciphertext, nil
}

func chunkNonce(prefix []byte, index uint32) []byte {
	nonce := make([]byte, 12)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[8:], index)
	return nonce
}

func deriveBackupKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, argonKeyLength)
}
