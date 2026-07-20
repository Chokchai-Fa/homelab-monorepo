package ai

import (
	"context"

	"consumer-llm-processor/internal/store"
)

// Image is a picture attached to the current user message. Only the current
// message can carry one - history is stored as text, never image bytes.
type Image struct {
	Data     []byte
	MimeType string
}

// Provider generates a chat reply from conversation history plus the new
// user message and an optional attached image. Gemini, OpenAI-compatible
// clients and the Router all implement it.
type Provider interface {
	Name() string
	Reply(ctx context.Context, history []store.Message, userMessage string, image *Image) (string, error)
}

// StreamProvider is a Provider that can emit its answer incrementally. emit
// is called with each new text delta as it arrives; the full concatenated
// answer is also returned. A provider that doesn't implement this is still
// usable in a streaming route - the router just emits its whole Reply as one
// delta. emit returning an error (e.g. the client disconnected) aborts
// generation.
type StreamProvider interface {
	Provider
	ReplyStream(ctx context.Context, history []store.Message, userMessage string, image *Image, emit func(delta string) error) (string, error)
}

// PersonaInstruction is the shared system prompt so Mio sounds the same no
// matter which provider answers.
const PersonaInstruction = `You are "Umaru" (อุมารุ), a sassy anime girl chatting with users on the LINE messaging app.

Personality (สาวอนิเมะแบบกวนตีน):
- Playful, cheeky and teasing like a mischievous anime character. A little snarky banter is welcome, but never actually rude or hurtful.
- Sprinkle in anime-style expressions and emoticons where natural, e.g. "เอ๊ะ~", "ฮึ่ม!", "อ๊ะๆ", "~", (¬‿¬), (＞ω＜).
- Tease first, help anyway: after the banter, still give a genuinely correct and useful answer.
- Please always keep this personal as girly and anime-like as possible, even when answering technical questions.

Rules:
- ALWAYS reply in the same language the user writes in. Thai in, Thai out; English in, English out; any other language likewise.
- Keep replies concise and chat-friendly: plain text only, no markdown formatting.`

// ClassifierInstruction is the system prompt for the small model that routes
// each question to a difficulty tier.
const ClassifierInstruction = `You classify chat messages (Thai, English or any language). Reply with exactly one word and nothing else:
simple - greetings, small talk, jokes, short casual questions.
general - everyday questions with a factual or advisory answer (travel, food, news, opinions).
technical - programming, math, science, debugging, or anything needing multi-step reasoning.
image - the user asks you to create, generate or draw a picture/artwork (e.g. "draw a cat", "วาดรูปแมวให้หน่อย", "generate an image of ...").
reminder - the user asks to be reminded of something at a time, or to set/schedule a reminder or alarm (e.g. "เตือนพรุ่งนี้ 9 โมง กินยา", "remind me to call mom at 6pm", "ตั้งเตือนตอนเย็น").`
