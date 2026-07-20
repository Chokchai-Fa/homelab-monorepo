"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import ReactMarkdown from "react-markdown";
import { BsChatDots } from "react-icons/bs";
import { FiSend, FiTrash2, FiX } from "react-icons/fi";

type ChatMessage = {
  role: "user" | "assistant";
  text: string;
  streaming?: boolean;
};

type Status = { nats?: boolean; host?: string; uptime?: string };

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
const NUDGE_KEY = "portfolio-chat-nudge-seen";

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

// TypingDots is the "thinking" indicator shown before the first token arrives.
const TypingDots = (): JSX.Element => (
  <div className="flex gap-1 py-1">
    {[0, 1, 2].map((i) => (
      <motion.span
        key={i}
        className="h-1.5 w-1.5 rounded-full bg-accent"
        animate={{ opacity: [0.3, 1, 0.3] }}
        transition={{ duration: 1, repeat: Infinity, delay: i * 0.2 }}
      />
    ))}
  </div>
);

// Minimal markdown styling so answers (incl. code blocks) read well inside the
// small bubble without pulling in a typography plugin.
const markdownComponents = {
  a: (props: React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a {...props} target="_blank" rel="noopener noreferrer" className="text-accent underline" />
  ),
  code: (props: React.HTMLAttributes<HTMLElement>) => (
    <code {...props} className="rounded bg-black/40 px-1 py-0.5 text-[0.85em]" />
  ),
  pre: (props: React.HTMLAttributes<HTMLPreElement>) => (
    <pre {...props} className="my-1 overflow-x-auto rounded-lg bg-black/50 p-2 text-[0.85em]" />
  ),
  ul: (props: React.HTMLAttributes<HTMLUListElement>) => (
    <ul {...props} className="my-1 list-disc pl-4" />
  ),
  ol: (props: React.HTMLAttributes<HTMLOListElement>) => (
    <ol {...props} className="my-1 list-decimal pl-4" />
  ),
};

const ChatWidget = (): JSX.Element => {
  const [open, setOpen] = useState(false);
  const [messages, setMessages] = useState<ChatMessage[]>([WELCOME]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [nudge, setNudge] = useState(false);
  const [status, setStatus] = useState<Status | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, loading, open]);

  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  // First-visit discovery nudge near the button.
  useEffect(() => {
    try {
      if (!localStorage.getItem(NUDGE_KEY)) {
        const t = setTimeout(() => setNudge(true), 3500);
        return () => clearTimeout(t);
      }
    } catch {
      /* ignore */
    }
  }, []);

  const dismissNudge = useCallback(() => {
    setNudge(false);
    try {
      localStorage.setItem(NUDGE_KEY, "1");
    } catch {
      /* ignore */
    }
  }, []);

  // Fetch live homelab status whenever the panel opens.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    fetch("/api/chat/status")
      .then((r) => r.json())
      .then((d: Status) => {
        if (!cancelled) setStatus(d);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [open]);

  const toggleOpen = useCallback(() => {
    setOpen((o) => !o);
    dismissNudge();
  }, [dismissNudge]);

  // updateLast replaces the trailing (assistant) message's text/streaming flag.
  const updateLast = useCallback((text: string, streaming: boolean) => {
    setMessages((prev) => {
      const copy = [...prev];
      const last = copy[copy.length - 1];
      if (last && last.role === "assistant") {
        copy[copy.length - 1] = { ...last, text, streaming };
      }
      return copy;
    });
  }, []);

  const send = useCallback(
    async (text: string) => {
      const message = text.trim();
      if (!message || loading) return;
      setInput("");
      setMessages((prev) => [
        ...prev,
        { role: "user", text: message },
        { role: "assistant", text: "", streaming: true },
      ]);
      setLoading(true);

      try {
        const res = await fetch("/api/chat/stream", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ sessionId: getSessionId(), message }),
        });

        const contentType = res.headers.get("content-type") ?? "";
        if (!res.ok || !contentType.includes("text/event-stream") || !res.body) {
          const data: { error?: string } = await res.json().catch(() => ({}));
          updateLast(data.error ?? "Sorry, something went wrong. Please try again.", false);
          return;
        }

        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        let acc = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          // SSE frames are separated by a blank line.
          let sep;
          while ((sep = buffer.indexOf("\n\n")) !== -1) {
            const rawEvent = buffer.slice(0, sep);
            buffer = buffer.slice(sep + 2);
            const dataLine = rawEvent
              .split("\n")
              .find((l) => l.startsWith("data:"));
            if (!dataLine) continue;
            const payload = dataLine.slice(5).trim();
            if (!payload) continue;
            try {
              const frame: { delta?: string; done?: boolean } = JSON.parse(payload);
              if (frame.delta) {
                acc += frame.delta;
                updateLast(acc, true);
              }
            } catch {
              /* ignore malformed frame */
            }
          }
        }
        updateLast(
          acc || "Sorry, I couldn't reach the assistant. Please try again.",
          false
        );
      } catch {
        updateLast("Sorry, I couldn't reach the assistant. Please try again.", false);
      } finally {
        setLoading(false);
      }
    },
    [loading, updateLast]
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

  const online = status?.nats === true;

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
                <p
                  className="flex items-center gap-1.5 text-xs text-white/50"
                  title={
                    status?.host
                      ? `Answered by ${status.host}${status.uptime ? ` · up ${status.uptime}` : ""}`
                      : "homelab status"
                  }
                >
                  <span
                    className={`inline-block h-1.5 w-1.5 rounded-full ${
                      online ? "bg-green-400" : "bg-amber-400"
                    }`}
                  />
                  {online ? "Live on my homelab" : "answered from my homelab"}
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
                    className={`max-w-[85%] rounded-2xl px-3 py-2 text-sm leading-relaxed ${
                      m.role === "user"
                        ? "whitespace-pre-wrap rounded-br-sm bg-accent text-primary"
                        : "rounded-bl-sm bg-white/10 text-white/90"
                    }`}
                  >
                    {m.role === "assistant" ? (
                      m.streaming && m.text === "" ? (
                        <TypingDots />
                      ) : (
                        <div className="space-y-1 break-words [&_p]:my-0">
                          <ReactMarkdown components={markdownComponents}>
                            {m.text}
                          </ReactMarkdown>
                        </div>
                      )
                    ) : (
                      m.text
                    )}
                  </div>
                </div>
              ))}
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

      {/* First-visit discovery nudge */}
      <AnimatePresence>
        {nudge && !open && (
          <motion.button
            initial={{ opacity: 0, y: 8, scale: 0.9 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, scale: 0.9 }}
            onClick={toggleOpen}
            className="max-w-[240px] rounded-2xl rounded-br-sm border border-accent/30 bg-primary px-3 py-2 text-left text-xs text-white/80 shadow-lg shadow-black/40"
          >
            👋 New here? Ask my AI assistant anything about my work — it runs on
            my own homelab.
          </motion.button>
        )}
      </AnimatePresence>

      <motion.button
        initial={{ opacity: 0, scale: 0 }}
        animate={{ opacity: 1, scale: 1 }}
        transition={{ delay: 2.6, duration: 0.3 }}
        whileHover={{ scale: 1.08 }}
        whileTap={{ scale: 0.95 }}
        onClick={toggleOpen}
        aria-label={open ? "Close chat" : "Open chat"}
        className="flex h-14 w-14 items-center justify-center rounded-full bg-accent text-2xl text-primary shadow-lg shadow-accent/30"
      >
        {open ? <FiX /> : <BsChatDots />}
      </motion.button>
    </div>
  );
};

export default ChatWidget;
