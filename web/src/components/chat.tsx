"use client";

import { FormEvent, useState } from "react";
import { Mic, Send } from "lucide-react";

type Line = { from: "you" | "ano"; text: string };

type Heard = { results: ArrayLike<ArrayLike<{ transcript: string }>> };

type Listener = {
  lang: string;
  interimResults: boolean;
  continuous: boolean;
  onresult: ((event: Heard) => void) | null;
  onerror: (() => void) | null;
  onend: (() => void) | null;
  start: () => void;
  stop: () => void;
};

export function Chat({ onChanged }: { onChanged: () => void }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [lines, setLines] = useState<Line[]>([]);
  const [busy, setBusy] = useState(false);
  const [listening, setListening] = useState(false);

  async function send(request: string, spoken: boolean) {
    const trimmed = request.trim();
    if (!trimmed || busy) return;
    setText("");
    setLines((current) => [...current, { from: "you", text: trimmed }]);
    setBusy(true);
    try {
      const response = await fetch("/api/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text: trimmed }),
      });
      const body = (await response.json()) as { say?: string; changed?: boolean; error?: string };
      const say = body.say || body.error || "Ano did not answer";
      setLines((current) => [...current, { from: "ano", text: say }]);
      if (spoken) speak(say);
      if (body.changed) onChanged();
    } catch (err) {
      setLines((current) => [...current, { from: "ano", text: err instanceof Error ? err.message : "Ano did not answer" }]);
    } finally {
      setBusy(false);
    }
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    void send(text, false);
  }

  function listen() {
    const root = window as Window & { SpeechRecognition?: new () => Listener; webkitSpeechRecognition?: new () => Listener };
    const Maker = root.SpeechRecognition || root.webkitSpeechRecognition;
    if (!Maker) {
      setLines((current) => [...current, { from: "ano", text: "This browser has no voice input. Type the request." }]);
      return;
    }
    const heard = new Maker();
    heard.lang = "en-US";
    heard.interimResults = false;
    heard.continuous = false;
    heard.onresult = (event) => {
      const said = event.results[0]?.[0]?.transcript ?? "";
      setListening(false);
      void send(said, true);
    };
    heard.onerror = () => {
      setListening(false);
      setLines((current) => [...current, { from: "ano", text: "Ano did not hear that." }]);
    };
    heard.onend = () => setListening(false);
    setListening(true);
    heard.start();
  }

  return (
    <div className="fixed right-5 bottom-5 z-30 flex w-[min(24rem,calc(100vw-2rem))] flex-col items-end gap-2">
      {open ? (
        <section className="flex max-h-[70vh] w-full flex-col rounded-xl border bg-card shadow-[0_16px_48px_rgba(16,24,40,0.16)]">
          <div className="border-b px-3 py-2.5">
            <h2 className="text-sm font-semibold">Ask Ano</h2>
            <p className="text-[12px] leading-5 text-muted-foreground">Type or speak.</p>
          </div>
          <div className="min-h-40 flex-1 space-y-2 overflow-y-auto px-3 py-3">
            {lines.length === 0 ? <p className="text-[13px] text-muted-foreground">Say hello, or ask for a change.</p> : null}
            {lines.map((line, index) => (
              <p key={`${line.from}-${index}`} className={line.from === "you" ? "text-right text-sm" : "text-sm text-muted-foreground"}>
                {line.text}
              </p>
            ))}
          </div>
          <form onSubmit={onSubmit} className="flex items-center gap-2 border-t px-3 py-2.5">
            <input
              value={text}
              onChange={(event) => setText(event.target.value)}
              placeholder="Ask Ano"
              aria-label="Ask Ano"
              className="h-9 min-w-0 flex-1 rounded-md border bg-background px-2 text-sm"
            />
            <button type="button" onClick={listen} disabled={busy} aria-label={listening ? "Listening" : "Speak"} className="grid size-9 place-items-center rounded-md border disabled:opacity-60">
              <Mic className="size-4" />
            </button>
            <button type="submit" disabled={busy || text.trim() === ""} aria-label="Send" className="grid size-9 place-items-center rounded-md bg-primary text-primary-foreground disabled:opacity-60">
              <Send className="size-4" />
            </button>
          </form>
        </section>
      ) : null}
      <button type="button" onClick={() => setOpen((value) => !value)} className="h-10 rounded-full bg-primary px-4 text-sm font-medium text-primary-foreground shadow">
        {open ? "Close" : "Ask Ano"}
      </button>
    </div>
  );
}

function speak(text: string) {
  if (!window.speechSynthesis) return;
  window.speechSynthesis.cancel();
  const line = new SpeechSynthesisUtterance(text);
  line.lang = "en-US";
  window.speechSynthesis.speak(line);
}
