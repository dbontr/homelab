// Command oci-archive-verify checks the complete content graph of an OCI image archive.
// It never extracts layers or modifies a containerd content store.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxJSON = 4 << 20
const maxUnpackedLayer = 8 << 30

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type blob struct {
	offset int64
	size   int64
	data   []byte
}

type receipt struct {
	ImageDigest     string `json:"image_digest,omitempty"`
	Manifests       int    `json:"manifests"`
	Layers          int    `json:"layers"`
	VerifiedBlobs   int    `json:"verified_blobs"`
	CompressedBytes int64  `json:"compressed_bytes"`
	Result          string `json:"result"`
}

type archive struct {
	file    *os.File
	blobs   map[string]blob
	visited map[string]bool
	active  map[string]bool
	receipt receipt
}

func validDigest(digest string) bool {
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		return false
	}
	value := digest[7:]
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == value
}

func safeTarName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsRune(name, '\\') || strings.ContainsRune(name, '\x00') {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func openArchive(path string) (*archive, []byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	closeOnFailure := func(err error) (*archive, []byte, error) { _ = file.Close(); return nil, nil, err }
	a := &archive{file: file, blobs: map[string]blob{}, visited: map[string]bool{}, active: map[string]bool{}}
	reader := tar.NewReader(file)
	var index, layout []byte
	names := map[string]bool{}
	count := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return closeOnFailure(err)
		}
		count++
		if count > 100000 {
			return closeOnFailure(errors.New("too many archive entries"))
		}
		name := header.Name
		if strings.HasSuffix(name, "/") && header.Typeflag == tar.TypeDir {
			continue
		}
		if !safeTarName(name) || names[name] {
			return closeOnFailure(fmt.Errorf("unsafe or duplicate archive entry: %q", name))
		}
		names[name] = true
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return closeOnFailure(fmt.Errorf("nonregular archive entry: %q", name))
		}
		offset, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return closeOnFailure(err)
		}
		if header.Size < 0 {
			return closeOnFailure(errors.New("negative archive member size"))
		}
		if name == "index.json" || name == "oci-layout" {
			if header.Size > maxJSON {
				return closeOnFailure(errors.New("OCI metadata exceeds size limit"))
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				return closeOnFailure(err)
			}
			if name == "index.json" {
				index = data
			} else {
				layout = data
			}
			continue
		}
		if strings.HasPrefix(name, "blobs/sha256/") {
			digest := "sha256:" + strings.TrimPrefix(name, "blobs/sha256/")
			if !validDigest(digest) {
				return closeOnFailure(errors.New("invalid OCI blob path"))
			}
			h := sha256.New()
			var data []byte
			if header.Size <= maxJSON {
				data, err = io.ReadAll(io.TeeReader(reader, h))
			} else {
				_, err = io.Copy(h, reader)
			}
			if err != nil {
				return closeOnFailure(err)
			}
			if "sha256:"+hex.EncodeToString(h.Sum(nil)) != digest {
				return closeOnFailure(fmt.Errorf("blob digest mismatch: %s", digest))
			}
			a.blobs[digest] = blob{offset: offset, size: header.Size, data: data}
			a.receipt.VerifiedBlobs++
			continue
		}
		if _, err := io.Copy(io.Discard, reader); err != nil {
			return closeOnFailure(err)
		}
	}
	if len(index) == 0 || len(layout) == 0 {
		return closeOnFailure(errors.New("OCI layout or index missing"))
	}
	var l struct {
		ImageLayoutVersion string `json:"imageLayoutVersion"`
	}
	if err := json.Unmarshal(layout, &l); err != nil || l.ImageLayoutVersion != "1.0.0" {
		return closeOnFailure(errors.New("invalid OCI layout version"))
	}
	return a, index, nil
}

func (a *archive) checkedBlob(d descriptor) (blob, error) {
	if !validDigest(d.Digest) || d.Size < 0 {
		return blob{}, errors.New("invalid OCI descriptor")
	}
	b, ok := a.blobs[d.Digest]
	if !ok {
		return blob{}, fmt.Errorf("missing OCI blob %s", d.Digest)
	}
	if b.size != d.Size {
		return blob{}, fmt.Errorf("OCI descriptor size mismatch: %s", d.Digest)
	}
	return b, nil
}

func (a *archive) descriptorJSON(d descriptor, into any) error {
	b, err := a.checkedBlob(d)
	if err != nil {
		return err
	}
	if b.size > maxJSON {
		return errors.New("OCI JSON descriptor exceeds size limit")
	}
	return json.Unmarshal(b.data, into)
}

func (a *archive) verifyLayer(d descriptor, diffID string) error {
	b, err := a.checkedBlob(d)
	if err != nil {
		return err
	}
	if !validDigest(diffID) {
		return errors.New("invalid OCI uncompressed layer digest")
	}
	source := io.NewSectionReader(a.file, b.offset, b.size)
	var input io.Reader = source
	var decompressor *gzip.Reader
	switch d.MediaType {
	case "application/vnd.oci.image.layer.v1.tar+gzip", "application/vnd.docker.image.rootfs.diff.tar.gzip":
		decompressor, err = gzip.NewReader(source)
		if err != nil {
			return fmt.Errorf("unreadable gzip layer: %w", err)
		}
		defer decompressor.Close()
		decompressor.Multistream(false)
		input = decompressor
	case "application/vnd.oci.image.layer.v1.tar", "application/vnd.oci.image.layer.nondistributable.v1.tar":
	default:
		return fmt.Errorf("unsupported layer media type: %s", d.MediaType)
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(input, maxUnpackedLayer+1))
	if err != nil {
		return fmt.Errorf("layer decompression failed: %w", err)
	}
	if n > maxUnpackedLayer {
		return errors.New("unpacked layer exceeds limit")
	}
	if "sha256:"+hex.EncodeToString(h.Sum(nil)) != diffID {
		return fmt.Errorf("unpacked layer digest mismatch: %s", d.Digest)
	}
	if decompressor != nil {
		if source.Size() != b.size {
			return errors.New("unreadable layer size")
		}
		// One gzip stream must consume the exact compressed layer payload.
		if sourcePosition, err := source.Seek(0, io.SeekCurrent); err != nil || sourcePosition != b.size {
			return errors.New("trailing gzip layer content")
		}
	}
	a.receipt.Layers++
	a.receipt.CompressedBytes += b.size
	return nil
}

func (a *archive) traverse(d descriptor) error {
	if a.visited[d.Digest] {
		return nil
	}
	if a.active[d.Digest] {
		return errors.New("cyclic OCI descriptor graph")
	}
	a.active[d.Digest] = true
	defer delete(a.active, d.Digest)
	switch d.MediaType {
	case "application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json":
		var index struct {
			Manifests []descriptor `json:"manifests"`
		}
		if err := a.descriptorJSON(d, &index); err != nil {
			return err
		}
		if len(index.Manifests) == 0 {
			return errors.New("empty OCI image index")
		}
		for _, child := range index.Manifests {
			if err := a.traverse(child); err != nil {
				return err
			}
		}
	case "application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json":
		var manifest struct {
			Config descriptor   `json:"config"`
			Layers []descriptor `json:"layers"`
		}
		if err := a.descriptorJSON(d, &manifest); err != nil {
			return err
		}
		var config struct {
			RootFS struct {
				DiffIDs []string `json:"diff_ids"`
			} `json:"rootfs"`
		}
		if err := a.descriptorJSON(manifest.Config, &config); err != nil {
			return err
		}
		if len(config.RootFS.DiffIDs) != len(manifest.Layers) {
			return errors.New("OCI layer count differs from config rootfs")
		}
		for i, layer := range manifest.Layers {
			if err := a.verifyLayer(layer, config.RootFS.DiffIDs[i]); err != nil {
				return err
			}
		}
		a.receipt.Manifests++
	default:
		return fmt.Errorf("unrecognized OCI manifest type: %s", d.MediaType)
	}
	a.visited[d.Digest] = true
	return nil
}

func verify(path, expected string) (receipt, error) {
	if expected != "" && !validDigest(expected) {
		return receipt{}, errors.New("expected digest must be a full sha256 descriptor")
	}
	a, index, err := openArchive(path)
	if err != nil {
		return receipt{}, err
	}
	defer a.file.Close()
	var roots struct {
		Manifests []descriptor `json:"manifests"`
	}
	if err := json.Unmarshal(index, &roots); err != nil {
		return receipt{}, err
	}
	if len(roots.Manifests) == 0 {
		return receipt{}, errors.New("OCI index has no image references")
	}
	found := false
	for _, root := range roots.Manifests {
		if root.Digest == expected {
			found = true
		}
		if err := a.traverse(root); err != nil {
			return receipt{}, err
		}
	}
	if expected != "" && !found {
		return receipt{}, errors.New("expected image digest is absent from OCI index")
	}
	if a.receipt.Manifests == 0 {
		return receipt{}, errors.New("archive contains no image manifest")
	}
	a.receipt.ImageDigest = expected
	a.receipt.Result = "verified"
	return a.receipt, nil
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("oci-archive-verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	archivePath := flags.String("archive", "", "OCI tar export to inspect without importing")
	digest := flags.String("image-digest", "", "Exact index/manifest digest to require")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *archivePath == "" || *digest == "" {
		return errors.New("require --archive and --image-digest")
	}
	result, err := verify(*archivePath, *digest)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
