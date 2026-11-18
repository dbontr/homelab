package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func sha(data []byte) string { v := sha256.Sum256(data); return "sha256:" + hex.EncodeToString(v[:]) }
func marshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type ociFixture struct {
	path, rootDigest string
	content          map[string][]byte
}

func buildOCI(t *testing.T) ociFixture {
	t.Helper()
	root := t.TempDir()
	rawLayer := []byte("verified unpacked layer bytes")
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	if _, err := gz.Write(rawLayer); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	compressed := packed.Bytes()
	config := marshal(t, map[string]any{"rootfs": map[string]any{"type": "layers", "diff_ids": []string{sha(rawLayer)}}})
	cfg := descriptor{MediaType: "application/vnd.oci.image.config.v1+json", Digest: sha(config), Size: int64(len(config))}
	layer := descriptor{MediaType: "application/vnd.oci.image.layer.v1.tar+gzip", Digest: sha(compressed), Size: int64(len(compressed))}
	manifest := marshal(t, map[string]any{"schemaVersion": 2, "config": cfg, "layers": []descriptor{layer}})
	m := descriptor{MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: sha(manifest), Size: int64(len(manifest))}
	index := marshal(t, map[string]any{"schemaVersion": 2, "manifests": []descriptor{m}})
	paths := map[string][]byte{
		"oci-layout": []byte(`{"imageLayoutVersion":"1.0.0"}`), "index.json": index,
		"blobs/sha256/" + cfg.Digest[7:]:   config,
		"blobs/sha256/" + layer.Digest[7:]: compressed,
		"blobs/sha256/" + m.Digest[7:]:     manifest,
	}
	archivePath := filepath.Join(root, "export.tar")
	writeTar(t, archivePath, paths)
	return ociFixture{path: archivePath, rootDigest: m.Digest, content: paths}
}
func writeTar(t *testing.T, path string, entries map[string][]byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	for name, data := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestVerifiesAllBlobsAndUnpackedLayer(t *testing.T) {
	f := buildOCI(t)
	result, err := verify(f.path, f.rootDigest)
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifests != 1 || result.Layers != 1 || result.VerifiedBlobs != 3 || result.Result != "verified" {
		t.Fatalf("unexpected receipt: %+v", result)
	}
	var out bytes.Buffer
	if err := run([]string{"--archive", f.path, "--image-digest", f.rootDigest}, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"result": "verified"`)) {
		t.Fatal("missing receipt")
	}
}
func TestRejectsMissingAndCorruptLayer(t *testing.T) {
	f := buildOCI(t)
	for name := range f.content {
		if bytes.Contains([]byte(name), []byte("blobs/sha256/")) && bytes.Equal(f.content[name][:2], []byte{0x1f, 0x8b}) {
			delete(f.content, name)
			break
		}
	}
	writeTar(t, f.path, f.content)
	if _, err := verify(f.path, f.rootDigest); err == nil {
		t.Fatal("missing compressed layer accepted")
	}
	f = buildOCI(t)
	for name, data := range f.content {
		if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
			data[3] ^= 0xff
			f.content[name] = data
			break
		}
	}
	writeTar(t, f.path, f.content)
	if _, err := verify(f.path, f.rootDigest); err == nil {
		t.Fatal("changed compressed layer accepted")
	}
}
func TestRejectsBadUncompressedDiffIDAndIdentity(t *testing.T) {
	f := buildOCI(t)
	for name, data := range f.content {
		var cfg map[string]any
		if json.Unmarshal(data, &cfg) == nil && cfg["rootfs"] != nil {
			cfg["rootfs"] = map[string]any{"type": "layers", "diff_ids": []string{"sha256:" + hex.EncodeToString(make([]byte, 32))}}
			newCfg := marshal(t, cfg)
			newName := "blobs/sha256/" + sha(newCfg)[7:]
			// A conflicting blob with the correct hash still cannot repair a stale manifest reference.
			delete(f.content, name)
			f.content[newName] = newCfg
			break
		}
	}
	writeTar(t, f.path, f.content)
	if _, err := verify(f.path, f.rootDigest); err == nil {
		t.Fatal("mismatched config accepted")
	}
	f = buildOCI(t)
	if _, err := verify(f.path, "sha256:"+hex.EncodeToString(make([]byte, 32))); err == nil {
		t.Fatal("unrelated image digest accepted")
	}
}
func TestRejectsUnsafeArchiveMember(t *testing.T) {
	f := buildOCI(t)
	f.content["../../escape"] = []byte("not allowed")
	writeTar(t, f.path, f.content)
	if _, err := verify(f.path, f.rootDigest); err == nil {
		t.Fatal("tar traversal accepted")
	}
}
