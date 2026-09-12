package telegram

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/attachment"
	"github.com/daoleno/zen/daemon/brain"
)

const (
	maxTelegramFileBytes int64 = 20 << 20
	maxMediaInputs             = 128
	albumQuietTime             = 2 * time.Second
	mediaDownloadTimeout       = 30 * time.Second
)

var errMediaReceiptCapacity = errors.New("media receipt capacity reached")

type mediaPart struct {
	UpdateID  int64              `json:"update_id"`
	MessageID int64              `json:"message_id"`
	Remote    File               `json:"remote"`
	File      attachment.File    `json:"file"`
	Caption   attachment.Caption `json:"caption"`
}

// Media inputs are channel delivery receipts, not a second media database.
// Their bytes live in the same bounded upload store as mobile attachments.
type mediaInput struct {
	TextOnly        bool        `json:"text_only,omitempty"`
	DependsOn       []string    `json:"depends_on,omitempty"`
	ID              string      `json:"id"`
	Receipt         string      `json:"receipt"`
	SessionID       string      `json:"session_id,omitempty"`
	BrainThreadID   string      `json:"brain_thread_id,omitempty"`
	MessageThreadID int64       `json:"message_thread_id,omitempty"`
	OwnerID         int64       `json:"owner_id"`
	ChatID          int64       `json:"chat_id"`
	BotID           int64       `json:"bot_id"`
	Parts           []mediaPart `json:"parts"`
	Reply           string      `json:"reply,omitempty"`
	Album           bool        `json:"album,omitempty"`
	State           string      `json:"state"`
	Body            string      `json:"body,omitempty"`
	Attempts        int         `json:"attempts,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	ReadyAt         time.Time   `json:"ready_at"`
}

func mediaSelection(message Message) (mediaPart, error) {
	part := mediaPart{MessageID: message.MessageID, Caption: attachment.Caption{Text: message.Caption, Entities: message.CaptionEntities}}
	if err := validateCaption(part.Caption); err != nil {
		return part, err
	}
	var selected *MediaFile
	file := attachment.File{Kind: "file"}
	switch {
	case len(message.Photo) > 0:
		var best *PhotoSize
		for i := range message.Photo {
			photo := &message.Photo[i]
			if photo.Width <= 0 || photo.Height <= 0 || photo.Width > 40000 || photo.Height > 40000 {
				continue
			}
			if best == nil || int64(photo.Width)*int64(photo.Height) > int64(best.Width)*int64(best.Height) {
				best = photo
			}
		}
		if best == nil {
			return part, fmt.Errorf("Photo metadata is invalid; send it as a document.")
		}
		selected = &MediaFile{File: best.File, FileName: "photo.jpg", MIMEType: "image/jpeg"}
		file.Kind = "image"
	case message.Sticker != nil:
		sticker := message.Sticker
		selected = &MediaFile{File: sticker.File, FileName: "sticker.webp", MIMEType: "image/webp"}
		file.Kind = "image"
		file.Description = "Static sticker " + sticker.Emoji
		if sticker.IsAnimated || sticker.IsVideo {
			file.Kind = "file"
			file.Description = "Sticker " + sticker.Emoji + " (animation not interpreted)"
			selected.FileName, selected.MIMEType = "sticker.tgs", "application/x-tgsticker"
			if sticker.IsVideo {
				selected.FileName, selected.MIMEType = "sticker.webm", "video/webm"
			}
		}
	case message.Animation != nil:
		selected = message.Animation
		file.Description = "Animation file; motion is not interpreted by this connection."
	case message.Document != nil:
		selected = message.Document
	case message.Audio != nil:
		selected = message.Audio
		file.Description = "Audio file; no transcription was performed."
	case message.Voice != nil:
		selected = message.Voice
		file.Description = "Voice file; no transcription was performed."
	case message.Video != nil:
		selected = message.Video
		file.Description = "Video file; audio and motion are not interpreted by this connection."
	case message.VideoNote != nil:
		selected = message.VideoNote
		file.Description = "Video note; audio and motion are not interpreted by this connection."
	default:
		return part, fmt.Errorf("This media type is not supported. Send a document or photo.")
	}
	if selected.FileID == "" || len(selected.FileID) > 1024 || selected.FileSize < 0 || selected.FileSize > maxTelegramFileBytes {
		return part, fmt.Errorf("File metadata is invalid or the file exceeds Telegram's 20 MiB download limit.")
	}
	file.Name = selected.FileName
	if file.Name == "" {
		file.Name = "attachment"
	}
	if !utf8.ValidString(file.Name) || strings.TrimSpace(file.Name) == "" || len(file.Name) > 1024 || strings.ContainsAny(file.Name, "/\\") || file.Name == "." || file.Name == ".." {
		return part, fmt.Errorf("File name is invalid; resend with a simple file name.")
	}
	for _, r := range file.Name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			return part, fmt.Errorf("File name contains unsafe control characters.")
		}
	}
	file.ContentType = selected.MIMEType
	if file.ContentType == "" {
		file.ContentType = "application/octet-stream"
	}
	if len(file.ContentType) > 255 {
		return part, fmt.Errorf("File content type is invalid.")
	}
	typeName, _, err := mime.ParseMediaType(file.ContentType)
	if err != nil || !strings.Contains(typeName, "/") {
		return part, fmt.Errorf("File content type is invalid.")
	}
	file.ContentType = typeName
	file.Size = selected.FileSize
	if file.Size == 0 {
		file.Size = -1
	}
	part.Remote, part.File = selected.File, file
	// Never persist the tokenized download path; getFile is refreshed on retry.
	part.Remote.FilePath = ""
	return part, nil
}

func validateCaption(caption attachment.Caption) error {
	if !utf8.ValidString(caption.Text) || utf16Len(caption.Text) > 4096 || len(caption.Entities) > 100 {
		return fmt.Errorf("Caption is invalid or too long; shorten it and resend the file.")
	}
	boundaries := map[int]bool{0: true}
	units := 0
	for _, r := range caption.Text {
		units++
		if r > 0xffff {
			units++
		}
		boundaries[units] = true
	}
	for _, entity := range caption.Entities {
		if entity.Length <= 0 || entity.Offset < 0 || entity.Length > units || entity.Offset > units-entity.Length || !boundaries[entity.Offset] || !boundaries[entity.Offset+entity.Length] || len(entity.URL) > 2048 || len(entity.CustomEmojiID) > 128 {
			return fmt.Errorf("Caption entities are invalid; resend the caption as plain text.")
		}
	}
	return nil
}

func (m *Manager) stageMedia(message Message, updateID int64) (string, error) {
	state := m.store.snapshot()
	id := fmt.Sprintf("update:%d", updateID)
	if message.MediaGroupID != "" {
		id = fmt.Sprintf("album:%d:%s", message.MessageThreadID, digestText(message.MediaGroupID))
	}
	if existing, ok := state.MediaInputs[id]; ok && existing.State != "collecting" {
		for _, part := range existing.Parts {
			if part.UpdateID == updateID {
				return "media_queued", nil
			}
		}
		m.enqueueTopicText("media-late:"+id, "This album is already sealed. This late item was not forwarded; resend it separately.", message.MessageThreadID, message.MessageID)
		return "media_late", nil
	}
	reject := func(reason string) (string, error) {
		if _, ok := state.MediaInputs[id]; ok {
			return "media_rejected", m.failMedia(id, reason)
		}
		m.enqueueTopicText("media-error:"+id, reason+" Nothing was forwarded.", message.MessageThreadID, message.MessageID)
		return "media_rejected", nil
	}
	if m.attachments == nil || m.brain == nil {
		return reject("Attachment storage is unavailable.")
	}
	if _, ok := m.api.(FileAPI); !ok {
		return reject("Telegram file download is unavailable.")
	}
	part, err := mediaSelection(message)
	if err != nil {
		return reject(err.Error())
	}
	part.UpdateID = updateID
	input, err := m.mediaRecipient(state, message, updateID)
	if err != nil {
		return reject(err.Error())
	}
	input.ID, input.Album = id, message.MediaGroupID != ""
	if !m.mediaRecipientAvailable(input) {
		return reject("The exact recipient is unavailable; choose Brain or a current Session.")
	}
	err = m.store.mutate(func(current *durableState) error {
		if current.BotID != state.BotID || current.ChatID != state.ChatID || current.OwnerID != state.OwnerID || !current.Enabled {
			return fmt.Errorf("Telegram binding changed before staging media")
		}
		for key, row := range current.MediaInputs {
			if mediaTerminal(row.State) && m.now().Sub(row.CreatedAt) > 24*time.Hour {
				delete(current.MediaInputs, key)
			}
		}
		if existing, ok := current.MediaInputs[id]; ok {
			input = existing
		} else if len(current.MediaInputs) >= maxMediaInputs {
			return errMediaReceiptCapacity
		}
		for _, p := range input.Parts {
			if p.MessageID == message.MessageID {
				return nil
			}
		}
		input.Parts = append(input.Parts, part)
		input.ReadyAt = m.now().UTC()
		if input.Album {
			input.ReadyAt = minTime(input.ReadyAt.Add(albumQuietTime), input.CreatedAt.Add(10*time.Second))
		}
		current.MediaInputs[id] = input
		return nil
	})
	if err != nil {
		if errors.Is(err, errMediaReceiptCapacity) {
			return reject("Attachment receipt capacity is full; try again later.")
		}
		return "", err
	}
	var total int64
	for _, p := range m.store.snapshot().MediaInputs[id].Parts {
		total += max(p.File.Size, 0)
	}
	if len(m.store.snapshot().MediaInputs[id].Parts) > 10 || total > maxTelegramFileBytes {
		return "media_rejected", m.failMedia(id, "Album exceeds 10 files or the 20 MiB total batch limit.")
	}
	return "media_queued", nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func mediaTerminal(state string) bool {
	return state == "accepted" || state == "uncertain" || state == "failed" || state == "not_submitted"
}

func (m *Manager) mediaRecipientAvailable(input mediaInput) bool {
	state := m.store.snapshot()
	if !state.Enabled || state.BotID != input.BotID || state.ChatID != input.ChatID || state.OwnerID != input.OwnerID || m.brain == nil {
		return false
	}
	if input.SessionID != "" {
		projection, err := m.brain.SessionProjection(input.SessionID)
		return err == nil && projection.Present
	}
	thread, err := m.brain.ChatThreadID()
	return err == nil && thread == input.BrainThreadID
}

func (m *Manager) failMedia(id, reason string) error {
	return m.store.mutate(func(state *durableState) error {
		input, ok := state.MediaInputs[id]
		if !ok || mediaTerminal(input.State) {
			return nil
		}
		input.State = "failed"
		if input.TextOnly {
			input.State = "not_submitted"
			for _, part := range input.Parts {
				key := fmt.Sprint(part.UpdateID)
				record := state.Processed[key]
				record.Disposition, record.SessionID, record.BrainThreadID, record.MessageThreadID = "deferred_not_submitted", input.SessionID, input.BrainThreadID, input.MessageThreadID
				state.Processed[key] = record
			}
		}
		state.MediaInputs[id] = input
		text := reason + " Nothing was forwarded. Resend the whole batch to retry."
		if input.TextOnly {
			text = reason + " This follow-up was not submitted. Resend the file and prompt together to retry."
		}
		if !enqueue(state, outboxRecord{ID: "media-error:" + id, Kind: "send", Text: text,
			SessionID: input.SessionID, BrainThreadID: input.BrainThreadID, MessageThreadID: input.MessageThreadID, ReplyMessageID: input.Parts[0].MessageID, CreatedAt: m.now().UTC(), ReplyMarkup: navigationKeyboard(*state, input.MessageThreadID)}) {
			return fmt.Errorf("media feedback queue is full")
		}
		return nil
	})
}

// advanceMedia runs on the polling owner. It starts at most one download or
// admits one completed batch per pass, never draining the backlog before polls.
func (m *Manager) advanceMedia(ctx context.Context, token string) error {
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	state := m.store.snapshot()
	if !state.Enabled || state.WebhookConflict {
		return nil
	}
	currentToken, err := m.store.readToken()
	if err != nil || currentToken != token {
		return nil
	}
	if followup := readyMediaFollowup(state); followup != nil {
		return m.admitMedia(*followup)
	}
	m.mediaMu.Lock()
	active := m.mediaActive
	if active != nil {
		select {
		case <-active.done:
			m.mediaActive = nil
		default:
			m.mediaMu.Unlock()
			return nil
		}
	}
	m.mediaMu.Unlock()
	if active != nil {
		cancelled := active.ctx.Err() != nil
		active.cancel()
		if !cancelled && active.err != nil {
			input, found := m.store.snapshot().MediaInputs[active.id]
			if found && m.mediaBindingCurrent(input) && !mediaTerminal(input.State) {
				input.Attempts++
				if input.Attempts >= 3 {
					return m.failMedia(input.ID, "File download failed, timed out, or exceeded its declared size/20 MiB limit.")
				}
				input.ReadyAt = m.now().Add(time.Duration(input.Attempts) * 2 * time.Second)
				return m.saveMedia(input)
			}
		}
	}
	// FIFO media ordering includes collecting albums and retry backoffs.
	// Ready follow-ups above depend only on their own recipient, not on an
	// unrelated recipient's slower file transfer.
	var next *mediaInput
	for _, input := range state.MediaInputs {
		if mediaTerminal(input.State) || input.TextOnly {
			continue
		}
		if next == nil || input.CreatedAt.Before(next.CreatedAt) ||
			(input.CreatedAt.Equal(next.CreatedAt) && input.Parts[0].UpdateID < next.Parts[0].UpdateID) {
			copy := input
			next = &copy
		}
	}
	if next == nil || m.now().Before(next.ReadyAt) {
		return nil
	}
	input := *next
	if input.State == "admitting" {
		return m.finishMedia(input, brain.ExternalInputUncertain)
	}
	if !m.mediaRecipientAvailable(input) || m.now().Sub(input.CreatedAt) > 30*time.Minute {
		return m.failMedia(input.ID, "The original recipient is unavailable or this pending batch expired.")
	}
	if input.Body == "" {
		input.State = "downloading"
		if err := m.saveMedia(input); err != nil {
			return err
		}
		workerCtx, cancel := context.WithCancel(ctx)
		transfer := &mediaTransfer{id: input.ID, ctx: workerCtx, cancel: cancel, done: make(chan struct{})}
		m.mediaMu.Lock()
		m.mediaActive = transfer
		m.mediaMu.Unlock()
		go func() {
			defer close(transfer.done)
			transfer.err = m.downloadMediaBatch(workerCtx, token, input)
			m.signal()
		}()
		return nil
	}
	return m.admitMedia(input)
}

// Provider admission stays on Run under outboundMu for both files and their
// dependent prompts. Download workers never call this method.
func (m *Manager) admitMedia(input mediaInput) error {
	if input.State == "admitting" {
		return m.finishMedia(input, brain.ExternalInputUncertain)
	}
	state := m.store.snapshot()
	pendingDependency := false
	for _, id := range input.DependsOn {
		dependency, found := state.MediaInputs[id]
		if !found || (mediaTerminal(dependency.State) && dependency.State != "accepted") {
			return m.failMedia(input.ID, "The earlier attachment or message was not confirmed as received.")
		}
		if dependency.State != "accepted" {
			pendingDependency = true
		}
	}
	if pendingDependency {
		return nil
	}
	if !m.mediaRecipientAvailable(input) || m.now().Sub(input.CreatedAt) > 30*time.Minute {
		return m.failMedia(input.ID, "The original recipient is unavailable or this pending input expired.")
	}
	for _, part := range input.Parts {
		if !input.TextOnly && !m.attachments.Exists(part.File) {
			return m.failMedia(input.ID, "A staged file is no longer available.")
		}
	}
	input.State = "admitting"
	if err := m.saveMedia(input); err != nil {
		return err
	}
	var admission brain.ExternalInputDisposition
	if input.SessionID != "" {
		admission, _ = m.brain.SubmitExternalSessionInput(input.SessionID, input.Receipt, input.Body)
	} else {
		if err := m.store.mutate(func(state *durableState) error { state.BrainReplyTopicID = input.MessageThreadID; return nil }); err != nil {
			return err
		}
		admission, _ = m.brain.SubmitExternalUserInputInThread(input.Receipt, input.Body, input.BrainThreadID)
	}
	return m.finishMedia(input, admission)
}

type mediaTransfer struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	err    error // published by closing done
}

// Callers changing channel authority hold outboundMu. Cancellation does not
// join IO under that mutex: the worker may be finishing a fenced checkpoint.
func (m *Manager) cancelMedia() {
	m.mediaMu.Lock()
	defer m.mediaMu.Unlock()
	if m.mediaActive != nil {
		m.mediaActive.cancel()
	}
}

func (m *Manager) stopMedia() {
	m.mediaMu.Lock()
	active := m.mediaActive
	if active != nil {
		active.cancel()
	}
	m.mediaMu.Unlock()
	if active != nil {
		<-active.done
	}
	m.mediaMu.Lock()
	m.mediaActive = nil
	m.mediaMu.Unlock()
}

func (m *Manager) mediaBindingCurrent(input mediaInput) bool {
	state := m.store.snapshot()
	row, exists := state.MediaInputs[input.ID]
	return exists && row.Receipt == input.Receipt && state.BotID == input.BotID &&
		state.ChatID == input.ChatID && state.OwnerID == input.OwnerID
}

// Only IO and file checkpoints run in the worker. Canonical recipient checks
// and provider admission stay on Run's serialized input owner.
func (m *Manager) downloadMediaBatch(ctx context.Context, token string, input mediaInput) error {
	ctx, cancel := context.WithTimeout(ctx, m.mediaTimeout)
	defer cancel()
	var files []attachment.File
	var captions []attachment.Caption
	var bodies []string
	var total int64
	for i, part := range input.Parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if part.File.Path == "" || !m.attachments.Exists(part.File) {
			file, err := m.downloadMedia(ctx, token, part, maxTelegramFileBytes-total)
			if err != nil {
				return err
			}
			part.File = file
			input.Parts[i] = part
			if err := m.checkpointMedia(ctx, input); err != nil {
				_ = m.attachments.Remove(file)
				return err
			}
		}
		total += part.File.Size
		files = append(files, part.File)
		captions = append(captions, part.Caption)
		if part.Caption.Text != "" {
			bodies = append(bodies, part.Caption.Text)
		}
	}
	body := strings.Join(bodies, "\n\n")
	if input.Reply != "" {
		body = "Replying to: " + input.Reply + "\n\n" + body
	}
	input.Body = attachment.Input(body, files, captions)
	return m.checkpointMedia(ctx, input)
}

func (m *Manager) checkpointMedia(ctx context.Context, input mediaInput) error {
	m.outboundMu.Lock()
	defer m.outboundMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	state := m.store.snapshot()
	if !state.Enabled || state.WebhookConflict || !m.mediaBindingCurrent(input) {
		return context.Canceled
	}
	return m.saveMedia(input)
}

func (m *Manager) downloadMedia(ctx context.Context, token string, part mediaPart, remaining int64) (attachment.File, error) {
	api, ok := m.api.(FileAPI)
	if !ok || m.attachments == nil || remaining <= 0 {
		return attachment.File{}, fmt.Errorf("media download unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, m.mediaTimeout)
	defer cancel()
	remote, err := api.GetFile(ctx, token, part.Remote.FileID)
	if err != nil {
		return attachment.File{}, err
	}
	if remote.FileID != part.Remote.FileID || remote.FileSize < 0 || remote.FileSize > remaining || (part.File.Size >= 0 && remote.FileSize > 0 && part.File.Size != remote.FileSize) {
		return attachment.File{}, fmt.Errorf("media metadata mismatch")
	}
	body, err := api.DownloadFile(ctx, token, remote)
	if err != nil {
		return attachment.File{}, err
	}
	defer body.Close()
	reader := bufio.NewReader(body)
	header, _ := reader.Peek(512)
	if part.File.Kind == "image" && http.DetectContentType(header) != part.File.ContentType {
		return attachment.File{}, fmt.Errorf("image content does not match its media type")
	}
	file := part.File
	if file.Size < 0 && remote.FileSize > 0 {
		file.Size = remote.FileSize
	}
	limits := attachment.DefaultLimits()
	limits.FileBytes = remaining
	return m.attachments.Save(ctx, reader, file, limits)
}

func (m *Manager) saveMedia(input mediaInput) error {
	// The downloader continues editing its own part list after a checkpoint.
	// Store.mutate retains the assigned value, so it must own a separate slice.
	input.Parts = slices.Clone(input.Parts)
	return m.store.mutate(func(state *durableState) error { state.MediaInputs[input.ID] = input; return nil })
}

func (m *Manager) finishMedia(input mediaInput, result brain.ExternalInputDisposition) error {
	input.State = string(result)
	text := "Files received by Zen."
	switch result {
	case brain.ExternalInputAccepted:
		for _, part := range input.Parts {
			if part.File.Description != "" {
				text += "\n" + part.File.Description
			}
		}
	case brain.ExternalInputPending, brain.ExternalInputUncertain:
		input.State = "uncertain"
		text = "Zen could not prove whether the recipient received these files. They were not replayed."
	default:
		input.State = "not_submitted"
		text = "These files were not submitted. Resend the whole batch when the recipient is available."
	}
	if input.TextOnly {
		switch input.State {
		case "accepted":
			text = ""
		case "uncertain":
			text = "Zen could not prove whether the recipient received this follow-up. It was not replayed."
		default:
			text = "This follow-up was not submitted. Resend the file and prompt together when the recipient is available."
		}
	}
	return m.store.mutate(func(state *durableState) error {
		state.MediaInputs[input.ID] = input
		for _, part := range input.Parts {
			if result == brain.ExternalInputAccepted {
				recordMessageSource(state, part.MessageID, messageSource{SessionID: input.SessionID, BrainThreadID: input.BrainThreadID, MessageThreadID: input.MessageThreadID})
			}
			key := fmt.Sprint(part.UpdateID)
			row := state.Processed[key]
			row.Disposition, row.SessionID, row.BrainThreadID, row.MessageThreadID = "media_"+input.State, input.SessionID, input.BrainThreadID, input.MessageThreadID
			if input.TextOnly {
				row.Disposition = "deferred_" + input.State
			}
			state.Processed[key] = row
		}
		if text == "" {
			return nil
		}
		if !enqueue(state, outboxRecord{ID: "media-ack:" + input.ID, Kind: "send", Text: text, MessageThreadID: input.MessageThreadID,
			SessionID: input.SessionID, BrainThreadID: input.BrainThreadID, ReplyMessageID: input.Parts[0].MessageID, CreatedAt: m.now().UTC(), ReplyMarkup: navigationKeyboard(*state, input.MessageThreadID)}) {
			return fmt.Errorf("media feedback queue is full")
		}
		return nil
	})
}
