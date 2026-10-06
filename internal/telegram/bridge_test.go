package telegram

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/secretstore"
	tg "github.com/mymmrac/telego"
)

type fakeClient struct {
	mu                                            sync.Mutex
	sends, edits, markupEdits, documents, answers int
	texts                                         []string
	messages                                      []outgoingText
	editIDs                                       []int
	keyboards                                     []*tg.InlineKeyboardMarkup
	documentBytes                                 []string
	sendErr                                       error
	webhook                                       bool
	takeovers                                     int
	updates                                       chan []tg.Update
	download                                      []byte
	downloadErr                                   error
	downloadIDs                                   []string
}

func (f *fakeClient) Me(context.Context) (*tg.User, error) {
	return &tg.User{ID: 123, Username: "test_bot"}, nil
}
func (f *fakeClient) Webhook(context.Context) (bool, error) { return f.webhook, nil }
func (f *fakeClient) TakeOver(context.Context) error        { f.takeovers++; return nil }
func (f *fakeClient) Updates(ctx context.Context, _ int) ([]tg.Update, error) {
	select {
	case v := <-f.updates:
		return v, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (f *fakeClient) Send(_ context.Context, _ int64, message outgoingText, keys *tg.InlineKeyboardMarkup) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends++
	f.texts = append(f.texts, message.Text)
	f.messages = append(f.messages, message)
	f.keyboards = append(f.keyboards, keys)
	return f.sends, f.sendErr
}
func (f *fakeClient) Edit(_ context.Context, _ int64, id int, message outgoingText, keys *tg.InlineKeyboardMarkup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits++
	f.editIDs = append(f.editIDs, id)
	f.texts = append(f.texts, message.Text)
	f.messages = append(f.messages, message)
	f.keyboards = append(f.keyboards, keys)
	return nil
}
func (f *fakeClient) EditMarkup(_ context.Context, _ int64, _ int, keys *tg.InlineKeyboardMarkup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markupEdits++
	f.keyboards = append(f.keyboards, keys)
	return nil
}
func (f *fakeClient) Document(_ context.Context, _ int64, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.documents++
	f.documentBytes = append(f.documentBytes, string(data))
	return nil
}
func (f *fakeClient) Download(_ context.Context, id string, path string) error {
	f.downloadIDs = append(f.downloadIDs, id)
	if f.downloadErr != nil {
		return f.downloadErr
	}
	return os.WriteFile(path, f.download, 0600)
}
func (f *fakeClient) Answer(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers++
	return nil
}
func (f *fakeClient) Commands(context.Context) error { return nil }
func testBridge(t *testing.T, h Host) (*Bridge, *fakeClient) {
	t.Helper()
	if h.Snapshot == nil {
		h.Snapshot = func() api.Snapshot { return api.Snapshot{Connection: "connected"} }
	}
	b, e := Open(t.TempDir(), h)
	if e != nil {
		t.Fatal(e)
	}
	f := &fakeClient{updates: make(chan []tg.Update, 10), download: []byte("attachment fixture")}
	b.newClient = func(string) (client, error) { return f, nil }
	b.secrets = secretstore.Functions{
		SaveFunc:   func(string, string) error { return nil },
		LoadFunc:   func(string) (string, error) { return "fixture-token", nil },
		DeleteFunc: func(string) error { return nil },
	}
	t.Cleanup(b.Close)
	return b, f
}
func message(id int, chat, user int64, text string) tg.Update {
	return tg.Update{UpdateID: id, Message: &tg.Message{MessageID: id, Chat: tg.Chat{ID: chat, Type: "private"}, From: &tg.User{ID: user, FirstName: "Owner"}, Text: text}}
}
func paired(b *Bridge) {
	b.state.ChatID, b.state.UserID, b.state.BotID, b.state.Enabled = 10, 20, 123, true
}
func TestPairingRequiresNoncePrivateChatAndDesktopConfirmation(t *testing.T) {
	b, f := testBridge(t, Host{})
	b.state.Bot = "test_bot"
	b.nonce = "secret-nonce"
	b.expires = time.Now().Add(time.Minute)
	b.input(t.Context(), f, message(1, 10, 20, "/start wrong"))
	if b.Status().Candidate != "" {
		t.Fatal("wrong nonce accepted")
	}
	group := message(2, 10, 20, "/start secret-nonce")
	group.Message.Chat.Type = "group"
	b.input(t.Context(), f, group)
	if b.Status().Candidate != "" {
		t.Fatal("group accepted")
	}
	b.input(t.Context(), f, message(3, 10, 20, "/start secret-nonce"))
	if b.Status().Paired || b.Status().Candidate == "" {
		t.Fatal("pairing did not wait for desktop")
	}
	b.input(t.Context(), f, message(4, 99, 88, "/start secret-nonce"))
	if b.candidate.From.ID != 20 {
		t.Fatal("candidate was replaced")
	}
	s, e := b.Confirm()
	if e != nil || !s.Paired || b.state.UserID != 20 {
		t.Fatal("confirm failed", e)
	}
	if _, e = b.Confirm(); e == nil {
		t.Fatal("consumed nonce accepted")
	}
}
func TestOwnerInputUsesOriginalIDAndNeverReplaysUnknownAfterRestart(t *testing.T) {
	calls := 0
	var got api.Submission
	h := Host{Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		calls++
		got = in
		return api.Receipt{ID: in.ID, Outcome: "unknown"}, errors.New("lost response")
	}}
	b, f := testBridge(t, h)
	paired(b)
	b.input(t.Context(), f, message(1, 10, 999, "ignored"))
	if calls != 0 {
		t.Fatal("unpaired sender reached Bot")
	}
	u := message(2, 10, 20, "original input")
	b.input(t.Context(), f, u)
	b.input(t.Context(), f, u)
	if calls != 1 || got.ID != "telegram:123:2" || got.Text != "original input" {
		t.Fatal("input identity or deduplication failed")
	}
	restored, e := Open(filepath.Dir(b.path), h)
	if e != nil {
		t.Fatal(e)
	}
	restored.input(t.Context(), f, u)
	if calls != 1 {
		t.Fatal("uncertain input replayed after restart")
	}
	data, _ := os.ReadFile(b.path)
	if strings.Contains(string(data), "original input") || strings.Contains(string(data), "fixture-token") {
		t.Fatal("private text or token persisted in bridge metadata")
	}
}
func TestStreamingEditsOneMessageAndChunksUnicode(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{{ID: "a", Kind: "assistant", Text: "first"}}})
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{{ID: "a", Kind: "assistant", Text: "first and final"}}})
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{{ID: "a", Kind: "assistant", Text: "first and final"}}})
	if f.sends != 1 || f.edits != 1 {
		t.Fatal("stream did not coalesce on stable message", f.sends, f.edits)
	}
	if f.messages[0].Text != "first" || f.messages[1].Text != "first and final" || len(f.messages[0].Entities) != 0 || len(f.messages[1].Entities) != 0 {
		t.Fatal("assistant stream gained a heading or leaked entities")
	}
	text := strings.Repeat("🦉你好", 1500)
	parts := splitText(text)
	if strings.Join(parts, "") != text {
		t.Fatal("text lost")
	}
	for _, p := range parts {
		if len(utf16.Encode([]rune(p))) > 4000 {
			t.Fatal("UTF16 Telegram limit exceeded")
		}
	}
}

func TestHostNoticeNeverMirrorsTextOrFilesButSameTextUserAndReportDo(t *testing.T) {
	artifactReads, imageReads := 0, 0
	b, f := testBridge(t, Host{
		Artifact:    func(string) (string, error) { artifactReads++; return "", errors.New("unexpected host file read") },
		ScreenImage: func(string) ([]byte, error) { imageReads++; return nil, errors.New("unexpected host image read") },
	})
	paired(b)
	const notice = "Task task-42 is completed."
	items := []api.Item{
		{ID: "internal", Kind: "hostNotice", Text: notice, Artifacts: []api.Artifact{{ID: "private-file", Name: "private.txt"}}, Screen: &api.ScreenPresentation{Images: []api.ScreenImage{{ID: "private-image"}}}},
		{ID: "report", Kind: "assistant", Text: "The requested work is ready."},
		{ID: "human", Kind: "user", RequestID: "desktop-human", Text: notice},
	}
	b.mirror(t.Context(), f, api.Snapshot{Items: items})
	b.mirror(t.Context(), f, api.Snapshot{Items: items})
	if f.sends != 2 || f.edits != 0 || f.documents != 0 || artifactReads != 0 || imageReads != 0 {
		t.Fatalf("host notice leaked or visible replies lost: sends=%d edits=%d documents=%d artifactReads=%d imageReads=%d", f.sends, f.edits, f.documents, artifactReads, imageReads)
	}
	checkPlainAssistantMessage(t, f.messages[0], "The requested work is ready.")
	checkMacUserMessage(t, f.messages[1], "You · from Mac", notice)
	if _, exists := b.state.Messages["internal"]; exists {
		t.Fatal("host notice was journaled as a Telegram delivery")
	}
}

func TestMirroredMacIdentityAndPlainAssistantOnSendAndEdit(t *testing.T) {
	for _, tc := range []struct {
		name, title string
		chinese     bool
	}{
		{"English", "You · from Mac", false},
		{"Chinese", "你 · 来自 Mac", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, f := testBridge(t, Host{Chinese: func() bool { return tc.chinese }})
			paired(b)
			body := "first 🦉 line\n`code` <tag> & https://example.com/a?x=1&y=2"
			reply := "Reply 🧭\n**literal Markdown**"
			items := []api.Item{{ID: "mac", Kind: "user", RequestID: "desktop:1", Text: body}, {ID: "reply", Kind: "assistant", Text: reply}}
			b.mirror(t.Context(), f, api.Snapshot{Items: items})
			b.mirror(t.Context(), f, api.Snapshot{Items: items})
			if f.sends != 2 || f.edits != 0 {
				t.Fatalf("duplicate role publication: sends=%d edits=%d", f.sends, f.edits)
			}
			checkMacUserMessage(t, f.messages[0], tc.title, body)
			checkPlainAssistantMessage(t, f.messages[1], reply)
			items[0].Text += "\nupdated"
			items[1].Text += "!"
			b.mirror(t.Context(), f, api.Snapshot{Items: items})
			if f.sends != 2 || f.edits != 2 {
				t.Fatalf("role edit changed identity: sends=%d edits=%d", f.sends, f.edits)
			}
			checkMacUserMessage(t, f.messages[2], tc.title, items[0].Text)
			checkPlainAssistantMessage(t, f.messages[3], items[1].Text)
		})
	}
}

func checkMacUserMessage(t *testing.T, message outgoingText, title, body string) {
	t.Helper()
	if message.Text != title+"\n"+body || len(message.Entities) != 2 {
		t.Fatalf("role text/entities changed: %#v", message)
	}
	if entity := message.Entities[0]; entity.Type != tg.EntityTypeBold || entity.Offset != 0 || entity.Length != utf16Length(title) {
		t.Fatalf("wrong title entity: %#v", entity)
	}
	entity := message.Entities[1]
	if entity.Type != tg.EntityTypeBlockquote || entity.Offset != utf16Length(title+"\n") || entity.Length != utf16Length(body) {
		t.Fatalf("wrong body entity: %#v", entity)
	}
	if utf16Length(message.Text) > 4000 {
		t.Fatalf("Telegram text limit exceeded: %d", utf16Length(message.Text))
	}
}

func checkPlainAssistantMessage(t *testing.T, message outgoingText, body string) {
	t.Helper()
	if message.Text != body || len(message.Entities) != 0 || utf16Length(message.Text) > 4000 {
		t.Fatalf("assistant body was decorated, truncated, or oversized: %#v", message)
	}
}

func TestLongMacPartsKeepIdentityAndAssistantPartsStayPlain(t *testing.T) {
	for _, tc := range []struct {
		role, title string
	}{
		{"user", "You · from Mac"},
		{"assistant", ""},
	} {
		t.Run(tc.role, func(t *testing.T) {
			b, f := testBridge(t, Host{})
			paired(b)
			body := strings.Repeat("🦉<>&`https://example.com`\n", 350)
			item := api.Item{ID: "long", Kind: tc.role, Text: body}
			b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{item}})
			if f.sends < 2 || f.edits != 0 {
				t.Fatalf("long message did not split: sends=%d edits=%d", f.sends, f.edits)
			}
			var restored strings.Builder
			for _, message := range f.messages {
				chunk := message.Text
				if tc.role == "user" {
					chunk = strings.TrimPrefix(message.Text, tc.title+"\n")
					checkMacUserMessage(t, message, tc.title, chunk)
				} else {
					checkPlainAssistantMessage(t, message, chunk)
				}
				restored.WriteString(chunk)
			}
			if restored.String() != body {
				t.Fatal("long body changed during role-aware splitting")
			}
			b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{item}})
			if f.sends != len(f.messages) || f.edits != 0 {
				t.Fatal("repeated long snapshot published again")
			}
		})
	}
}

func TestAssistantPlainUTF16BoundaryKeepsAllTextWithoutEntities(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	body := strings.Repeat("🦉", 2000) // Exactly 4000 UTF-16 units.
	item := api.Item{ID: "boundary", Kind: "assistant", Text: body}
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{item}})
	if f.sends != 1 || f.edits != 0 {
		t.Fatal("plain assistant body was split before its original limit")
	}
	checkPlainAssistantMessage(t, f.messages[0], body)
	item.Text += "尾"
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{item}})
	if f.sends != 2 || f.edits != 0 {
		t.Fatal("streaming across the original limit lost the first part identity")
	}
	checkPlainAssistantMessage(t, f.messages[1], "尾")
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{item}})
	if f.sends != 2 || f.edits != 0 {
		t.Fatal("unchanged assistant boundary was republished")
	}
}

func TestPreviouslyDecoratedAssistantPartsEditInPlaceWithoutLeavingHeader(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	body := strings.Repeat("🦉", 1995) // One plain part, two with the former heading.
	oldChunks := splitTextLimit(body, 4000-utf16Length("Caelis Bot\n"))
	if len(oldChunks) != 2 {
		t.Fatal("fixture does not cross former heading boundary")
	}
	record := delivery{IDs: []int{51, 52}}
	for _, chunk := range oldChunks {
		record.Hashes = append(record.Hashes, outgoingDigest(outgoingText{
			Text:     "Caelis Bot\n" + chunk,
			Entities: []tg.MessageEntity{{Type: tg.EntityTypeBold, Offset: 0, Length: utf16Length("Caelis Bot")}},
		}))
	}
	b.state.Messages["item:old-decorated"] = record
	item := api.Item{ID: "old-decorated", Kind: "assistant", Text: body}
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{item}})
	if f.sends != 0 || f.edits != 2 || f.editIDs[0] != 51 || f.editIDs[1] != 52 {
		t.Fatal("former assistant parts were recreated or left stale", f.sends, f.edits, f.editIDs)
	}
	var restored strings.Builder
	for _, message := range f.messages {
		checkPlainAssistantMessage(t, message, message.Text)
		restored.WriteString(message.Text)
	}
	if restored.String() != body {
		t.Fatal("former heading migration truncated assistant text")
	}
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{item}})
	if f.sends != 0 || f.edits != 2 {
		t.Fatal("migrated assistant history was republished")
	}
}

func TestLegacyRoleDigestDoesNotRepublishHistoryButChangedStreamEditsOriginal(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	b.state.Messages["input:desktop:old"] = delivery{IDs: []int{51}, Hashes: []string{digest("From Mac: unchanged")}}
	b.state.Messages["item:stream"] = delivery{IDs: []int{52}, Hashes: []string{digest("old reply")}}
	items := []api.Item{{ID: "old", Kind: "user", RequestID: "desktop:old", Text: "unchanged"}, {ID: "stream", Kind: "assistant", Text: "old reply"}}
	b.mirror(t.Context(), f, api.Snapshot{Items: items})
	if f.sends != 0 || f.edits != 0 {
		t.Fatal("unchanged pre-format history was republished")
	}
	items[1].Text = "old reply, now complete"
	b.mirror(t.Context(), f, api.Snapshot{Items: items})
	if f.sends != 0 || f.edits != 1 {
		t.Fatal("changed stream did not edit existing Telegram ID")
	}
	checkPlainAssistantMessage(t, f.messages[0], items[1].Text)
}
func TestUnknownTelegramCreateIsNotRetried(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	f.sendErr = &transportError{issue: "network"}
	b.sendText(t.Context(), f, "item:a", 10, "text", nil)
	b.sendText(t.Context(), f, "item:a", 10, "more", nil)
	restored, e := Open(filepath.Dir(b.path), b.host)
	if e != nil {
		t.Fatal(e)
	}
	restored.sendText(t.Context(), f, "item:a", 10, "more", nil)
	if f.sends != 1 || b.Status().Issue != "delivery_uncertain" {
		t.Fatal("unknown send duplicated")
	}
}
func TestAttachmentDownloadIsBoundedAndFilenameCannotEscape(t *testing.T) {
	for _, name := range []string{".", "..", "../..", "\\..", "\x00"} {
		if !filepath.IsLocal(safeName(name)) || safeName(name) == "." {
			t.Fatalf("unsafe attachment filename %q", name)
		}
	}
	var input api.InputFile
	var inputBytes []byte
	b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		input = files[0]
		inputBytes, _ = os.ReadFile(input.Path)
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	}})
	paired(b)
	u := message(1, 10, 20, "")
	u.Message.Document = &tg.Document{FileID: "file", FileName: "../../fixture.txt", FileSize: 12}
	b.input(t.Context(), f, u)
	if input.Name != "fixture.txt" || !strings.HasPrefix(input.Path, b.root+string(os.PathSeparator)) {
		t.Fatal("untrusted filename escaped storage")
	}
	if string(inputBytes) != "attachment fixture" {
		t.Fatal("attachment bytes lost")
	}
	u = message(2, 10, 20, "")
	u.Message.Document = &tg.Document{FileID: "large", FileName: "big.txt", FileSize: maxInputBytes + 1}
	if !b.input(t.Context(), f, u) || b.state.Inputs["telegram:123:2"] != "rejected" {
		t.Fatal("oversized attachment accepted")
	}
}
func TestStickerInputUsesImageBytesAndOriginalReceipt(t *testing.T) {
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 12, 12))); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"static", "animated", "video"} {
		t.Run(format, func(t *testing.T) {
			visual := imageBytes.Bytes()
			if format == "animated" {
				var sample bytes.Buffer
				if err := jpeg.Encode(&sample, image.NewRGBA(image.Rect(0, 0, 12, 12)), nil); err != nil {
					t.Fatal(err)
				}
				visual = sample.Bytes()
			} else if format == "static" || format == "video" {
				var err error
				visual, err = os.ReadFile("../../frontend/public/portraits/caelis-sage-v1/focus.webp")
				if err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
				calls++
				if in.ID != "telegram:123:1" || len(files) != 1 {
					t.Fatal(in, files)
				}
				data, err := os.ReadFile(files[0].Path)
				if err != nil || !bytes.Equal(data, visual) {
					t.Fatal("visual bytes lost", err)
				}
				if format != "static" && !strings.Contains(in.Text, "单帧预览") {
					t.Fatal("motion limitation omitted", in.Text)
				}
				if !strings.Contains(in.Text, "🙂") {
					t.Fatal("emoji metadata omitted", in.Text)
				}
				return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
			}})
			paired(b)
			f.download = visual
			u := message(1, 10, 20, "")
			u.Message.Sticker = &tg.Sticker{FileID: "original", FileSize: len(visual), Emoji: "🙂", IsAnimated: format == "animated", IsVideo: format == "video"}
			if format != "static" {
				u.Message.Sticker.Thumbnail = &tg.PhotoSize{FileID: "preview", FileSize: imageBytes.Len()}
			}
			if !b.input(t.Context(), f, u) || calls != 1 || b.state.Inputs["telegram:123:1"] != "accepted" {
				t.Fatal("sticker not accepted")
			}
			want := "original"
			if format != "static" {
				want = "preview"
			}
			if len(f.downloadIDs) != 1 || f.downloadIDs[0] != want {
				t.Fatal("wrong Telegram visual source", f.downloadIDs)
			}
			b.input(t.Context(), f, u)
			if calls != 1 {
				t.Fatal("original sticker replayed")
			}
		})
	}
}
func TestTelegramPhotoWithCaptionKeepsVisualAndOriginalMessageID(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 7, 7))); err != nil {
		t.Fatal(err)
	}
	var got api.Submission
	b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		got = in
		if len(files) != 1 || files[0].Name != "photo.jpg" {
			t.Fatal(files)
		}
		data, err := os.ReadFile(files[0].Path)
		if err != nil || !bytes.Equal(data, picture.Bytes()) {
			t.Fatal(err)
		}
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	}})
	paired(b)
	f.download = picture.Bytes()
	u := message(12, 10, 20, "")
	u.Message.Caption = "What is in this picture?"
	u.Message.Photo = []tg.PhotoSize{{FileID: "photo-small", FileSize: 40}, {FileID: "photo-large", FileSize: picture.Len()}}
	if !b.input(t.Context(), f, u) || got.ID != "telegram:123:12" || got.Text != u.Message.Caption || len(f.downloadIDs) != 1 || f.downloadIDs[0] != "photo-large" {
		t.Fatal(got, f.downloadIDs)
	}
}
func TestStickerWithoutFrameDownloadFailureAndSizeRejectBeforeSubmit(t *testing.T) {
	for _, issue := range []string{"missing", "download", "oversize"} {
		t.Run(issue, func(t *testing.T) {
			calls := 0
			b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
				calls++
				return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
			}})
			paired(b)
			u := message(1, 10, 20, "")
			u.Message.Sticker = &tg.Sticker{FileID: "animation", IsAnimated: true}
			if issue != "missing" {
				u.Message.Sticker.Thumbnail = &tg.PhotoSize{FileID: "preview", FileSize: 100}
			}
			if issue == "download" {
				f.downloadErr = errors.New("PRIVATE_TOKEN")
			}
			if issue == "oversize" {
				u.Message.Sticker.Thumbnail.FileSize = maxInputBytes + 1
			}
			if !b.input(t.Context(), f, u) || calls != 0 || b.state.Inputs["telegram:123:1"] != "rejected" {
				t.Fatal("invalid sticker dispatched")
			}
			if strings.Contains(strings.Join(f.texts, " "), "PRIVATE_TOKEN") {
				t.Fatal("private transport error shown")
			}
		})
	}
}
func TestTelegramSubmissionRefusalShowsOnlySafeReasonAndUnknownDoesNotReplay(t *testing.T) {
	for _, tc := range []struct{ message, outcome, want string }{
		{"当前无法发送，请先处理待确认事项或恢复连接", "rejected", "恢复 Codex 连接"},
		{"private /Users/example/token=secret", "rejected", "Bot 拒绝"},
		{"", "unknown", "结果暂不确定"},
	} {
		t.Run(tc.outcome+tc.want, func(t *testing.T) {
			calls := 0
			b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
				calls++
				return api.Receipt{ID: in.ID, Outcome: tc.outcome, Message: tc.message}, nil
			}, Chinese: func() bool { return true }})
			paired(b)
			u := message(1, 10, 20, "caption")
			b.input(t.Context(), f, u)
			b.input(t.Context(), f, u)
			if calls != 1 || b.state.Inputs["telegram:123:1"] != tc.outcome || !strings.Contains(strings.Join(f.texts, " "), tc.want) || strings.Contains(strings.Join(f.texts, " "), "secret") {
				t.Fatal(f.texts, b.state.Inputs, calls)
			}
		})
	}
}
func TestTelegramEarlyRejectedErrorIsNotPresentedAsUnknown(t *testing.T) {
	b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		return api.Receipt{ID: in.ID, Outcome: "rejected"}, errors.New("runtime setup required")
	}, Chinese: func() bool { return true }})
	paired(b)
	b.input(t.Context(), f, message(1, 10, 20, "hello"))
	if b.state.Inputs["telegram:123:1"] != "rejected" || !strings.Contains(strings.Join(f.texts, " "), "运行时连接设置") {
		t.Fatal(b.state.Inputs, f.texts)
	}
}
func TestApprovalUsesNativeChoiceAndStaleButtonDoesNotApprove(t *testing.T) {
	var decision api.Decision
	calls := 0
	snapshot := api.Snapshot{Approvals: []api.Approval{{ID: "native-request", Status: "pending", Choices: []api.Choice{{ID: "once", Label: "Allow once"}}}}}
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot { return snapshot }, Decide: func(_ context.Context, d api.Decision) error { calls++; decision = d; return nil }})
	paired(b)
	b.mirror(t.Context(), f, snapshot)
	q := &tg.CallbackQuery{ID: "cb", From: tg.User{ID: 20}, Message: &tg.Message{MessageID: 1, Chat: tg.Chat{ID: 10}}, Data: callbackID("native-request", "once")}
	b.callback(t.Context(), f, q)
	b.callback(t.Context(), f, q)
	if calls != 1 || decision.ID != "native-request" || decision.Choice != "once" {
		t.Fatal("native choice or dedupe lost")
	}
	snapshot.Approvals[0].Status = "resolved"
	q.ID = "cb2"
	b.callback(t.Context(), f, q)
	if calls != 1 {
		t.Fatal("stale button approved")
	}
	snapshot.Approvals = nil
	snapshot.Reviews = []api.Review{{ID: "review", Status: "rejected"}}
	b.mirror(t.Context(), f, snapshot)
	if f.sends != 1 {
		t.Fatal("automatic review exposed as approval")
	}
}

func TestApprovalKeyboardAppearsOnExistingMessageAndOnlyOriginalButtonCanDecide(t *testing.T) {
	var got []api.Decision
	snapshot := api.Snapshot{Approvals: []api.Approval{{ID: "computer-use-request", Title: "Computer Use", Status: "pending"}}}
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot { return snapshot }, Decide: func(_ context.Context, d api.Decision) error { got = append(got, d); return nil }})
	paired(b)
	b.mirror(t.Context(), f, snapshot)
	if f.sends != 1 || f.keyboards[0] != nil {
		t.Fatal("text-only approval must not send an empty keyboard")
	}
	snapshot.Approvals[0].Choices = []api.Choice{{ID: "once", Label: "Allow once", Scope: "allow_once"}, {ID: "session", Label: "Allow this session", Scope: "allow_always"}, {ID: "always", Label: "Always allow", Scope: "allow_always"}, {ID: "deny", Label: "Deny", Scope: "reject_once"}}
	b.mirror(t.Context(), f, snapshot)
	if f.sends != 1 || f.edits != 1 || f.markupEdits != 0 || f.keyboards[1] == nil || len(f.keyboards[1].InlineKeyboard) != 4 {
		t.Fatalf("native options were not attached to the existing message: sends=%d edits=%d markupEdits=%d keyboards=%+v", f.sends, f.edits, f.markupEdits, f.keyboards)
	}
	// A later choice correction with unchanged text edits markup only.
	snapshot.Approvals[0].Choices = snapshot.Approvals[0].Choices[:3]
	b.mirror(t.Context(), f, snapshot)
	if f.sends != 1 || f.edits != 1 || f.markupEdits != 1 || len(f.keyboards[2].InlineKeyboard) != 3 {
		t.Fatal("unchanged approval text was resent instead of updating its keyboard")
	}
	snapshot.Approvals[0].Choices = append(snapshot.Approvals[0].Choices, api.Choice{ID: "deny", Label: "Deny", Scope: "reject_once"})
	b.mirror(t.Context(), f, snapshot)
	invalid := &tg.CallbackQuery{ID: "invalid", From: tg.User{ID: 20}, Message: &tg.Message{MessageID: 100, Chat: tg.Chat{ID: 10}}, Data: callbackID("computer-use-request", "always")}
	b.callback(t.Context(), f, invalid)
	if len(got) != 0 {
		t.Fatal("foreign message granted permission")
	}
	q := &tg.CallbackQuery{ID: "first", From: tg.User{ID: 20}, Message: &tg.Message{MessageID: 1, Chat: tg.Chat{ID: 10}}, Data: callbackID("computer-use-request", "session")}
	b.callback(t.Context(), f, q)
	q.ID, q.Data = "competing", callbackID("computer-use-request", "always")
	b.callback(t.Context(), f, q)
	if len(got) != 1 || got[0].ID != "computer-use-request" || got[0].Choice != "session" {
		t.Fatalf("approval raced or changed native decision: %+v", got)
	}
	if b.state.Inputs["approval-decision:computer-use-request"] != "handled" {
		t.Fatal("original approval claim not durable")
	}
}

func TestRecoveryBaselinesHistoricalTerminalStatusAndKeepsCurrentReceipt(t *testing.T) {
	old := api.Snapshot{Connection: "ready", Phase: "failed", CurrentTurn: "old-turn", LastReceipt: api.Receipt{ID: "old-request", Outcome: "accepted"}, Items: []api.Item{{ID: "old-input", Kind: "user", RequestID: "old-request", TurnKey: "old-turn"}}}
	b, f := testBridge(t, Host{})
	paired(b)
	b.baselineLocked(old)
	b.mirror(t.Context(), f, old)
	if f.sends != 0 || !b.state.Messages["status:old-turn:failed"].Skip {
		t.Fatal("historical failure was republished after recovery")
	}
	current := api.Snapshot{Connection: "ready", Phase: "failed", CurrentTurn: "new-turn", LastReceipt: api.Receipt{ID: "new-request", Outcome: "accepted"}, Items: []api.Item{{ID: "new-input", Kind: "user", RequestID: "new-request", TurnKey: "new-turn"}}}
	b.mirror(t.Context(), f, current)
	if f.sends != 2 || b.state.Messages["status:new-turn:failed"].IDs[0] <= 0 {
		t.Fatalf("current failure lost or repeated: sends=%d state=%+v", f.sends, b.state.Messages)
	}
	b.mirror(t.Context(), f, current)
	if f.sends != 2 {
		t.Fatal("current terminal status sent twice")
	}
	current.CurrentTurn = "another-turn"
	b.mirror(t.Context(), f, current)
	if f.sends != 2 {
		t.Fatal("stale last receipt was attributed to a new turn")
	}
}

func TestRejectedAndUnknownLocalBubblesDoNotMirrorAsDeliveredInput(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	items := []api.Item{{ID: "outgoing:rejected", RequestID: "rejected", Kind: "user", Status: "rejected", Text: "old failed"}, {ID: "outgoing:unknown", RequestID: "unknown", Kind: "user", Status: "unknown", Text: "unconfirmed"}}
	b.mirror(t.Context(), f, api.Snapshot{Items: items})
	if f.sends != 0 {
		t.Fatal("unaccepted local bubbles were delivered as Mac messages")
	}
	items[1] = api.Item{ID: "native:unknown", RequestID: "unknown", Kind: "user", Status: "completed", Text: "confirmed"}
	b.mirror(t.Context(), f, api.Snapshot{Items: items})
	if f.sends != 1 || !strings.Contains(f.texts[0], "confirmed") {
		t.Fatal("later canonical original input was not mirrored once")
	}
}

func TestUnknownApprovalDecisionIsNeverRepeatedAfterBridgeRestart(t *testing.T) {
	snapshot := api.Snapshot{Approvals: []api.Approval{{ID: "exact-approval", Title: "Computer Use", Status: "pending", Choices: []api.Choice{{ID: "once", Label: "Allow once"}}}}}
	calls := 0
	host := Host{Snapshot: func() api.Snapshot { return snapshot }, Decide: func(context.Context, api.Decision) error { calls++; return errors.New("lost decision response") }}
	b, f := testBridge(t, host)
	paired(b)
	b.mirror(t.Context(), f, snapshot)
	q := &tg.CallbackQuery{ID: "first", From: tg.User{ID: 20}, Message: &tg.Message{MessageID: 1, Chat: tg.Chat{ID: 10}}, Data: callbackID("exact-approval", "once")}
	b.callback(t.Context(), f, q)
	if calls != 1 || b.state.Inputs["approval-decision:exact-approval"] != "unknown" {
		t.Fatal("unknown original decision was not retained")
	}
	restored, err := Open(filepath.Dir(b.path), host)
	if err != nil {
		t.Fatal(err)
	}
	q.ID = "second"
	restored.callback(t.Context(), f, q)
	if calls != 1 {
		t.Fatal("unknown approval was submitted again with a new callback")
	}
}
func TestExistingWebhookRequiresExplicitTakeoverAndCloseCancelsPolling(t *testing.T) {
	b, f := testBridge(t, Host{})
	f.webhook = true
	if _, e := b.Connect(t.Context(), "fixture-token", false); e == nil || f.takeovers != 0 {
		t.Fatal("silent webhook takeover")
	}
	if _, e := b.Connect(t.Context(), "fixture-token", true); e != nil || f.takeovers != 1 {
		t.Fatal("explicit takeover failed", e)
	}
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("polling did not cancel on quit")
	}
}
func TestDesktopFileCopiedBeforeComposerConsumesOriginal(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	source := filepath.Join(t.TempDir(), "example.txt")
	os.WriteFile(source, []byte("original bytes"), 0600)
	b.Accepted(api.Submission{ID: "desktop-1", Text: "input"}, []api.InputFile{{Name: "example.txt", Path: source}}, api.Receipt{Outcome: "accepted"})
	os.Remove(source)
	var path string
	for _, relative := range b.state.PendingFiles {
		path = filepath.Join(b.root, relative)
	}
	data, e := os.ReadFile(path)
	if e != nil || string(data) != "original bytes" {
		t.Fatal("copy was too late")
	}
	b.sendFile(t.Context(), f, "upload", path)
	b.sendFile(t.Context(), f, "upload", path)
	if f.documents != 1 {
		t.Fatal("attachment duplicated")
	}
}

func TestReadyBackendLoopMirrorsNewOutputAndFailure(t *testing.T) {
	var mu sync.Mutex
	view := api.Snapshot{Connection: "ready", Items: []api.Item{{ID: "old", Kind: "assistant", Text: "private old history"}}}
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot { mu.Lock(); defer mu.Unlock(); return view }})
	paired(b)
	b.launch(f)
	await := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if check() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("bridge loop did not deliver")
	}
	// Wait until startup has recorded its history boundary, then publish new output.
	await(func() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.state.Messages["item:old"].Skip })
	mu.Lock()
	view.Items = append(view.Items, api.Item{ID: "new", Kind: "assistant", Text: "stream begin"})
	mu.Unlock()
	await(func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.sends == 1 })
	mu.Lock()
	view.Items[1].Text = "stream final"
	view.Phase = "failed"
	view.LastReceipt = api.Receipt{ID: "original", Outcome: "accepted"}
	mu.Unlock()
	await(func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.edits == 1 && f.sends == 1 })
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, text := range f.texts {
		if strings.Contains(text, "private old history") {
			t.Fatal("earlier history leaked")
		}
	}
}
func TestRateLimitHonorsServerDelayAndRetriesOnlyConfirmedRejection(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	f.sendErr = &transportError{issue: "rate_limited", code: 429, retry: 60}
	b.sendText(t.Context(), f, "new", 10, "stream", nil)
	b.sendText(t.Context(), f, "new", 10, "stream", nil)
	if f.sends != 1 || !b.backingOff() {
		t.Fatal("ignored Telegram retry_after")
	}
	b.mu.Lock()
	b.retryUntil = time.Time{}
	b.mu.Unlock()
	f.sendErr = nil
	b.sendText(t.Context(), f, "new", 10, "stream", nil)
	if f.sends != 2 {
		t.Fatal("known unaccepted send could not resume")
	}
}

func TestOwnedArtifactsExportOnceAndEarlierPaginationStaysPrivate(t *testing.T) {
	owned := filepath.Join(t.TempDir(), "result.txt")
	os.WriteFile(owned, []byte("result"), 0600)
	b, f := testBridge(t, Host{Artifact: func(id string) (string, error) {
		if id != "owned-result" {
			t.Fatal("unowned resolver input")
		}
		return owned, nil
	}})
	paired(b)
	b.state.Messages["item:known"] = delivery{Skip: true}
	s := api.Snapshot{Items: []api.Item{{ID: "earlier", Kind: "assistant", Text: "old private conversation"}, {ID: "known", Kind: "assistant", Text: "known"}, {ID: "new-file", Kind: "assistant", Text: "result", Artifacts: []api.Artifact{{ID: "owned-result", Name: "result.txt"}}}}}
	b.mirror(t.Context(), f, s)
	b.mirror(t.Context(), f, s)
	if f.sends != 1 || f.documents != 1 {
		t.Fatal("pagination leaked or artifact duplicated", f.sends, f.documents)
	}
}

func awaitBridge(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("bridge did not reach expected state")
}

func TestDelayedBackendRecoveryBaselinesBeforeQueuedInput(t *testing.T) {
	var mu sync.Mutex
	view := api.Snapshot{Connection: "connecting"}
	unready := make(chan struct{}, 1)
	b, f := testBridge(t, Host{
		Snapshot: func() api.Snapshot {
			mu.Lock()
			defer mu.Unlock()
			if !ready(view) {
				select {
				case unready <- struct{}{}:
				default:
				}
			}
			return view
		},
		Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
			mu.Lock()
			defer mu.Unlock()
			view.Items = append(view.Items, api.Item{ID: "fresh", Kind: "assistant", Text: "fresh reply"})
			return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
		},
	})
	paired(b)
	f.updates <- []tg.Update{message(1, 10, 20, "new work")}
	b.launch(f)
	select {
	case <-unready:
	case <-time.After(2 * time.Second):
		t.Fatal("startup did not inspect backend")
	}
	b.mu.Lock()
	acceptedBeforeRecovery := len(b.state.Inputs) != 0
	b.mu.Unlock()
	if acceptedBeforeRecovery {
		t.Fatal("input was dispatched before recovery boundary")
	}
	mu.Lock()
	view = api.Snapshot{Connection: "ready", Items: []api.Item{{ID: "old", Kind: "assistant", Text: "private recovered history"}}}
	mu.Unlock()
	// Submit publishes its reply immediately, before the first output tick.
	awaitBridge(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.sends == 1 })
	b.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.state.Messages["item:old"].Skip || b.state.Messages["item:fresh"].Skip || b.state.Inputs["telegram:123:1"] != "accepted" {
		t.Fatal("recovered history and fresh reply were not separated")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.texts) != 1 || f.texts[0] != "fresh reply" {
		t.Fatal("fresh reply missing or private history published")
	}
}

func TestDesktopSameNamedAttachmentsKeepSeparateBytesAndCleanup(t *testing.T) {
	b, f := testBridge(t, Host{})
	paired(b)
	files := make([]api.InputFile, 2)
	for index, data := range []string{"first report", "second report"} {
		path := filepath.Join(t.TempDir(), "report.txt")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		files[index] = api.InputFile{Name: "report.txt", Path: path}
	}
	b.Accepted(api.Submission{ID: "two-files"}, files, api.Receipt{Outcome: "accepted"})
	if len(b.state.PendingFiles) != 2 {
		t.Fatal("same-name attachments collapsed")
	}
	var staged []string
	for _, relative := range b.state.PendingFiles {
		staged = append(staged, filepath.Join(b.root, relative))
	}
	if staged[0] == staged[1] || filepath.Base(staged[0]) != "report.txt" || filepath.Base(staged[1]) != "report.txt" {
		t.Fatal("attachment identity or display name lost")
	}
	for _, file := range files {
		os.Remove(file.Path)
	}
	b.flushFiles(t.Context(), f)
	b.flushFiles(t.Context(), f)
	if f.documents != 2 || len(b.state.PendingFiles) != 0 {
		t.Fatal("attachments not delivered exactly once")
	}
	seen := map[string]bool{}
	for _, data := range f.documentBytes {
		seen[data] = true
	}
	if !seen["first report"] || !seen["second report"] {
		t.Fatal("same-name bytes overwritten")
	}
	for _, path := range staged {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("private upload copy was not cleaned")
		}
	}
}

type blockedDocumentClient struct {
	*fakeClient
	started, finished chan struct{}
}

func (f *blockedDocumentClient) Document(ctx context.Context, _ int64, _ string) error {
	close(f.started)
	defer close(f.finished)
	<-ctx.Done()
	return ctx.Err()
}

func TestBlockedUploadDoesNotBlockControlsAndCloseJoinsWorkers(t *testing.T) {
	stopped := make(chan struct{}, 1)
	decided := make(chan api.Decision, 1)
	b, f := testBridge(t, Host{
		Snapshot: func() api.Snapshot {
			return api.Snapshot{Connection: "ready", Approvals: []api.Approval{{ID: "pending", Status: "pending", Choices: []api.Choice{{ID: "once", Label: "Allow once"}}}}}
		},
		Interrupt: func(context.Context) error { stopped <- struct{}{}; return nil },
		Decide:    func(_ context.Context, d api.Decision) error { decided <- d; return nil },
	})
	paired(b)
	b.state.Messages["approval:pending"] = delivery{IDs: []int{99}}
	source := filepath.Join(t.TempDir(), "upload.txt")
	os.WriteFile(source, []byte("fixture"), 0600)
	b.Accepted(api.Submission{ID: "desktop-upload"}, []api.InputFile{{Name: "upload.txt", Path: source}}, api.Receipt{Outcome: "accepted"})
	blocked := &blockedDocumentClient{f, make(chan struct{}), make(chan struct{})}
	b.launch(blocked)
	select {
	case <-blocked.started:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not start")
	}
	f.updates <- []tg.Update{
		message(1, 10, 20, "/stop"), message(2, 10, 20, "/status"),
		{UpdateID: 3, CallbackQuery: &tg.CallbackQuery{ID: "choice", From: tg.User{ID: 20}, Message: &tg.Message{MessageID: 99, Chat: tg.Chat{ID: 10}}, Data: callbackID("pending", "once")}},
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("slow upload blocked interrupt")
	}
	select {
	case d := <-decided:
		if d.ID != "pending" || d.Choice != "once" {
			t.Fatal("approval identity lost")
		}
	case <-time.After(time.Second):
		t.Fatal("slow upload blocked approval")
	}
	awaitBridge(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.answers == 1 && f.sends >= 1 })
	select {
	case <-blocked.finished:
		t.Fatal("upload finished before controls were handled")
	default:
	}
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel and join workers")
	}
	select {
	case <-blocked.finished:
	default:
		t.Fatal("Close returned while upload was still running")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.Offset != 4 || b.state.Inputs["telegram:123:1"] != "handled" || b.state.Messages["upload:desktop-upload:0"].IDs[0] != -1 {
		t.Fatal("control offset or uncertain upload receipt lost")
	}
}

func TestCloseCancelsWaitingForBackendRecovery(t *testing.T) {
	b, f := testBridge(t, Host{Snapshot: func() api.Snapshot { return api.Snapshot{Connection: "connecting"} }})
	b.launch(f)
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on backend recovery")
	}
}

func TestRemoteInputCannotEchoDuringConcurrentOutput(t *testing.T) {
	published := make(chan api.Submission, 1)
	release := make(chan struct{})
	b, f := testBridge(t, Host{Submit: func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		published <- in
		<-release
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	}})
	paired(b)
	done := make(chan struct{})
	go func() { defer close(done); b.input(t.Context(), f, message(1, 10, 20, "remote input")) }()
	in := <-published
	b.mirror(t.Context(), f, api.Snapshot{Items: []api.Item{{ID: "user", Kind: "user", RequestID: in.ID, Text: in.Text}, {ID: "reply", Kind: "assistant", Text: "reply while submit completes"}}})
	close(release)
	<-done
	if f.sends != 1 || len(f.texts) != 1 || f.texts[0] != "reply while submit completes" {
		t.Fatal("concurrent output echoed Telegram input")
	}
}
