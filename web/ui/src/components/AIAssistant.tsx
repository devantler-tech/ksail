import { Bot, Check, Send, Sparkles, User, Wrench, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { confirmChatTool, errorMessage, streamChat, type ChatEvent, type ChatMessage } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { Button } from "./ui.tsx";

// suggestions seed an empty conversation with useful starting prompts.
const SUGGESTIONS = [
  "What's running in this cluster?",
  "Are any pods failing, and why?",
  "Explain this cluster's GitOps setup.",
];

// ConfirmStatus tracks where a write-tool confirmation is in its lifecycle: awaiting the user's
// decision, or resolved (approved/denied) — once resolved the buttons are replaced by the outcome.
type ConfirmStatus = "pending" | "approved" | "denied";

// PendingConfirm is one write tool the assistant asked to run, surfaced as an inline confirmation card.
// id is the backend's confirmId (echoed back to resolve it); tool/summary describe the action.
interface PendingConfirm {
  id: string;
  tool: string;
  summary?: string;
  status: ConfirmStatus;
}

// AIAssistant is the web UI's AI chat panel, streaming the assistant's reply from the backend's chat
// endpoint (GitHub Copilot). It is cluster-aware: the active cluster is sent as context so the
// assistant scopes its answers. When allowWrite is set the assistant may request write actions, each
// gated behind an inline Approve/Deny confirmation; otherwise it stays read-only. App renders it only
// when the backend advertises the aiChat capability.
export function AIAssistant({
  clusterName,
  namespace,
  allowWrite,
}: {
  clusterName: string | null;
  namespace: string | null;
  allowWrite: boolean;
}) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [confirms, setConfirms] = useState<PendingConfirm[]>([]);
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
  }, [messages, confirms]);

  // resolveConfirm posts the user's Approve/Deny decision for a pending write tool and marks the card
  // resolved, so the blocked turn proceeds (or rejects) and the buttons are replaced by the outcome.
  const resolveConfirm = useCallback(async (id: string, approved: boolean) => {
    setConfirms((prev) =>
      prev.map((confirm) =>
        confirm.id === id ? { ...confirm, status: approved ? "approved" : "denied" } : confirm,
      ),
    );

    try {
      await confirmChatTool(id, approved);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);

  const send = useCallback(
    async (text: string) => {
      const trimmed = text.trim();
      if (trimmed === "" || streaming) {
        return;
      }

      setError(null);
      setInput("");
      setConfirms([]);

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
          (event) => applyChatEvent(event, setMessages, setConfirms, setError),
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
          <EmptyConversation onPick={(text) => void send(text)} disabled={streaming} allowWrite={allowWrite} />
        ) : (
          messages.map((message, index) => <MessageBubble key={index} message={message} />)
        )}
        {confirms.map((confirm) => (
          <ConfirmCard
            key={confirm.id}
            confirm={confirm}
            onApprove={() => void resolveConfirm(confirm.id, true)}
            onDeny={() => void resolveConfirm(confirm.id, false)}
          />
        ))}
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

// applyChatEvent folds one streamed event into the panel state: deltas append to the in-progress
// assistant message, tool notices are appended as a subtle marker, a tool-confirm spawns an inline
// confirmation card, and an error event surfaces the message. A "done" event needs no state change
// (the assistant message is already complete).
function applyChatEvent(
  event: ChatEvent,
  setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>,
  setConfirms: React.Dispatch<React.SetStateAction<PendingConfirm[]>>,
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

  if (event.type === "tool-confirm" && event.confirmId) {
    const id = event.confirmId;
    setConfirms((prev) => [
      ...prev,
      { id, tool: event.text ?? "an action", summary: event.summary, status: "pending" },
    ]);

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

// ConfirmCard renders one write-tool confirmation inline: the tool name, its summary, and Approve/Deny
// buttons while pending; once resolved the buttons are replaced by the recorded outcome so the user can
// see what they decided.
function ConfirmCard({
  confirm,
  onApprove,
  onDeny,
}: {
  confirm: PendingConfirm;
  onApprove: () => void;
  onDeny: () => void;
}) {
  return (
    <div className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-500/40 dark:bg-amber-500/10">
      <div className="flex items-center gap-2 font-medium text-amber-900 dark:text-amber-200">
        <Wrench className="size-4" aria-hidden />
        Approve write action: <code className="font-mono">{confirm.tool}</code>
      </div>
      {confirm.summary ? (
        <p className="mt-1 text-amber-800 dark:text-amber-300/90">{confirm.summary}</p>
      ) : null}
      {confirm.status === "pending" ? (
        <div className="mt-3 flex gap-2">
          <Button type="button" onClick={onApprove}>
            <Check className="size-4" aria-hidden />
            Approve
          </Button>
          <Button type="button" variant="secondary" onClick={onDeny}>
            <X className="size-4" aria-hidden />
            Deny
          </Button>
        </div>
      ) : (
        <p
          className={cx(
            "mt-2 inline-flex items-center gap-1.5 font-medium",
            confirm.status === "approved"
              ? "text-green-700 dark:text-green-400"
              : "text-slate-500 dark:text-slate-400",
          )}
        >
          {confirm.status === "approved" ? (
            <>
              <Check className="size-3.5" aria-hidden />
              Approved
            </>
          ) : (
            <>
              <X className="size-3.5" aria-hidden />
              Denied
            </>
          )}
        </p>
      )}
    </div>
  );
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

// EmptyConversation is the starting state: a short intro plus clickable prompt suggestions. The intro
// reflects whether the assistant may make changes (allowWrite) so the user knows what to expect.
function EmptyConversation({
  onPick,
  disabled,
  allowWrite,
}: {
  onPick: (text: string) => void;
  disabled: boolean;
  allowWrite: boolean;
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 text-center">
      <div className="flex size-12 items-center justify-center rounded-full bg-blue-50 text-blue-600 dark:bg-blue-500/10 dark:text-blue-400">
        <Sparkles className="size-6" aria-hidden />
      </div>
      <div>
        <p className="font-medium text-slate-900 dark:text-white">KSail Assistant</p>
        <p className="mt-1 max-w-md text-sm text-slate-500 dark:text-slate-400">
          {allowWrite
            ? "Ask about your clusters, workloads, and KSail. It can inspect freely; any change is paused for your approval first."
            : "Ask about your clusters, workloads, and KSail. The assistant is read-only — it can inspect, but will not change anything."}
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
