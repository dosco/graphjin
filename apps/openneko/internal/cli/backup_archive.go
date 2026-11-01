package cli

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// An `openneko backup` file is one authenticated, encrypted stream:
//
//	header  := magic[16] | version[1] | keyID[16] | salt[32]
//	chunk   := length[4, big endian] | AES-256-GCM(ciphertext || tag)
//
// The AES key is HKDF-SHA256(backup key, salt, "openneko backup v1"), fresh
// for every file. Each chunk seals at most backupChunkSize plaintext bytes
// with a nonce of counter[8] | 0[3] | final[1] and the header as additional
// data, so reordering, truncation, dropped or appended chunks, header edits
// and a wrong key all fail authentication. Exactly one chunk, the last, is
// final; anything after it is rejected.
//
// The decrypted payload is a sequence of named entries:
//
//	entry   := 0x01 | nameLength[2] | name | frame* | 0x00000000
//	frame   := length[4] | bytes          (1 <= length <= backupFrameMax)
//	payload := entry* | 0x00
//
// Entries are streamed, so neither a database dump nor a volume archive has
// to be staged on disk in plaintext.
const (
	backupArchiveMagic   = "OPENNEKO-BACKUP\n"
	backupArchiveVersion = 1
	backupKeyIDSize      = 16
	backupSaltSize       = 32
	backupHeaderSize     = len(backupArchiveMagic) + 1 + backupKeyIDSize + backupSaltSize
	backupChunkSize      = 64 << 10
	backupFrameMax       = 1 << 20
	backupFileSuffix     = ".openneko-backup"
)

var (
	errBackupTruncated = errors.New("backup file is truncated")
	errBackupCorrupt   = errors.New("backup file failed authentication (corrupted, tampered with, or encrypted with a different key)")
	backupEntryName    = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,199}$`)
)

// backupKeyID identifies a key without revealing it, so a restore can say
// which key a file needs before attempting decryption.
func backupKeyID(key string) []byte {
	id, err := hkdf.Key(sha256.New, []byte(key), nil, "openneko backup key id v1", backupKeyIDSize)
	if err != nil {
		panic(err) // only possible for an invalid output length
	}
	return id
}

func backupKeyIDString(key string) string {
	return hex.EncodeToString(backupKeyID(key))
}

func backupStreamCipher(key string, salt []byte) (cipher.AEAD, error) {
	derived, err := hkdf.Key(sha256.New, []byte(key), salt, "openneko backup v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func backupChunkNonce(counter uint64, final bool) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce, counter)
	if final {
		nonce[11] = 1
	}
	return nonce
}

type backupEncrypter struct {
	out     io.Writer
	aead    cipher.AEAD
	header  []byte
	buf     []byte
	counter uint64
	closed  bool
}

// newBackupEncrypter writes the header immediately. Close must be called to
// emit the final chunk; a stream without it is rejected as truncated.
func newBackupEncrypter(out io.Writer, key string) (io.WriteCloser, error) {
	salt := make([]byte, backupSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := backupStreamCipher(key, salt)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, backupHeaderSize)
	header = append(header, backupArchiveMagic...)
	header = append(header, backupArchiveVersion)
	header = append(header, backupKeyID(key)...)
	header = append(header, salt...)
	if _, err := out.Write(header); err != nil {
		return nil, err
	}
	return &backupEncrypter{out: out, aead: aead, header: header, buf: make([]byte, 0, backupChunkSize)}, nil
}

func (e *backupEncrypter) Write(p []byte) (int, error) {
	if e.closed {
		return 0, errors.New("write to closed backup stream")
	}
	written := 0
	for len(p) > 0 {
		// A full buffer is sealed as non-final only once more data arrives,
		// so Close can always mark the true last chunk final.
		if len(e.buf) == backupChunkSize {
			if err := e.seal(false); err != nil {
				return written, err
			}
		}
		n := copy(e.buf[len(e.buf):backupChunkSize], p)
		e.buf = e.buf[:len(e.buf)+n]
		p = p[n:]
		written += n
	}
	return written, nil
}

func (e *backupEncrypter) seal(final bool) error {
	sealed := e.aead.Seal(nil, backupChunkNonce(e.counter, final), e.buf, e.header)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(sealed)))
	if _, err := e.out.Write(length[:]); err != nil {
		return err
	}
	if _, err := e.out.Write(sealed); err != nil {
		return err
	}
	e.counter++
	e.buf = e.buf[:0]
	return nil
}

func (e *backupEncrypter) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	return e.seal(true)
}

type backupDecrypter struct {
	in      *bufio.Reader
	aead    cipher.AEAD
	header  []byte
	plain   []byte
	counter uint64
	final   bool
	err     error
}

// readBackupHeader parses the plaintext header and returns the key ID it
// names, so callers can report a key mismatch before any decryption.
func readBackupHeader(in io.Reader) ([]byte, error) {
	header := make([]byte, backupHeaderSize)
	if _, err := io.ReadFull(in, header); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("not an OpenNeko backup file: %w", errBackupTruncated)
		}
		return nil, err
	}
	if string(header[:len(backupArchiveMagic)]) != backupArchiveMagic {
		return nil, errors.New("not an OpenNeko backup file")
	}
	if version := header[len(backupArchiveMagic)]; version != backupArchiveVersion {
		return nil, fmt.Errorf("unsupported OpenNeko backup format version %d", version)
	}
	return header, nil
}

func backupHeaderKeyID(header []byte) []byte {
	start := len(backupArchiveMagic) + 1
	return header[start : start+backupKeyIDSize]
}

// newBackupDecrypter returns a reader of the authenticated plaintext. Every
// byte it returns has been authenticated; it reports errBackupTruncated or
// errBackupCorrupt rather than io.EOF when the stream is incomplete or altered.
func newBackupDecrypter(in io.Reader, key string) (io.Reader, error) {
	reader := bufio.NewReaderSize(in, backupChunkSize+64)
	header, err := readBackupHeader(reader)
	if err != nil {
		return nil, err
	}
	if got, want := hex.EncodeToString(backupHeaderKeyID(header)), backupKeyIDString(key); got != want {
		return nil, &backupKeyMismatchError{fileKeyID: got, keyID: want}
	}
	salt := header[len(header)-backupSaltSize:]
	aead, err := backupStreamCipher(key, salt)
	if err != nil {
		return nil, err
	}
	return &backupDecrypter{in: reader, aead: aead, header: header}, nil
}

type backupKeyMismatchError struct {
	fileKeyID string
	keyID     string
}

func (e *backupKeyMismatchError) Error() string {
	return fmt.Sprintf("backup was encrypted with key %s, but the selected key is %s", e.fileKeyID, e.keyID)
}

func (d *backupDecrypter) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.err != nil {
			return 0, d.err
		}
		d.err = d.next()
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}

func (d *backupDecrypter) next() error {
	var length [4]byte
	if _, err := io.ReadFull(d.in, length[:]); err != nil {
		if errors.Is(err, io.EOF) && d.final {
			return io.EOF
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return errBackupTruncated
		}
		return err
	}
	if d.final {
		return fmt.Errorf("%w: data after the final chunk", errBackupCorrupt)
	}
	size := binary.BigEndian.Uint32(length[:])
	if size < uint32(d.aead.Overhead()) || size > uint32(backupChunkSize+d.aead.Overhead()) {
		return errBackupCorrupt
	}
	sealed := make([]byte, size)
	if _, err := io.ReadFull(d.in, sealed); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return errBackupTruncated
		}
		return err
	}
	// The final flag is not stored; try the common non-final case first.
	plain, err := d.aead.Open(sealed[:0:0], backupChunkNonce(d.counter, false), sealed, d.header)
	if err != nil {
		plain, err = d.aead.Open(sealed[:0:0], backupChunkNonce(d.counter, true), sealed, d.header)
		if err != nil {
			return errBackupCorrupt
		}
		d.final = true
	}
	d.counter++
	d.plain = plain
	return nil
}

// backupBundleWriter frames named entries inside the encrypted stream.
type backupBundleWriter struct {
	out  io.Writer
	open bool
}

func newBackupBundleWriter(out io.Writer) *backupBundleWriter {
	return &backupBundleWriter{out: out}
}

// Entry starts a named entry. The returned writer must be closed before the
// next entry starts.
func (w *backupBundleWriter) Entry(name string) (io.WriteCloser, error) {
	if w.open {
		return nil, errors.New("previous backup entry is still open")
	}
	if !backupEntryName.MatchString(name) {
		return nil, fmt.Errorf("invalid backup entry name %q", name)
	}
	header := []byte{1, 0, 0}
	binary.BigEndian.PutUint16(header[1:], uint16(len(name)))
	header = append(header, name...)
	if _, err := w.out.Write(header); err != nil {
		return nil, err
	}
	w.open = true
	return &backupEntryWriter{bundle: w}, nil
}

// Finish writes the end-of-bundle marker.
func (w *backupBundleWriter) Finish() error {
	if w.open {
		return errors.New("backup entry is still open")
	}
	_, err := w.out.Write([]byte{0})
	return err
}

type backupEntryWriter struct {
	bundle *backupBundleWriter
	closed bool
}

func (e *backupEntryWriter) Write(p []byte) (int, error) {
	if e.closed {
		return 0, errors.New("write to closed backup entry")
	}
	written := 0
	for len(p) > 0 {
		frame := p
		if len(frame) > backupFrameMax {
			frame = frame[:backupFrameMax]
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(frame)))
		if _, err := e.bundle.out.Write(length[:]); err != nil {
			return written, err
		}
		if _, err := e.bundle.out.Write(frame); err != nil {
			return written, err
		}
		written += len(frame)
		p = p[len(frame):]
	}
	return written, nil
}

func (e *backupEntryWriter) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	e.bundle.open = false
	_, err := e.bundle.out.Write([]byte{0, 0, 0, 0})
	return err
}

// backupBundleReader iterates the entries of a decrypted payload.
type backupBundleReader struct {
	in      io.Reader
	current *backupEntryReader
	done    bool
}

func newBackupBundleReader(in io.Reader) *backupBundleReader {
	return &backupBundleReader{in: in}
}

// Next skips whatever remains of the current entry and returns the next one.
// It returns io.EOF only after the end marker and the authenticated end of
// the underlying stream.
func (r *backupBundleReader) Next() (string, io.Reader, error) {
	if r.done {
		return "", nil, io.EOF
	}
	if r.current != nil {
		if _, err := io.Copy(io.Discard, r.current); err != nil {
			return "", nil, err
		}
		r.current = nil
	}
	var kind [1]byte
	if _, err := io.ReadFull(r.in, kind[:]); err != nil {
		return "", nil, unexpectedBackupEnd(err)
	}
	switch kind[0] {
	case 0:
		var trailing [1]byte
		if n, err := r.in.Read(trailing[:]); n != 0 || !errors.Is(err, io.EOF) {
			if err != nil && !errors.Is(err, io.EOF) {
				return "", nil, err
			}
			return "", nil, fmt.Errorf("%w: data after the end of the backup", errBackupCorrupt)
		}
		r.done = true
		return "", nil, io.EOF
	case 1:
	default:
		return "", nil, fmt.Errorf("%w: unknown entry marker", errBackupCorrupt)
	}
	var nameLength [2]byte
	if _, err := io.ReadFull(r.in, nameLength[:]); err != nil {
		return "", nil, unexpectedBackupEnd(err)
	}
	name := make([]byte, binary.BigEndian.Uint16(nameLength[:]))
	if _, err := io.ReadFull(r.in, name); err != nil {
		return "", nil, unexpectedBackupEnd(err)
	}
	if !backupEntryName.Match(name) {
		return "", nil, fmt.Errorf("%w: invalid entry name", errBackupCorrupt)
	}
	r.current = &backupEntryReader{in: r.in}
	return string(name), r.current, nil
}

func unexpectedBackupEnd(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: backup ended inside its contents", errBackupCorrupt)
	}
	return err
}

type backupEntryReader struct {
	in        io.Reader
	remaining uint32
	done      bool
}

func (e *backupEntryReader) Read(p []byte) (int, error) {
	for e.remaining == 0 {
		if e.done {
			return 0, io.EOF
		}
		var length [4]byte
		if _, err := io.ReadFull(e.in, length[:]); err != nil {
			return 0, unexpectedBackupEnd(err)
		}
		e.remaining = binary.BigEndian.Uint32(length[:])
		if e.remaining == 0 {
			e.done = true
			return 0, io.EOF
		}
		if e.remaining > backupFrameMax {
			return 0, fmt.Errorf("%w: oversized frame", errBackupCorrupt)
		}
	}
	if uint32(len(p)) > e.remaining {
		p = p[:e.remaining]
	}
	n, err := e.in.Read(p)
	e.remaining -= uint32(n)
	if err != nil && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
		if e.remaining > 0 {
			return n, unexpectedBackupEnd(err)
		}
		err = nil
	}
	return n, err
}
