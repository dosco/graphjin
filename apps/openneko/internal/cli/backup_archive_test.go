package cli

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testBackupKey  = "test-backup-key-with-more-than-thirty-two-characters"
	otherBackupKey = "another-backup-key-with-more-than-thirty-two-characters"
)

func encryptForTest(t *testing.T, key string, plain []byte, writeSize int) []byte {
	t.Helper()
	var out bytes.Buffer
	encrypter, err := newBackupEncrypter(&out, key)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < len(plain); start += writeSize {
		end := min(start+writeSize, len(plain))
		if _, err := encrypter.Write(plain[start:end]); err != nil {
			t.Fatal(err)
		}
	}
	if err := encrypter.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func decryptForTest(key string, sealed []byte) ([]byte, error) {
	plain, err := newBackupDecrypter(bytes.NewReader(sealed), key)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(plain)
}

func TestBackupStreamRoundTrip(t *testing.T) {
	for _, size := range []int{0, 1, backupChunkSize - 1, backupChunkSize, backupChunkSize + 1, 3*backupChunkSize + 17} {
		plain := make([]byte, size)
		if _, err := rand.Read(plain); err != nil {
			t.Fatal(err)
		}
		for _, writeSize := range []int{1 << 20, 4093} {
			sealed := encryptForTest(t, testBackupKey, plain, writeSize)
			got, err := decryptForTest(testBackupKey, sealed)
			if err != nil {
				t.Fatalf("size %d: %v", size, err)
			}
			if !bytes.Equal(got, plain) {
				t.Fatalf("size %d: round trip mismatch", size)
			}
		}
	}
}

func TestBackupStreamUsesFreshSaltAndHidesPlaintext(t *testing.T) {
	plain := bytes.Repeat([]byte("secret-config-value "), 100)
	first := encryptForTest(t, testBackupKey, plain, len(plain))
	second := encryptForTest(t, testBackupKey, plain, len(plain))
	if bytes.Equal(first, second) {
		t.Fatal("two backups of the same data produced identical ciphertext")
	}
	if bytes.Contains(first, []byte("secret-config-value")) {
		t.Fatal("ciphertext contains plaintext")
	}
}

func TestBackupStreamRejectsWrongKey(t *testing.T) {
	sealed := encryptForTest(t, testBackupKey, []byte("hello"), 5)
	_, err := decryptForTest(otherBackupKey, sealed)
	var mismatch *backupKeyMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("wrong key error = %v, want key mismatch", err)
	}
	if mismatch.fileKeyID != backupKeyIDString(testBackupKey) || mismatch.keyID != backupKeyIDString(otherBackupKey) {
		t.Fatalf("mismatch names wrong key ids: %+v", mismatch)
	}

	// A forged header naming the other key still cannot decrypt the data.
	forged := append([]byte(nil), sealed...)
	copy(forged[len(backupArchiveMagic)+1:], backupKeyID(otherBackupKey))
	if _, err := decryptForTest(otherBackupKey, forged); !errors.Is(err, errBackupCorrupt) {
		t.Fatalf("forged key id error = %v, want authentication failure", err)
	}
}

func TestBackupStreamRejectsTruncation(t *testing.T) {
	plain := make([]byte, 2*backupChunkSize+100)
	sealed := encryptForTest(t, testBackupKey, plain, len(plain))
	chunk := 4 + backupChunkSize + 16
	for _, cut := range []int{
		0,
		backupHeaderSize - 1,
		backupHeaderSize,             // header only
		backupHeaderSize + 2,         // inside a length prefix
		backupHeaderSize + chunk,     // clean chunk boundary, final chunk missing
		backupHeaderSize + 2*chunk,   // clean boundary before the final chunk
		backupHeaderSize + chunk + 9, // inside a chunk
		len(sealed) - 1,              // inside the final chunk
	} {
		_, err := decryptForTest(testBackupKey, sealed[:cut])
		if err == nil {
			t.Fatalf("truncation at %d of %d accepted", cut, len(sealed))
		}
		if !errors.Is(err, errBackupTruncated) && !errors.Is(err, errBackupCorrupt) {
			t.Fatalf("truncation at %d: unexpected error %v", cut, err)
		}
	}
}

func TestBackupStreamRejectsTampering(t *testing.T) {
	plain := make([]byte, 2*backupChunkSize+100)
	sealed := encryptForTest(t, testBackupKey, plain, len(plain))
	chunk := 4 + backupChunkSize + 16

	cases := map[string][]byte{}
	flipped := append([]byte(nil), sealed...)
	flipped[backupHeaderSize+4+10] ^= 1
	cases["flipped ciphertext bit"] = flipped

	salt := append([]byte(nil), sealed...)
	salt[backupHeaderSize-1] ^= 1
	cases["edited salt"] = salt

	reordered := append([]byte(nil), sealed[:backupHeaderSize]...)
	reordered = append(reordered, sealed[backupHeaderSize+chunk:backupHeaderSize+2*chunk]...)
	reordered = append(reordered, sealed[backupHeaderSize:backupHeaderSize+chunk]...)
	reordered = append(reordered, sealed[backupHeaderSize+2*chunk:]...)
	cases["swapped chunks"] = reordered

	dropped := append([]byte(nil), sealed[:backupHeaderSize+chunk]...)
	dropped = append(dropped, sealed[backupHeaderSize+2*chunk:]...)
	cases["dropped chunk"] = dropped

	cases["appended data"] = append(append([]byte(nil), sealed...), 0, 0, 0, 0)

	for name, data := range cases {
		if _, err := decryptForTest(testBackupKey, data); !errors.Is(err, errBackupCorrupt) {
			t.Errorf("%s: error = %v, want authentication failure", name, err)
		}
	}

	notBackup := []byte(strings.Repeat("x", backupHeaderSize+10))
	if _, err := decryptForTest(testBackupKey, notBackup); err == nil || !strings.Contains(err.Error(), "not an OpenNeko backup") {
		t.Fatalf("foreign file error = %v", err)
	}
}

func TestBackupBundleRoundTrip(t *testing.T) {
	var payload bytes.Buffer
	bundle := newBackupBundleWriter(&payload)
	large := bytes.Repeat([]byte{7}, backupFrameMax+5)
	entries := []struct {
		name string
		data []byte
	}{
		{"manifest.json", []byte(`{"format":1}`)},
		{"empty", nil},
		{"volumes/openneko-config.tar", large},
	}
	for _, entry := range entries {
		writer, err := bundle.Entry(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(entry.data); err != nil {
			t.Fatal(err)
		}
		if _, err := bundle.Entry("too-early"); err == nil {
			t.Fatal("bundle allowed overlapping entries")
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := bundle.Finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := newBackupBundleWriter(io.Discard).Entry("../escape"); err == nil {
		t.Fatal("bundle accepted an unsafe entry name")
	}

	reader := newBackupBundleReader(bytes.NewReader(payload.Bytes()))
	for index, want := range entries {
		name, entry, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if name != want.name {
			t.Fatalf("entry %d name = %q, want %q", index, name, want.name)
		}
		if index == 1 {
			continue // skipped entries are drained by Next
		}
		got, err := io.ReadAll(entry)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want.data) {
			t.Fatalf("entry %s data mismatch", name)
		}
	}
	if _, _, err := reader.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("end of bundle = %v, want io.EOF", err)
	}

	truncated := newBackupBundleReader(bytes.NewReader(payload.Bytes()[:payload.Len()-1]))
	var err error
	for err == nil {
		_, _, err = truncated.Next()
	}
	if !errors.Is(err, errBackupCorrupt) {
		t.Fatalf("bundle without end marker = %v, want corrupt", err)
	}
}

type testBackupContents struct {
	volumes      map[string][]byte
	rolePassword string
	dump         []byte
	skipDump     bool
}

func testVolumeTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := tar.NewWriter(&out)
	for name, content := range files {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Uid: 1001, Gid: 1001}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func writeTestBackupFile(t *testing.T, key string, contents testBackupContents) string {
	t.Helper()
	manifest := backupManifest{Format: backupManifestFormat, CreatedAt: time.Now().UTC(), Project: "openneko-prod", OpenNekoVersion: "test"}
	manifest.Database.User, manifest.Database.Name = "neko", "neko"
	for _, volume := range backupVolumes {
		if _, ok := contents.volumes[volume.name]; ok {
			manifest.Volumes = append(manifest.Volumes, volume.name)
		}
	}
	path := filepath.Join(t.TempDir(), "test"+backupFileSuffix)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	encrypter, err := newBackupEncrypter(file, key)
	if err != nil {
		t.Fatal(err)
	}
	bundle := newBackupBundleWriter(encrypter)
	write := func(name string, data []byte) {
		entry, err := bundle.Entry(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := entry.Close(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	write(backupManifestEntry, raw)
	for _, volume := range manifest.Volumes {
		write(backupVolumeEntryPrefix+volume+backupVolumeEntrySuffix, contents.volumes[volume])
	}
	write(backupRolePasswordEntry, []byte(contents.rolePassword))
	if !contents.skipDump {
		write(backupDatabaseEntry, contents.dump)
	}
	if err := bundle.Finish(); err != nil {
		t.Fatal(err)
	}
	if err := encrypter.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func validTestBackupContents(t *testing.T) testBackupContents {
	return testBackupContents{
		volumes: map[string][]byte{
			"openneko-config":       testVolumeTar(t, map[string]string{"./config.json": `{"pg":{}}`, "./secret-key": "k"}),
			"openneko-graphjin-cli": testVolumeTar(t, map[string]string{"./files/library/report.md": "# Report"}),
		},
		rolePassword: "SCRAM-SHA-256$4096:c2FsdA==$c3RvcmVk:c2VydmVy",
		dump:         append([]byte("PGDMP"), bytes.Repeat([]byte{1}, 3*backupChunkSize)...),
	}
}

func TestVerifyBackupFileAcceptsCompleteBackup(t *testing.T) {
	path := writeTestBackupFile(t, testBackupKey, validTestBackupContents(t))
	verified, err := verifyBackupFile(path, testBackupKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(verified.Manifest.Volumes, ","); got != "openneko-config,openneko-graphjin-cli" {
		t.Fatalf("manifest volumes = %q", got)
	}
	if verified.Manifest.Project != "openneko-prod" || verified.Manifest.Database.Name != "neko" {
		t.Fatalf("manifest = %+v", verified.Manifest)
	}
}

func TestVerifyBackupFileRejectsBadBackups(t *testing.T) {
	missingDump := validTestBackupContents(t)
	missingDump.skipDump = true
	notADump := validTestBackupContents(t)
	notADump.dump = []byte("SELECT 1;")
	badTar := validTestBackupContents(t)
	badTar.volumes["openneko-config"] = []byte("definitely not a tar archive, but long enough to fill a header block" + strings.Repeat("!", 600))
	badRole := validTestBackupContents(t)
	badRole.rolePassword = "plain'; DROP ROLE neko; --"

	for name, test := range map[string]struct {
		contents testBackupContents
		want     string
	}{
		"missing dump":   {missingDump, "missing database/neko.dump"},
		"not a dump":     {notADump, "pg_dump custom-format"},
		"bad volume tar": {badTar, "invalid volume archive"},
		"bad role":       {badRole, "unexpected format"},
	} {
		path := writeTestBackupFile(t, testBackupKey, test.contents)
		if _, err := verifyBackupFile(path, testBackupKey); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error = %v, want %q", name, err, test.want)
		}
	}

	path := writeTestBackupFile(t, testBackupKey, validTestBackupContents(t))
	if _, err := verifyBackupFile(path, otherBackupKey); err == nil {
		t.Fatal("verification accepted the wrong key")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(t.TempDir(), "truncated"+backupFileSuffix)
	if err := os.WriteFile(truncated, raw[:len(raw)-backupChunkSize/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyBackupFile(truncated, testBackupKey); !errors.Is(err, errBackupTruncated) && !errors.Is(err, errBackupCorrupt) {
		t.Fatalf("truncated backup error = %v", err)
	}
	raw[len(raw)/2] ^= 0x40
	tampered := filepath.Join(t.TempDir(), "tampered"+backupFileSuffix)
	if err := os.WriteFile(tampered, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyBackupFile(tampered, testBackupKey); !errors.Is(err, errBackupCorrupt) {
		t.Fatalf("tampered backup error = %v", err)
	}
}
