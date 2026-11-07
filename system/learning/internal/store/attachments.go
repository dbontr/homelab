package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

const maxAttachmentBytes = 10 << 20

func verifiedMime(body []byte, claimed string) (string, error) {
	sniffed := http.DetectContentType(body)
	switch {
	case bytes.HasPrefix(body, []byte("\xff\xd8\xff")) && claimed == "image/jpeg":
		return claimed, nil
	case bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) && claimed == "image/png":
		return claimed, nil
	case len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP" && claimed == "image/webp":
		return claimed, nil
	case len(body) >= 12 && string(body[4:8]) == "ftyp" && (bytes.Equal(body[8:12], []byte("heic")) || bytes.Equal(body[8:12], []byte("heix")) || bytes.Equal(body[8:12], []byte("mif1"))) && claimed == "image/heic":
		return claimed, nil
	case bytes.HasPrefix(body, []byte("%PDF-")) && claimed == "application/pdf":
		return claimed, nil
	case strings.HasPrefix(sniffed, "text/plain") && (claimed == "text/plain" || claimed == "text/markdown"):
		return claimed, nil
	default:
		return "", fmt.Errorf("unsupported file contents or MIME mismatch: %s", claimed)
	}
}
func attachmentPublic(db *database, id string) Value {
	r := ensureRow(getRow(db, "SELECT id,owner_kind,owner_id,filename,mime_type,sha256,size_bytes,note,created_at FROM attachments WHERE id=?", id), "attachment", id)
	out := Value(r)
	out["annotations"] = mapRows(getRows(db, "SELECT id,text,method,uncertainty,created_at FROM attachment_annotations WHERE attachment_id=? ORDER BY rowid", id))
	return out
}
func (s *Store) UploadAttachment(ownerKind, ownerID, filename, claimedMime, encoded, note string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(ownerKind, "problem", "session", "source"), "invalid attachment owner kind")
		filename = filepath.Base(strings.ReplaceAll(filename, "\\", "/"))
		insist(filename != "." && filename != "/" && len(filename) > 0 && len(filename) <= 250 && !strings.ContainsRune(filename, 0), "invalid filename")
		note = strings.TrimSpace(note)
		insist(len(note) <= 4000, "attachment note too long")
		insist(len(encoded) > 0 && len(encoded) <= ((maxAttachmentBytes+2)/3)*4+16, "attachment exceeds 10 MiB")
		binary, err := base64.StdEncoding.Strict().DecodeString(encoded)
		insist(err == nil && len(binary) > 0 && len(binary) <= maxAttachmentBytes, "invalid or oversized base64 attachment")
		mime, err := verifiedMime(binary, claimedMime)
		insist(err == nil, fmt.Sprintf("file signature does not match declared MIME: %s", claimedMime))
		switch ownerKind {
		case "problem":
			ensureRow(getRow(db, "SELECT 1 FROM problems WHERE id=?", ownerID), "problem", ownerID)
		case "session":
			ensureRow(getRow(db, "SELECT 1 FROM learning_sessions WHERE id=?", ownerID), "learning session", ownerID)
		case "source":
			ensureRow(getRow(db, "SELECT 1 FROM knowledge_sources WHERE id=?", ownerID), "knowledge source", ownerID)
		}
		sum := sha256.Sum256(binary)
		sha := hex.EncodeToString(sum[:])
		previous := getRow(db, "SELECT id FROM attachments WHERE owner_kind=? AND owner_id=? AND sha256=?", ownerKind, ownerID, sha)
		if previous != nil {
			out := attachmentPublic(db, strVal(previous["id"]))
			out["duplicate"] = true
			return out
		}
		id := uuid()
		exec(db, "INSERT INTO attachments VALUES(?,?,?,?,?,?,?,?,?,?)", id, ownerKind, ownerID, filename, mime, sha, binary, len(binary), note, now())
		out := attachmentPublic(db, id)
		out["duplicate"] = false
		return out
	})
}
func (s *Store) ListAttachments(ownerKind, ownerID string) (Value, error) {
	return s.with(func(db *database) Value {
		insist(valid(ownerKind, "problem", "session", "source"), "invalid attachment owner kind")
		out := []Value{}
		for _, r := range getRows(db, "SELECT id FROM attachments WHERE owner_kind=? AND owner_id=? ORDER BY created_at,id", ownerKind, ownerID) {
			out = append(out, attachmentPublic(db, strVal(r["id"])))
		}
		return Value{"attachments": out}
	})
}
func (s *Store) GetAttachment(id string, includeOriginal bool) (Value, error) {
	return s.with(func(db *database) Value {
		out := attachmentPublic(db, id)
		if includeOriginal {
			r := ensureRow(getRow(db, "SELECT data,sha256,size_bytes FROM attachments WHERE id=?", id), "attachment", id)
			data, ok := r["data"].([]byte)
			insist(ok && len(data) > 0 && len(data) == intVal(r["size_bytes"]), "original attachment size mismatch or data unavailable")
			digest := sha256.Sum256(data)
			insist(hex.EncodeToString(digest[:]) == strVal(r["sha256"]), "original attachment checksum mismatch")
			out["base64"] = base64.StdEncoding.EncodeToString(data)
			out["integrity_verified"] = true
		}
		return out
	})
}
func (s *Store) AnnotateAttachment(id, text, method, uncertainty string) (Value, error) {
	return s.with(func(db *database) Value {
		ensureRow(getRow(db, "SELECT 1 FROM attachments WHERE id=?", id), "attachment", id)
		insist(valid(method, "model_interpretation", "learner_transcription", "verified_transcription", "observation"), "invalid annotation method")
		text = mustString(text, "annotation text")
		insist(len(uncertainty) <= 2000, "uncertainty note too long")
		annotationID := uuid()
		exec(db, "INSERT INTO attachment_annotations VALUES(?,?,?,?,?,?)", annotationID, id, text, method, uncertainty, now())
		return Value{"annotation_id": annotationID, "attachment": attachmentPublic(db, id), "mastery_unchanged": true}
	})
}
