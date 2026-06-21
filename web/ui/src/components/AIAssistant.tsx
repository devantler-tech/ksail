import { Bot, Send, Sparkles, User, Wrench } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage, streamChat, type ChatEvent, type ChatMessage } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { Button } from "./ui.tsx";

// suggestions seed an empty conversation with useful, read-only starting prompts.
const SUGGESTIONS = [
  "What's running in this cluster?",
  "Are any pods failing, and why?",
  "Explain this cluster's GitOps setup.",
];

// AIAssistant is the web UI's AI chat panel, streaming the assistant's reply from the backend's chat
// endpoint (GitHub Copilot, read-only). It is cluster-aware: the active cluster is sent as context so
// the assistant scopes its answers. App renders it only when the backend advertises the aiChat
// capability.
export function AIAssistant({
  clusterName,
  namespace,
}: {
  clusterName: string | null;
  namespace: string | null;
}) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Abort an in-flight turn if the panel unmounts (e.g. the user navigates away).
  useEffect(() => () => abortRef.current?.abort(), []);

  // Keep the latest message in view as the reply streams in.
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages]);

  const send = useCallback(
    async (text: string) => {
      const trimmed = text.trim();
      if (trimmed === "" || streaming) {
        return;
      }

      setError(null);
      setInput("");

      // Snapshot the history before appending the new turn; the backend replays it for context.
      const history = messages;
      setMessages((prev) => [...prev, { role: "user", content: trimmed }, { role: "assistant", content: "" }]);
      setStreaming(true);

      const controller = new AbortController();
      abortRef.current = controller;

      try {
        await streamChat(
          {
            message: trimmed,
            history,
            cluster: clusterName ?? undefined,
            namespace: namespace ?? undefined,
          },
          (event) => applyChatEvent(event, setMessages, setError),
          controller.signal,
        );
      } catch (err) {
        // An aborted turn (panel unmounted / new turn) is expected; surface anything else.
        if (!(err instanceof DOMException && err.name === "AbortError")) {
          setError(errorMessage(err));
        }
      } finally {
        setStreaming(false);
        abortRef.current = null;
      }
    },
    [streaming, messages, clusterName, namespace],
  );

  return (
    <div className="mx-auto flex h-full max-w-3xl flex-col gap-3">
      <div
        ref={scrollRef}
        className="flex-1 space-y-3 overflow-y-auto rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900"
      >
        {messages.length === 0 ? (
          <EmptyConversation onPick={(text) => void send(text)} disabled={streaming} />
        ) : (
          messages.map((message, index) => <MessageBubble key={index} message={message} />)
        )}
        {error ? (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-500/10 dark:text-red-300">
            {error}
          </p>
        ) : null}
      </div>

      <form
        className="flex items-end gap-2"
        onSubmit={(submitEvent) => {
          submitEvent.preventDefault();
          void send(input);
        }}
      >
        <textarea
          value={input}
          onChange={(changeEvent) => setInput(changeEvent.target.value)}
          onKeyDown={(keyEvent) => {
            // Enter sends; Shift+Enter inserts a newline.
            if (keyEvent.key === "Enter" && !keyEvent.shiftKey) {
              keyEvent.preventDefault();
              void send(input);
            }
          }}
          rows={2}
          placeholder={clusterName ? `Ask about ${clusterName}…` : "Ask about KSail or your clusters…"}
          className="min-h-[2.5rem] flex-1 resize-y rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
        />
        <Button type="submit" loading={streaming} disabled={input.trim() === ""}>
          <Send className="size-4" aria-hidden />
          Send
        </Button>
      </form>
    </div>
  );
}

// applyChatEvent folds one streamed event into the message list: deltas append to the in-progress
// assistant message, tool notices are appended as a subtle marker, and an error event surfaces the
// message. A "done" event needs no state change (the assistant message is already complete).
function applyChatEvent(
  event: ChatEvent,
  setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>,
  setError: (message: string) => void,
) {
  if (event.type === "error") {
    setError(event.text ?? "The assistant returned an error.");

    return;
  }

  if (event.type === "delta" && event.text) {
    appendToAssistant(setMessages, event.text);

    return;
  }

  if (event.type === "tool" && event.text) {
    // Messages render as plain text, so use a plain marker (not markdown italics) for the tool notice.
    appendToAssistant(setMessages, `\n\n→ Running ${event.text}…\n\n`);
  }
}

// appendToAssistant appends text to the last (assistant) message in the list.
function appendToAssistant(
  setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>,
  text: string,
) {
  setMessages((prev) => {
    const next = [...prev];
    const last = next[next.length - 1];
    if (last && last.role === "assistant") {
      next[next.length - 1] = { ...last, content: last.content + text };
    }

    return next;
  });
}

// MessageBubble renders one chat message, styled by role.
function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === "user";

  return (
    <div className={cx("flex gap-2.5", isUser ? "flex-row-reverse" : "flex-row")}>
      <div
        className={cx(
          "flex size-7 shrink-0 items-center justify-center rounded-full",
          isUser
            ? "bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-300"
            : "bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300",
        )}
      >
        {isUser ? <User className="size-4" aria-hidden /> : <Bot className="size-4" aria-hidden />}
      </div>
      <div
        className={cx(
          "max-w-[85%] whitespace-pre-wrap rounded-lg px-3 py-2 text-sm",
          isUser
            ? "bg-blue-600 text-white"
            : "bg-slate-100 text-slate-800 dark:bg-slate-800 dark:text-slate-100",
        )}
      >
        {message.content === "" ? (
          <span className="inline-flex items-center gap-1.5 text-slate-400">
            <Wrench className="size-3.5 animate-pulse" aria-hidden />
            Thinking…
          </span>
        ) : (
          message.content
        )}
      </div>
    </div>
  );
}

// EmptyConversation is the starting state: a short intro plus clickable read-only prompt suggestions.
function EmptyConversation({ onPick, disabled }: { onPick: (text: string) => void; disabled: boolean }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 text-center">
      <div className="flex size-12 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-500/10 dark:text-blue-400">
        <Sparkles className="size-6" aria-hidden />
      </div>
      <div>
        <p className="font-medium text-slate-900 dark:text-white">KSail Assistant</p>
        <p className="mt-1 max-w-md text-sm text-slate-500 dark:text-slate-400">
          Ask about your clusters, workloads, and KSail. The assistant is read-only — it can inspect, but
          will not change anything.
        </p>
      </div>
      <div className="flex flex-wrap justify-center gap-2">
        {SUGGESTIONS.map((suggestion) => (
          <button
            key={suggestion}
            type="button"
            disabled={disabled}
            onClick={() => onPick(suggestion)}
            className="rounded-full border border-slate-200 px-3 py-1.5 text-sm text-slate-600 transition-colors hover:border-blue-300 hover:bg-blue-50 hover:text-blue-700 disabled:opacity-50 dark:border-slate-700 dark:text-slate-300 dark:hover:border-blue-500/40 dark:hover:bg-blue-500/10 dark:hover:text-blue-300"
          >
            {suggestion}
          </button>
        ))}
      </div>
    </div>
  );
}
