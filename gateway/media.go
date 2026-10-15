package gateway

// Adapted from go_tg_assistant smart_media_downloader.go at 1d7ca224:
// use the existing account client and the document/photo's DC, not a new login.
import (
	"context"
	"errors"
	"fmt"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxMediaBytes int64 = 64 << 20

type telegramMedia struct {
	Kind, Mime, Name string
	Size             int64
	DC               int
	Location         tg.InputFileLocationClass
}

func messageMedia(m *tg.Message) *telegramMedia {
	switch v := m.Media.(type) {
	case *tg.MessageMediaPhoto:
		p, ok := v.Photo.(*tg.Photo)
		if !ok {
			return nil
		}
		out := &telegramMedia{Kind: "photo", Mime: "image/jpeg", Name: "photo.jpg", DC: p.DCID}
		thumb := ""
		for _, s := range p.Sizes {
			switch s := s.(type) {
			case *tg.PhotoSize:
				if int64(s.Size) > out.Size {
					out.Size = int64(s.Size)
					thumb = s.Type
				}
			case *tg.PhotoSizeProgressive:
				for _, size := range s.Sizes {
					if int64(size) > out.Size {
						out.Size = int64(size)
						thumb = s.Type
					}
				}
			}
		}
		if thumb == "" {
			return nil
		}
		out.Location = &tg.InputPhotoFileLocation{ID: p.ID, AccessHash: p.AccessHash, FileReference: p.FileReference, ThumbSize: thumb}
		return out
	case *tg.MessageMediaDocument:
		d, ok := v.Document.(*tg.Document)
		if !ok {
			return nil
		}
		out := &telegramMedia{Kind: "file", Mime: d.MimeType, Name: "attachment", Size: d.Size, DC: d.DCID, Location: &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference}}
		if strings.HasPrefix(d.MimeType, "image/") {
			out.Kind = "photo"
		}
		if strings.HasPrefix(d.MimeType, "video/") {
			out.Kind = "video"
		}
		if strings.HasPrefix(d.MimeType, "audio/") {
			out.Kind = "audio"
		}
		for _, attr := range d.Attributes {
			switch attr := attr.(type) {
			case *tg.DocumentAttributeAudio:
				out.Kind = "audio"
				if out.Mime == "application/ogg" {
					out.Mime = "audio/ogg"
				}
			case *tg.DocumentAttributeFilename:
				out.Name = filepath.Base(strings.ReplaceAll(attr.FileName, "\\", "/"))
			case *tg.DocumentAttributeSticker:
				out.Kind = "sticker"
			}
		}
		// Attribute ordering is not a protocol guarantee; sticker wins over video/audio.
		for _, attr := range d.Attributes {
			if _, ok := attr.(*tg.DocumentAttributeSticker); ok {
				out.Kind = "sticker"
			}
		}
		if out.Name == "attachment" {
			switch out.Mime {
			case "image/webp":
				out.Name = "sticker.webp"
			case "application/x-tgsticker":
				out.Name = "sticker.tgs"
			case "video/webm":
				out.Name = "sticker.webm"
			}
		}
		return out
	}
	return nil
}

type limitedMediaWriter struct {
	w         io.Writer
	remaining int64
}

func (w *limitedMediaWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("MEDIA_TOO_LARGE")
	}
	n, e := w.w.Write(p)
	w.remaining -= int64(n)
	return n, e
}

func (a *TelegramAdapter) mediaHTTP(w http.ResponseWriter, r *http.Request) {
	gid, mid, ok := strings.Cut(r.PathValue("id"), "_")
	n, e := strconv.Atoi(mid)
	if !ok || e != nil || n <= 0 || strconv.Itoa(n) != mid {
		fail(w, &APIError{404, "MEDIA_EXPIRED", 0})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	s := a.service
	// Copy the eligible members while locked. No network transfer holds this mutex.
	s.mu.Lock()
	g := s.state.Groups[gid]
	candidates := map[string]tg.InputChannel{}
	if g != nil {
		for aid, hash := range g.AccessHashes {
			acct := s.state.Accounts[aid]
			if acct != nil && acct.Status == "online" && g.Members[acct.PlatformID] {
				candidates[aid] = tg.InputChannel{ChannelID: g.ChannelID, AccessHash: hash}
			}
		}
	}
	s.mu.Unlock()
	if g == nil {
		fail(w, &APIError{404, "MEDIA_EXPIRED", 0})
		return
	}
	ids := make([]string, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var live *onlineSession
	var channel tg.InputChannel
	for _, id := range ids {
		if session, err := a.online(id); err == nil {
			live = session
			channel = candidates[id]
			break
		}
	}
	if live == nil {
		fail(w, &APIError{409, "ACCOUNT_OFFLINE", 0})
		return
	}
	// Refresh file_reference from the original message on every attempt; old URLs
	// survive gateway restarts without storing Telegram access hashes in the browser.
	messages, err := live.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &channel, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: n}}})
	if err != nil {
		fail(w, platformError(err))
		return
	}
	var media *telegramMedia
	var items []tg.MessageClass
	switch ms := messages.(type) {
	case *tg.MessagesChannelMessages:
		items = ms.Messages
	case *tg.MessagesMessages:
		items = ms.Messages
	case *tg.MessagesMessagesSlice:
		items = ms.Messages
	}
	for _, v := range items {
		if m, ok := v.(*tg.Message); ok && m.ID == n {
			media = messageMedia(m)
		}
	}
	if media == nil {
		fail(w, &APIError{404, "MEDIA_EXPIRED", 0})
		return
	}
	if media.Size > maxMediaBytes {
		fail(w, &APIError{413, "MEDIA_TOO_LARGE", 0})
		return
	}
	pool, err := live.client.DC(ctx, media.DC, 1)
	if err != nil {
		fail(w, platformError(err))
		return
	}
	defer pool.Close()
	f, err := os.CreateTemp("", "kapibala-media-*")
	if err != nil {
		fail(w, &APIError{500, "MEDIA_STORAGE_ERROR", 0})
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	_, err = downloader.NewDownloader().Download(a.fencedClient(tg.NewClient(pool)), media.Location).Stream(ctx, &limitedMediaWriter{f, maxMediaBytes})
	if err != nil {
		fail(w, platformError(err))
		return
	}
	stat, err := f.Stat()
	if err != nil {
		fail(w, &APIError{500, "MEDIA_STORAGE_ERROR", 0})
		return
	}
	if media.Size > 0 && stat.Size() != media.Size {
		fail(w, &APIError{502, "MEDIA_INCOMPLETE", 0})
		return
	}
	w.Header().Set("Content-Type", media.Mime)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": media.Name}))
	w.Header().Set("X-Media-Kind", media.Kind)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, fmt.Sprint(n), stat.ModTime(), f)
}
