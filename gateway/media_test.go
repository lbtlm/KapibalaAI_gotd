package gateway

import (
	"github.com/gotd/td/tg"
	"testing"
)

// Pure Telegram update conversion; no account connection or fabricated RPC success.
func TestIncomingStickerRetainsMediaInEvent(t *testing.T) {
	s := &Service{state: initialState()}
	s.state.Accounts["reader"] = &account{PlatformID: "10"}
	s.state.Groups["tg-20"] = &group{}
	a := &TelegramAdapter{service: s}
	a.platformMessage("reader", &tg.Message{ID: 30, Date: 100, PeerID: &tg.PeerChannel{ChannelID: 20}, FromID: &tg.PeerUser{UserID: 40}, Media: &tg.MessageMediaDocument{Document: &tg.Document{ID: 50, MimeType: "image/webp", Size: 123, Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{Alt: "🙂"}}}}})
	if len(s.state.Events) != 1 {
		t.Fatal("missing message event")
	}
	data := s.state.Events[0].Data
	if data["mediaUrl"] != "/media/tg-20_30" || data["mediaKind"] != "sticker" || data["mediaMime"] != "image/webp" {
		t.Fatalf("sticker became blank text: %v", data)
	}
	a.platformMessage("reader", &tg.Message{ID: 30, Date: 100, PeerID: &tg.PeerChannel{ChannelID: 20}})
	if len(s.state.Events) != 1 {
		t.Fatal("duplicate update")
	}
}

func TestMediaFormatsPreserveFileLocation(t *testing.T) {
	photo := messageMedia(&tg.Message{Media: &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 1, AccessHash: 2, DCID: 4, FileReference: []byte{3}, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "s", Size: 20}, &tg.PhotoSizeProgressive{Type: "y", Sizes: []int{50, 100}}}}}})
	if photo == nil || photo.Size != 100 || photo.Location.(*tg.InputPhotoFileLocation).ThumbSize != "y" || photo.DC != 4 {
		t.Fatal("photo location lost", photo)
	}
	for _, typ := range []string{"image/webp", "application/x-tgsticker", "video/webm"} {
		media := messageMedia(&tg.Message{Media: &tg.MessageMediaDocument{Document: &tg.Document{ID: 1, AccessHash: 2, DCID: 4, FileReference: []byte{3}, MimeType: typ, Size: 123, Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeSticker{Alt: "x"}, &tg.DocumentAttributeFilename{FileName: "../sticker"}}}}})
		if media.Kind != "sticker" || media.Mime != typ || media.Name != "sticker" || len(media.Location.(*tg.InputDocumentFileLocation).FileReference) != 1 {
			t.Fatal("sticker format or location lost", media)
		}
	}
}
