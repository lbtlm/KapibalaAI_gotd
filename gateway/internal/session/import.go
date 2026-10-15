package session

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	_ "github.com/glebarez/go-sqlite"
	"github.com/gotd/td/crypto"
	"github.com/gotd/td/session"
	"github.com/gotd/td/session/tdesktop"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ImportSession converts only the explicitly supplied isolated session file.
// It never writes the input, and rejects archives containing multiple identities.
func ImportSession(path, format, passcode string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read isolated session: %w", err)
	}
	var data *session.Data
	switch format {
	case "gotdJSON":
		storage := &session.StorageMemory{}
		_ = storage.StoreSession(context.Background(), b)
		data, err = (&session.Loader{Storage: storage}).Load(context.Background())
	case "TelethonSQLite":
		data, err = loadTelethonSession(path)
	case "tdataZIP":
		data, err = loadTdataZIP(b, passcode)
	default:
		return nil, fmt.Errorf("unsupported session format %q", format)
	}
	if err != nil {
		return nil, fmt.Errorf("convert %s session: %w", format, err)
	}
	if data.DC <= 0 || data.Addr == "" || len(data.AuthKey) != 256 || len(data.AuthKeyID) != 8 {
		return nil, fmt.Errorf("invalid Telegram authorization data")
	}
	if format == "gotdJSON" {
		return b, nil
	}
	storage := &session.StorageMemory{}
	if err = (&session.Loader{Storage: storage}).Save(context.Background(), data); err != nil {
		return nil, err
	}
	return storage.Bytes(nil)
}

// Adapted verbatim conversion body from internal/tg/telethon.go:224.
func loadTelethonSession(path string) (*session.Data, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()
	row := db.QueryRow(`SELECT dc_id, server_address, port, auth_key FROM sessions LIMIT 1`)
	var dcID, port int
	var addr string
	var authKey []byte
	if err := row.Scan(&dcID, &addr, &port, &authKey); err != nil {
		return nil, fmt.Errorf("scan row: %w", err)
	}
	if len(authKey) != 256 {
		return nil, fmt.Errorf("unexpected auth_key length: got %d, want 256", len(authKey))
	}
	var key crypto.Key
	copy(key[:], authKey)
	auth := key.WithID()
	return &session.Data{DC: dcID, Addr: net.JoinHostPort(addr, strconv.Itoa(port)), AuthKey: auth.Value[:], AuthKeyID: auth.ID[:]}, nil
}

// Reuses tdata.go's extraction -> tdesktop.Read -> TDesktopSession conversion.
func loadTdataZIP(b []byte, passcode string) (*session.Data, error) {
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "gateway-tdata-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	var roots []string
	var total int64
	for _, f := range z.File {
		clean := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || strings.Contains(clean, ":") || f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe tdata archive path")
		}
		dest := filepath.Join(dir, clean)
		rel, _ := filepath.Rel(dir, dest)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("unsafe archive path")
		}
		if f.FileInfo().IsDir() {
			if err = os.MkdirAll(dest, 0700); err != nil {
				return nil, err
			}
			continue
		}
		if f.UncompressedSize64 > 32<<20 || total+int64(f.UncompressedSize64) > 128<<20 {
			return nil, fmt.Errorf("tdata archive too large")
		}
		total += int64(f.UncompressedSize64)
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return nil, err
		}
		src, e := f.Open()
		if e != nil {
			return nil, e
		}
		out, e := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if e != nil {
			src.Close()
			return nil, e
		}
		_, e = io.Copy(out, io.LimitReader(src, 32<<20+1))
		src.Close()
		out.Close()
		if e != nil {
			return nil, e
		}
		if base := filepath.Base(dest); base == "key_data" || base == "key_datas" {
			roots = append(roots, filepath.Dir(dest))
		}
	}
	if len(roots) != 1 {
		return nil, fmt.Errorf("expected exactly one tdata root")
	}
	accounts, err := tdesktop.Read(roots[0], []byte(passcode))
	if err != nil {
		return nil, err
	}
	if len(accounts) != 1 {
		return nil, fmt.Errorf("expected exactly one tdata account")
	}
	return session.TDesktopSession(accounts[0])
}
