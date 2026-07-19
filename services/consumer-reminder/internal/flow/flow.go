// Package flow drives the reminder conversation: pick a target (myself /
// someone else), collect what+when (already extracted from natural language
// by consumer-llm-processor), confirm, save. Every outgoing prompt is a
// ReplyEvent published for consumer-reply-line-user; this service never
// calls LINE or an LLM directly.
package flow

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"consumer-reminder/internal/events"
	"consumer-reminder/internal/store"
)

// maxTargetButtons caps the user picker: LINE allows 13 quick-reply items,
// and one slot is reserved for Cancel.
const maxTargetButtons = 12

// maxLabelRunes is LINE's quick-reply label limit.
const maxLabelRunes = 20

// bangkok is the display/entry timezone. A fixed zone avoids depending on
// tzdata being present in the container image.
var bangkok = time.FixedZone("ICT", 7*60*60)

// Store is the subset of the Postgres store the flow needs.
type Store interface {
	ListUsers(ctx context.Context, exclude string, limit int) ([]store.User, error)
	GetDisplayName(ctx context.Context, userID string) (string, error)
	CreateReminder(ctx context.Context, creatorID, targetID, message string, remindAt time.Time) (int64, error)
	ListUpcoming(ctx context.Context, creatorID string, limit int) ([]store.Reminder, error)
	GetReminder(ctx context.Context, id int64, creatorID string) (*store.Reminder, error)
	CancelReminder(ctx context.Context, id int64, creatorID string) (bool, error)
	UpdateReminder(ctx context.Context, id int64, creatorID, message string, remindAt time.Time) (bool, error)
}

type Flow struct {
	store   Store
	state   *StateStore
	publish func(events.ReplyEvent) error
	now     func() time.Time
}

func New(st Store, state *StateStore, publish func(events.ReplyEvent) error) *Flow {
	return &Flow{store: st, state: state, publish: publish, now: time.Now}
}

// isTrigger mirrors line-webhook's isReminderRequest: the hard keyword
// "/reminder" (alone or with details) or Thai "ตั้งเตือน...".
func isTrigger(trimmed string) bool {
	return trimmed == "/reminder" ||
		strings.HasPrefix(trimmed, "/reminder ") ||
		strings.HasPrefix(trimmed, "ตั้งเตือน")
}

// isListTrigger opens the manage flow (list / edit / delete). Must match the
// list keywords in line-webhook's isReminderRequest and
// consumer-llm-processor's isReminderCommand.
func isListTrigger(trimmed string) bool {
	return trimmed == "/reminders" ||
		strings.HasPrefix(trimmed, "ดูเตือน") ||
		strings.HasPrefix(trimmed, "รายการเตือน")
}

// isCancelText reports whether the user typed a cancel instead of pressing
// the cancel button; both must work at every step. Must match
// consumer-llm-processor's isCancelText.
func isCancelText(trimmed string) bool {
	switch strings.ToLower(trimmed) {
	case "ยกเลิก", "cancel", "/cancel":
		return true
	default:
		return false
	}
}

// stripTrigger removes the trigger keyword, leaving any trailing details
// ("/reminder เตือนพรุ่งนี้ 9 โมง กินยา" -> "เตือนพรุ่งนี้ 9 โมง กินยา").
func stripTrigger(trimmed string) string {
	rest := strings.TrimPrefix(trimmed, "/reminder")
	rest = strings.TrimPrefix(rest, "ตั้งเตือน")
	return strings.TrimSpace(rest)
}

// HandleRequest handles a ReminderRequestEvent: a trigger keyword starts
// (or restarts) the flow; other text is a mid-flow answer.
func (f *Flow) HandleRequest(ctx context.Context, ev events.ReminderRequestEvent) {
	text := strings.TrimSpace(ev.Text)
	state, err := f.state.Get(ctx, ev.UserID)
	if err != nil {
		log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: state load failed")
		f.reply(ev.UserID, ev.ReplyToken, "ขอโทษน้า ระบบเตือนความจำขัดข้อง ลองใหม่อีกครั้งนะ", nil)
		return
	}

	// Typed cancel ends the flow just like the cancel button.
	if state != nil && isCancelText(text) {
		f.cancel(ctx, ev.UserID, ev.ReplyToken)
		return
	}

	// The list keyword opens the manage flow (also mid-flow: it restarts).
	if isListTrigger(text) {
		f.startManage(ctx, ev.UserID, ev.ReplyToken)
		return
	}

	// A trigger keyword always starts fresh - also mid-flow, so a stuck user
	// can just type /reminder again. Non-trigger text with no flow state
	// comes from consumer-llm-processor's intent classifier; it starts the
	// flow too, with the whole text as details.
	if state == nil || isTrigger(text) {
		f.start(ctx, ev, text)
		return
	}

	switch state.Step {
	case StepAwaitDetails:
		f.handleDetails(ctx, ev, state, text)
	default:
		// The user typed instead of pressing a button; repeat the prompt.
		f.promptForStep(ctx, ev.UserID, ev.ReplyToken, state)
	}
}

// start opens a new flow. Details extracted upstream (or, failing that, the
// raw text after the trigger keyword) pre-fill the state so the later steps
// can skip straight to confirmation.
func (f *Flow) start(ctx context.Context, ev events.ReminderRequestEvent, text string) {
	state := &State{Step: StepAwaitTarget}
	state.Message = ev.Message
	if state.Message == "" {
		// Extraction found nothing (or failed upstream): keep the user's own
		// words so at least the message survives.
		details := text
		if isTrigger(text) {
			details = stripTrigger(text)
		}
		state.Message = details
	}
	state.RemindAt = parseRemindAt(ev.RemindAt)

	if err := f.state.Put(ctx, ev.UserID, state); err != nil {
		log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: state save failed")
		f.reply(ev.UserID, ev.ReplyToken, "ขอโทษน้า ระบบเตือนความจำขัดข้อง ลองใหม่อีกครั้งนะ", nil)
		return
	}
	log.Info().Str("user_id", ev.UserID).Bool("prefilled", state.Message != "").Msg("flow: started")

	f.reply(ev.UserID, ev.ReplyToken, "จะให้เตือนใครดี? / Who is this reminder for?", []events.QuickReply{
		{Label: "เตือนตัวเอง", Data: "flow=rem&a=target&v=self", DisplayText: "เตือนตัวเอง"},
		{Label: "เตือนคนอื่น", Data: "flow=rem&a=target&v=other", DisplayText: "เตือนคนอื่น"},
		{Label: "ยกเลิก", Data: "flow=rem&a=cancel", DisplayText: "ยกเลิก"},
	})
}

// startManage lists the user's upcoming reminders with one pick-button per
// reminder; picking one offers edit / delete.
func (f *Flow) startManage(ctx context.Context, userID, replyToken string) {
	rems, err := f.store.ListUpcoming(ctx, userID, maxTargetButtons)
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("flow: list upcoming failed")
		f.reply(userID, replyToken, "ขอโทษน้า ดึงรายการเตือนไม่สำเร็จ ลองใหม่อีกครั้งนะ", nil)
		return
	}
	if len(rems) == 0 {
		// Not a flow: make sure no stale state lingers.
		if err := f.state.Delete(ctx, userID); err != nil {
			log.Error().Str("user_id", userID).Err(err).Msg("flow: state delete failed")
		}
		f.reply(userID, replyToken, "ยังไม่มีรายการเตือนที่กำลังจะถึงเลยน้า ตั้งใหม่ด้วย /reminder ได้เลย", nil)
		return
	}

	var b strings.Builder
	b.WriteString("รายการเตือนที่กำลังจะถึง ⏰\n")
	buttons := make([]events.QuickReply, 0, len(rems)+1)
	for i, r := range rems {
		fmt.Fprintf(&b, "\n%d) %s\n    %s", i+1, r.Message, formatBangkok(r.RemindAt))
		if r.TargetUserID != userID && r.TargetName != "" {
			fmt.Fprintf(&b, " (เตือน %s)", r.TargetName)
		}
		label := truncateLabel(fmt.Sprintf("%d) %s", i+1, r.Message))
		buttons = append(buttons, events.QuickReply{
			Label:       label,
			Data:        fmt.Sprintf("flow=rem&a=pick&v=%d", r.ID),
			DisplayText: label,
		})
	}
	b.WriteString("\n\nกดเลือกรายการเพื่อแก้ไขหรือลบได้เลย")
	buttons = append(buttons, events.QuickReply{Label: "ยกเลิก", Data: "flow=rem&a=cancel", DisplayText: "ยกเลิก"})

	f.save(ctx, userID, replyToken, &State{Step: StepManage})
	f.reply(userID, replyToken, b.String(), buttons)
}

// HandlePostback handles quick-reply button presses. Only flow=rem payloads
// belong to this service; anything else is dropped with a log line.
func (f *Flow) HandlePostback(ctx context.Context, ev events.PostbackEvent) {
	values, err := url.ParseQuery(ev.Data)
	if err != nil || values.Get("flow") != "rem" {
		log.Info().Str("user_id", ev.UserID).Str("data", ev.Data).Msg("flow: ignoring foreign postback")
		return
	}
	action := values.Get("a")

	state, err := f.state.Get(ctx, ev.UserID)
	if err != nil {
		log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: state load failed")
		f.reply(ev.UserID, ev.ReplyToken, "ขอโทษน้า ระบบเตือนความจำขัดข้อง ลองใหม่อีกครั้งนะ", nil)
		return
	}
	if action == "cancel" {
		f.cancel(ctx, ev.UserID, ev.ReplyToken)
		return
	}
	if state == nil {
		// TTL expired mid-conversation - never a silent drop.
		f.reply(ev.UserID, ev.ReplyToken, "หมดเวลาไปแล้วน้า เริ่มใหม่ด้วย /reminder ได้เลย", nil)
		return
	}

	switch action {
	case "target":
		f.handleTarget(ctx, ev, state, values.Get("v"))
	case "user":
		f.handleUserPick(ctx, ev, state, values.Get("v"))
	case "confirm":
		f.handleConfirm(ctx, ev, state)
	case "edit":
		state.Step = StepAwaitDetails
		state.RemindAt = time.Time{}
		f.save(ctx, ev.UserID, ev.ReplyToken, state)
		f.reply(ev.UserID, ev.ReplyToken, "โอเค พิมพ์ใหม่ได้เลย จะให้เตือนว่าอะไร เมื่อไหร่?", nil)
	case "pick":
		f.handleManagePick(ctx, ev, values.Get("v"))
	case "redit":
		f.handleManageEdit(ctx, ev, values.Get("v"))
	case "rdel":
		f.handleManageDelete(ctx, ev, values.Get("v"))
	default:
		log.Warn().Str("user_id", ev.UserID).Str("action", action).Msg("flow: unknown postback action")
		f.promptForStep(ctx, ev.UserID, ev.ReplyToken, state)
	}
}

// handleManagePick shows one reminder's details with edit/delete buttons.
func (f *Flow) handleManagePick(ctx context.Context, ev events.PostbackEvent, idStr string) {
	rem := f.lookupManaged(ctx, ev, idStr)
	if rem == nil {
		return
	}
	target := ""
	if rem.TargetUserID != ev.UserID && rem.TargetName != "" {
		target = fmt.Sprintf("\nเตือนให้: %s", rem.TargetName)
	}
	text := fmt.Sprintf("\"%s\"\n%s%s\n\nจะทำอะไรกับรายการนี้ดี?",
		rem.Message, formatBangkok(rem.RemindAt), target)
	f.reply(ev.UserID, ev.ReplyToken, text, []events.QuickReply{
		{Label: "แก้ไข", Data: fmt.Sprintf("flow=rem&a=redit&v=%d", rem.ID), DisplayText: "แก้ไข"},
		{Label: "ลบ", Data: fmt.Sprintf("flow=rem&a=rdel&v=%d", rem.ID), DisplayText: "ลบ"},
		{Label: "ยกเลิก", Data: "flow=rem&a=cancel", DisplayText: "ยกเลิก"},
	})
}

// handleManageEdit switches into the normal details step, pre-filled with the
// existing message so the user can change just the time (or type a whole new
// "what + when"). Confirm then UPDATEs instead of INSERTing.
func (f *Flow) handleManageEdit(ctx context.Context, ev events.PostbackEvent, idStr string) {
	rem := f.lookupManaged(ctx, ev, idStr)
	if rem == nil {
		return
	}
	state := &State{
		Step:         StepAwaitDetails,
		TargetUserID: rem.TargetUserID,
		Message:      rem.Message,
		EditingID:    rem.ID,
	}
	f.save(ctx, ev.UserID, ev.ReplyToken, state)
	f.reply(ev.UserID, ev.ReplyToken, fmt.Sprintf(
		"แก้ไข \"%s\" ยังไงดี? พิมพ์เวลาใหม่ หรือพิมพ์ทั้งข้อความใหม่พร้อมเวลาเลยก็ได้ เช่น \"พรุ่งนี้ 9 โมง กินยา\"",
		rem.Message), nil)
}

// handleManageDelete cancels the reminder (status guard makes an armed Redis
// timer fire into nothing).
func (f *Flow) handleManageDelete(ctx context.Context, ev events.PostbackEvent, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		f.reply(ev.UserID, ev.ReplyToken, "ไม่พบรายการนี้แล้วน้า ดูรายการล่าสุดด้วย /reminders ได้เลย", nil)
		return
	}
	ok, err := f.store.CancelReminder(ctx, id, ev.UserID)
	if err != nil {
		log.Error().Str("user_id", ev.UserID).Int64("reminder_id", id).Err(err).Msg("flow: cancel reminder failed")
		f.reply(ev.UserID, ev.ReplyToken, "ขอโทษน้า ลบไม่สำเร็จ ลองใหม่อีกครั้งนะ", nil)
		return
	}
	if !ok {
		f.reply(ev.UserID, ev.ReplyToken, "ไม่พบรายการนี้แล้วน้า อาจถูกลบหรือส่งไปแล้ว ดูรายการล่าสุดด้วย /reminders ได้เลย", nil)
		return
	}
	if err := f.state.Delete(ctx, ev.UserID); err != nil {
		log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: state delete failed")
	}
	log.Info().Str("user_id", ev.UserID).Int64("reminder_id", id).Msg("flow: reminder deleted")
	f.reply(ev.UserID, ev.ReplyToken, "ลบแล้วน้า 🗑️", nil)
}

// lookupManaged parses and loads a managed reminder, replying (and returning
// nil) when it's gone or never belonged to this user.
func (f *Flow) lookupManaged(ctx context.Context, ev events.PostbackEvent, idStr string) *store.Reminder {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		f.reply(ev.UserID, ev.ReplyToken, "ไม่พบรายการนี้แล้วน้า ดูรายการล่าสุดด้วย /reminders ได้เลย", nil)
		return nil
	}
	rem, err := f.store.GetReminder(ctx, id, ev.UserID)
	if err != nil {
		log.Error().Str("user_id", ev.UserID).Int64("reminder_id", id).Err(err).Msg("flow: get reminder failed")
		f.reply(ev.UserID, ev.ReplyToken, "ขอโทษน้า ดึงรายการไม่สำเร็จ ลองใหม่อีกครั้งนะ", nil)
		return nil
	}
	if rem == nil {
		f.reply(ev.UserID, ev.ReplyToken, "ไม่พบรายการนี้แล้วน้า อาจถูกลบหรือส่งไปแล้ว ดูรายการล่าสุดด้วย /reminders ได้เลย", nil)
		return nil
	}
	return rem
}

func (f *Flow) handleTarget(ctx context.Context, ev events.PostbackEvent, state *State, choice string) {
	switch choice {
	case "self":
		state.TargetUserID = ev.UserID
		f.advanceAfterTarget(ctx, ev.UserID, ev.ReplyToken, state)
	case "other":
		users, err := f.store.ListUsers(ctx, ev.UserID, maxTargetButtons)
		if err != nil {
			log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: list users failed")
			f.reply(ev.UserID, ev.ReplyToken, "ขอโทษน้า ดึงรายชื่อไม่สำเร็จ ลองใหม่อีกครั้งนะ", nil)
			return
		}
		if len(users) == 0 {
			f.reply(ev.UserID, ev.ReplyToken, "ยังไม่มีรายชื่อคนอื่นในระบบเลยน้า ให้เขาทักแชทนี้ก่อน แล้วค่อยตั้งเตือนให้ได้", nil)
			if err := f.state.Delete(ctx, ev.UserID); err != nil {
				log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: state delete failed")
			}
			return
		}
		buttons := make([]events.QuickReply, 0, len(users)+1)
		for _, u := range users {
			label := truncateLabel(u.DisplayName)
			buttons = append(buttons, events.QuickReply{
				Label:       label,
				Data:        "flow=rem&a=user&v=" + url.QueryEscape(u.ID),
				DisplayText: label,
			})
		}
		buttons = append(buttons, events.QuickReply{Label: "ยกเลิก", Data: "flow=rem&a=cancel", DisplayText: "ยกเลิก"})
		state.Step = StepAwaitUser
		f.save(ctx, ev.UserID, ev.ReplyToken, state)
		f.reply(ev.UserID, ev.ReplyToken, "จะเตือนใคร? เลือกจากรายชื่อได้เลย", buttons)
	default:
		f.promptForStep(ctx, ev.UserID, ev.ReplyToken, state)
	}
}

func (f *Flow) handleUserPick(ctx context.Context, ev events.PostbackEvent, state *State, target string) {
	if state.Step != StepAwaitUser || target == "" {
		f.promptForStep(ctx, ev.UserID, ev.ReplyToken, state)
		return
	}
	state.TargetUserID = target
	f.advanceAfterTarget(ctx, ev.UserID, ev.ReplyToken, state)
}

// advanceAfterTarget moves to confirmation when message+time are already
// known (pre-filled at start), otherwise asks for the details.
func (f *Flow) advanceAfterTarget(ctx context.Context, userID, replyToken string, state *State) {
	if state.Message != "" && f.validTime(state.RemindAt) {
		f.askConfirm(ctx, userID, replyToken, state)
		return
	}
	state.Step = StepAwaitDetails
	f.save(ctx, userID, replyToken, state)
	f.reply(userID, replyToken, "จะให้เตือนว่าอะไร เมื่อไหร่? เช่น \"พรุ่งนี้ 9 โมง กินยา\"", nil)
}

func (f *Flow) handleDetails(ctx context.Context, ev events.ReminderRequestEvent, state *State, text string) {
	if ev.Message != "" {
		state.Message = ev.Message
	} else if state.Message == "" {
		// Extraction found nothing upstream: keep the user's own words.
		state.Message = text
	}
	if at := parseRemindAt(ev.RemindAt); !at.IsZero() {
		state.RemindAt = at
	}

	if state.Message == "" {
		f.save(ctx, ev.UserID, ev.ReplyToken, state)
		f.reply(ev.UserID, ev.ReplyToken, "ยังไม่รู้ว่าจะให้เตือนเรื่องอะไรน้า พิมพ์บอกอีกทีได้ไหม เช่น \"พรุ่งนี้ 9 โมง กินยา\"", nil)
		return
	}
	if !f.validTime(state.RemindAt) {
		state.RemindAt = time.Time{}
		f.save(ctx, ev.UserID, ev.ReplyToken, state)
		f.reply(ev.UserID, ev.ReplyToken, fmt.Sprintf("จะเตือนเรื่อง \"%s\" ตอนไหนดี? บอกเวลาที่ยังมาไม่ถึงน้า เช่น \"พรุ่งนี้ 9 โมง\"", state.Message), nil)
		return
	}
	f.askConfirm(ctx, ev.UserID, ev.ReplyToken, state)
}

func (f *Flow) askConfirm(ctx context.Context, userID, replyToken string, state *State) {
	state.Step = StepAwaitConfirm
	f.save(ctx, userID, replyToken, state)

	target := "ตัวเอง"
	if state.TargetUserID != userID {
		if name, err := f.store.GetDisplayName(ctx, state.TargetUserID); err == nil && name != "" {
			target = name
		} else {
			target = "เพื่อน"
		}
	}
	preview := fmt.Sprintf("ตั้งเตือน %s: \"%s\"\n%s\nยืนยันไหม?",
		target, state.Message, formatBangkok(state.RemindAt))
	f.reply(userID, replyToken, preview, []events.QuickReply{
		{Label: "ยืนยัน", Data: "flow=rem&a=confirm", DisplayText: "ยืนยัน"},
		{Label: "แก้ไข", Data: "flow=rem&a=edit", DisplayText: "แก้ไข"},
		{Label: "ยกเลิก", Data: "flow=rem&a=cancel", DisplayText: "ยกเลิก"},
	})
}

func (f *Flow) handleConfirm(ctx context.Context, ev events.PostbackEvent, state *State) {
	if state.Step != StepAwaitConfirm || state.Message == "" || state.TargetUserID == "" || !f.validTime(state.RemindAt) {
		f.promptForStep(ctx, ev.UserID, ev.ReplyToken, state)
		return
	}
	// An edit rewrites the existing row (and resets it to pending so the
	// scheduler re-arms at the new time); otherwise insert a new one.
	if state.EditingID != 0 {
		ok, err := f.store.UpdateReminder(ctx, state.EditingID, ev.UserID, state.Message, state.RemindAt)
		if err != nil {
			log.Error().Str("user_id", ev.UserID).Int64("reminder_id", state.EditingID).Err(err).Msg("flow: update reminder failed")
			f.reply(ev.UserID, ev.ReplyToken, "ขอโทษน้า บันทึกไม่สำเร็จ ลองยืนยันอีกครั้งนะ", nil)
			return
		}
		if err := f.state.Delete(ctx, ev.UserID); err != nil {
			log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: state delete failed")
		}
		if !ok {
			f.reply(ev.UserID, ev.ReplyToken, "ไม่พบรายการนี้แล้วน้า อาจถูกลบหรือส่งไปแล้ว ดูรายการล่าสุดด้วย /reminders ได้เลย", nil)
			return
		}
		log.Info().Str("user_id", ev.UserID).Int64("reminder_id", state.EditingID).Time("remind_at", state.RemindAt).Msg("flow: reminder updated")
		f.reply(ev.UserID, ev.ReplyToken, fmt.Sprintf("แก้ไขแล้ว ⏰ จะเตือน \"%s\" %s น้า",
			state.Message, formatBangkok(state.RemindAt)), nil)
		return
	}

	id, err := f.store.CreateReminder(ctx, ev.UserID, state.TargetUserID, state.Message, state.RemindAt)
	if err != nil {
		log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: create reminder failed")
		f.reply(ev.UserID, ev.ReplyToken, "ขอโทษน้า บันทึกไม่สำเร็จ ลองยืนยันอีกครั้งนะ", nil)
		return
	}
	if err := f.state.Delete(ctx, ev.UserID); err != nil {
		log.Error().Str("user_id", ev.UserID).Err(err).Msg("flow: state delete failed")
	}
	log.Info().Str("user_id", ev.UserID).Int64("reminder_id", id).Time("remind_at", state.RemindAt).Msg("flow: reminder saved")
	f.reply(ev.UserID, ev.ReplyToken, fmt.Sprintf("บันทึกแล้ว ⏰ จะเตือน \"%s\" %s น้า",
		state.Message, formatBangkok(state.RemindAt)), nil)
}

// cancel ends the flow (button or typed) and acknowledges it.
func (f *Flow) cancel(ctx context.Context, userID, replyToken string) {
	if err := f.state.Delete(ctx, userID); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("flow: state delete failed")
	}
	f.reply(userID, replyToken, "ยกเลิกแล้วน้า", nil)
}

// promptForStep re-sends the prompt for whatever step the flow is stuck on.
func (f *Flow) promptForStep(ctx context.Context, userID, replyToken string, state *State) {
	switch state.Step {
	case StepAwaitTarget:
		f.reply(userID, replyToken, "จะให้เตือนใครดี? กดปุ่มเลือกได้เลยน้า", []events.QuickReply{
			{Label: "เตือนตัวเอง", Data: "flow=rem&a=target&v=self", DisplayText: "เตือนตัวเอง"},
			{Label: "เตือนคนอื่น", Data: "flow=rem&a=target&v=other", DisplayText: "เตือนคนอื่น"},
			{Label: "ยกเลิก", Data: "flow=rem&a=cancel", DisplayText: "ยกเลิก"},
		})
	case StepAwaitUser:
		// Rebuild the picker via the target handler.
		f.handleTarget(ctx, events.PostbackEvent{UserID: userID, ReplyToken: replyToken}, state, "other")
	case StepAwaitConfirm:
		f.askConfirm(ctx, userID, replyToken, state)
	case StepManage:
		f.startManage(ctx, userID, replyToken)
	default:
		f.reply(userID, replyToken, "จะให้เตือนว่าอะไร เมื่อไหร่? เช่น \"พรุ่งนี้ 9 โมง กินยา\"", nil)
	}
}

// parseRemindAt parses the RFC3339 time consumer-llm-processor extracted;
// empty or malformed values come back zero (the flow re-asks the user).
func parseRemindAt(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		log.Error().Str("remind_at", s).Err(err).Msg("flow: bad remind_at from upstream - ignoring")
		return time.Time{}
	}
	return at
}

// validTime accepts only future times (1 minute of clock-skew allowance).
func (f *Flow) validTime(t time.Time) bool {
	return !t.IsZero() && t.After(f.now().Add(-time.Minute))
}

func (f *Flow) save(ctx context.Context, userID, replyToken string, state *State) {
	if err := f.state.Put(ctx, userID, state); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("flow: state save failed")
	}
}

func (f *Flow) reply(userID, replyToken, text string, quickReplies []events.QuickReply) {
	if err := f.publish(events.ReplyEvent{
		UserID:       userID,
		ReplyToken:   replyToken,
		Text:         text,
		QuickReplies: quickReplies,
	}); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("flow: reply publish failed")
	}
}

func formatBangkok(t time.Time) string {
	return t.In(bangkok).Format("วันที่ 02/01/2006 เวลา 15:04 น.")
}

func truncateLabel(s string) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) == 0 {
		return "(no name)"
	}
	if len(runes) > maxLabelRunes {
		return string(runes[:maxLabelRunes-1]) + "…"
	}
	return string(runes)
}
