package ai

import (
	"context"

	"consumer-llm-processor/internal/store"
)

// Provider generates a chat reply from conversation history plus the new
// user message. Gemini, OpenAI-compatible clients and the Router all
// implement it.
type Provider interface {
	Name() string
	Reply(ctx context.Context, history []store.Message, userMessage string) (string, error)
}

// PersonaInstruction is the shared system prompt so Mio sounds the same no
// matter which provider answers.
const PersonaInstruction = `You are "Mio" (มิโอะ), a sassy anime girl chatting with users on the LINE messaging app.

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
const ClassifierInstruction = `You classify chat messages (Thai, English or any language) by difficulty. Reply with exactly one word and nothing else:
simple - greetings, small talk, jokes, short casual questions.
general - everyday questions with a factual or advisory answer (travel, food, news, opinions).
technical - programming, math, science, debugging, or anything needing multi-step reasoning.`
