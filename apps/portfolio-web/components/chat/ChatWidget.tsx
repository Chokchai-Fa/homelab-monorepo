"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { BsChatDots } from "react-icons/bs";
import { FiSend, FiTrash2, FiX } from "react-icons/fi";

type ChatMessage = {
  role: "user" | "assistant";
  text: string;
};

const WELCOME: ChatMessage = {
  role: "assistant",
  text: "Hi! I'm Chokchai's AI assistant, running on his self-hosted homelab. Ask me anything about his experience, skills or projects.",
};

const SUGGESTIONS = [
  "What's his experience with Go and Kubernetes?",
  "How does this chatbot work?",
  "ประสบการณ์ทำงานของเขามีอะไรบ้าง",
];

const SESSION_KEY = "portfolio-chat-session";

const getSessionId = (): string => {
  try {
    const existing = localStorage.getItem(SESSION_KEY);
    if (existing) return existing;
    const id =
      typeof crypto.randomUUID === "function"
        ? crypto.randomUUID()
        : `${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
    localStorage.setItem(SESSION_KEY, id);
    return id;
  } catch {
    // localStorage unavailable (private mode): a throwaway session still works.
    return `${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
  }
};

const ChatWidget = (): JSX.Element => {
  const [open, setOpen] = useState(false);
  const [messages, setMessages] = useState<ChatMessage[]>([WELCOME]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, loading, open]);

  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  const send = useCallback(
    async (text: string) => {
      const message = text.trim();
      if (!message || loading) return;
      setInput("");
      setMessages((prev) => [...prev, { role: "user", text: message }]);
      setLoading(true);
      try {
        const res = await fetch("/api/chat", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ sessionId: getSessionId(), message }),
        });
        const data: { text?: string; error?: string } = await res
          .json()
          .catch(() => ({}));
        const reply =
          res.ok && data.text
            ? data.text
            : data.error ?? "Sorry, something went wrong. Please try again.";
        setMessages((prev) => [...prev, { role: "assistant", text: reply }]);
      } catch {
        setMessages((prev) => [
          ...prev,
          {
            role: "assistant",
            text: "Sorry, I couldn't reach the assistant. Please try again.",
          },
        ]);
      } finally {
        setLoading(false);
      }
    },
    [loading]
  );

  const clearChat = useCallback(() => {
    setMessages([WELCOME]);
    // Best-effort server-side reset; the widget doesn't wait for it.
    fetch("/api/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sessionId: getSessionId(), message: "/reset" }),
    }).catch(() => undefined);
  }, []);

  return (
    <div className="fixed bottom-6 right-6 z-40 flex flex-col items-end gap-4">
      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0, y: 24, scale: 0.96 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 24, scale: 0.96 }}
            transition={{ duration: 0.2, ease: "easeOut" }}
            className="flex h-[70vh] max-h-[560px] w-[calc(100vw-3rem)] max-w-[380px] flex-col overflow-hidden rounded-2xl border border-white/10 bg-primary shadow-2xl shadow-black/50"
          >
            <div className="flex items-center justify-between border-b border-white/10 px-4 py-3">
              <div>
                <p className="font-semibold text-accent">Ask AI about me</p>
                <p className="text-xs text-white/50">
                  answered live from my homelab
                </p>
              </div>
              <div className="flex items-center gap-1">
                <button
                  onClick={clearChat}
                  aria-label="Clear chat"
                  className="rounded-full p-2 text-white/50 transition-colors hover:bg-white/10 hover:text-white"
                >
                  <FiTrash2 />
                </button>
                <button
                  onClick={() => setOpen(false)}
                  aria-label="Close chat"
                  className="rounded-full p-2 text-white/50 transition-colors hover:bg-white/10 hover:text-white"
                >
                  <FiX />
                </button>
              </div>
            </div>

            <div className="flex-1 space-y-3 overflow-y-auto px-4 py-3">
              {messages.map((m, i) => (
                <div
                  key={i}
                  className={`flex ${m.role === "user" ? "justify-end" : "justify-start"}`}
                >
                  <div
                    className={`max-w-[85%] whitespace-pre-wrap rounded-2xl px-3 py-2 text-sm leading-relaxed ${
                      m.role === "user"
                        ? "rounded-br-sm bg-accent text-primary"
                        : "rounded-bl-sm bg-white/10 text-white/90"
                    }`}
                  >
                    {m.text}
                  </div>
                </div>
              ))}
              {loading && (
                <div className="flex justify-start">
                  <div className="flex gap-1 rounded-2xl rounded-bl-sm bg-white/10 px-3 py-3">
                    {[0, 1, 2].map((i) => (
                      <motion.span
                        key={i}
                        className="h-1.5 w-1.5 rounded-full bg-accent"
                        animate={{ opacity: [0.3, 1, 0.3] }}
                        transition={{
                          duration: 1,
                          repeat: Infinity,
                          delay: i * 0.2,
                        }}
                      />
                    ))}
                  </div>
                </div>
              )}
              {messages.length === 1 && !loading && (
                <div className="flex flex-wrap gap-2 pt-2">
                  {SUGGESTIONS.map((s) => (
                    <button
                      key={s}
                      onClick={() => send(s)}
                      className="rounded-full border border-accent/40 px-3 py-1.5 text-left text-xs text-accent transition-colors hover:bg-accent hover:text-primary"
                    >
                      {s}
                    </button>
                  ))}
                </div>
              )}
              <div ref={bottomRef} />
            </div>

            <form
              onSubmit={(e) => {
                e.preventDefault();
                send(input);
              }}
              className="flex items-center gap-2 border-t border-white/10 p-3"
            >
              <input
                ref={inputRef}
                value={input}
                onChange={(e) => setInput(e.target.value)}
                placeholder="Ask about Chokchai..."
                maxLength={1000}
                className="h-10 flex-1 rounded-full border border-white/10 bg-white/5 px-4 text-sm text-white placeholder:text-white/40 focus:border-accent focus:outline-none"
              />
              <button
                type="submit"
                disabled={loading || !input.trim()}
                aria-label="Send message"
                className="flex h-10 w-10 items-center justify-center rounded-full bg-accent text-primary transition-opacity disabled:opacity-40"
              >
                <FiSend />
              </button>
            </form>
          </motion.div>
        )}
      </AnimatePresence>

      <motion.button
        initial={{ opacity: 0, scale: 0 }}
        animate={{ opacity: 1, scale: 1 }}
        transition={{ delay: 2.6, duration: 0.3 }}
        whileHover={{ scale: 1.08 }}
        whileTap={{ scale: 0.95 }}
        onClick={() => setOpen((o) => !o)}
        aria-label={open ? "Close chat" : "Open chat"}
        className="flex h-14 w-14 items-center justify-center rounded-full bg-accent text-2xl text-primary shadow-lg shadow-accent/30"
      >
        {open ? <FiX /> : <BsChatDots />}
      </motion.button>
    </div>
  );
};

export default ChatWidget;
