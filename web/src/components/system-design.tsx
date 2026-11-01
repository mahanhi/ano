export function SystemDesign({
  agent,
}: {
  agent?: { mode: string; detail: string; model: { name: string; url: string; ready: boolean; kind: string } };
}) {
  return (
    <div className="space-y-8">
      <header className="max-w-3xl">
        <h1 className="text-xl font-semibold tracking-tight">How this setup works</h1>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          Ano is the agent. It collects device metadata into SQLite, asks AI Inference for drop or allow, and exposes that to this screen. Passive mode only collects. Active mode enforces AI Inference. A person&apos;s button still overrides it. AI Inference is not on the packet path.
        </p>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          {agent
            ? `AI Inference. ${agent.model.ready ? "Ready" : "Not ready"}. Mode ${agent.mode}. ${agent.detail.replaceAll("Qwen", "AI Inference")}`
            : "AI Inference runs locally."}
        </p>
      </header>

      <section className="space-y-3">
        <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">On this computer</h2>
        <div className="flex flex-col gap-2 lg:flex-row lg:items-stretch">
          <Box
            kicker="Screen"
            title="Next.js"
            port="127.0.0.1:4317"
            lines={["Overview, route, and charts", "Sends Refuse, Send, Answer here, and Set aside", "Reads /api/state every 2 seconds"]}
          />
          <Arrow label="Forwards /api" />
          <Box
            kicker="Control"
            title="Go Ano"
            port="127.0.0.1:8647"
            lines={["Collects every device and the protocol it sends", "AI Inference reviews each device from the past 24 hours", "Enforces only in active mode"]}
            accent
          />
          <Arrow label="Approved names" />
          <Box
            kicker="Enforcement demo"
            title="Envoy"
            port="127.0.0.1:10080"
            lines={["Refuse answers 403", "Answer here answers 200 locally", "Send and Set aside answer 404"]}
          />
        </div>
        <div className="grid gap-3 lg:grid-cols-[1fr_1fr]">
          <div className="rounded-md border bg-card px-4 py-3">
            <p className="text-xs tracking-wide text-muted-foreground uppercase">Memory</p>
            <p className="mt-1 text-base font-medium">SQLite · data/ano.db</p>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              Devices, one card per device and hostname, the protocol of each send, the approved rule, and the decision log. Go is the only writer.
            </p>
          </div>
          <div className="rounded-md border bg-card px-4 py-3">
            <p className="text-xs tracking-wide text-muted-foreground uppercase">Envoy files</p>
            <p className="mt-1 text-base font-medium">data/envoy.yaml</p>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              Rewritten and the process restarted after Refuse or Answer here. Admin stays on 127.0.0.1:19001. Send and Set aside are left out of the file.
            </p>
          </div>
        </div>
      </section>

      <section className="grid gap-6 lg:grid-cols-2">
        <div>
          <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">When a name first appears</h2>
          <ol className="mt-3 space-y-3">
            <Step n={1} title="One card per hostname">
              Each device keeps its own card for a hostname, with the protocol that device used. A burst of the same lookup does not become a pile of cards.
            </Step>
            <Step n={2} title="The advisor writes a sentence">
              The Go advisor classifies the name (usage reports, voice, ads, or unknown) and suggests drop or allow. An earlier choice for that kind of name wins over the default.
            </Step>
            <Step n={3} title="Nothing is enforced yet">
              The card waits on the screen. Envoy does not learn the suggestion. The route picture still shows that name leaving the house.
            </Step>
          </ol>
        </div>
        <div>
          <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">When you press Refuse</h2>
          <ol className="mt-3 space-y-3">
            <Step n={1} title="The screen posts the choice">
              The browser calls POST /api/cards/…/decide. Go stores the rule as an approved drop and freezes the before counts.
            </Step>
            <Step n={2} title="The log and the chart update">
              A decision row is written. Every two seconds the refused lookups and the bytes kept here increase. The bytes that left stay put.
            </Step>
            <Step n={3} title="Envoy reloads">
              Go writes envoy.yaml from Refuse and Answer here, then restarts Envoy. Checking that hostname returns 403.
            </Step>
          </ol>
        </div>
      </section>

      <section className="max-w-3xl space-y-3">
        <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">The actions</h2>
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[640px] text-left text-sm">
            <thead className="text-xs tracking-wide text-muted-foreground uppercase">
              <tr>
                <th className="px-4 py-2 font-medium">Your choice</th>
                <th className="px-4 py-2 font-medium">Stored rule</th>
                <th className="px-4 py-2 font-medium">Envoy on this machine</th>
                <th className="px-4 py-2 font-medium">Route picture</th>
              </tr>
            </thead>
            <tbody>
              <tr className="border-t border">
                <td className="px-4 py-3 font-medium">Refuse</td>
                <td className="px-4 py-3">approved drop</td>
                <td className="px-4 py-3">403, name is refused</td>
                <td className="px-4 py-3">Red dot stops inside Ano</td>
              </tr>
              <tr className="border-t border">
                <td className="px-4 py-3 font-medium">Send</td>
                <td className="px-4 py-3">approved allow direct</td>
                <td className="px-4 py-3">Left out of the config, so a check returns 404</td>
                <td className="px-4 py-3">Green dot continues to the internet</td>
              </tr>
              <tr className="border-t border">
                <td className="px-4 py-3 font-medium">Answer here</td>
                <td className="px-4 py-3">approved allow envoy</td>
                <td className="px-4 py-3">200 on this machine, or re-routed when Proxy has a target</td>
                <td className="px-4 py-3">Blue dot passes through the Envoy lane</td>
              </tr>
              <tr className="border-t border">
                <td className="px-4 py-3 font-medium">Set aside</td>
                <td className="px-4 py-3">set aside, not approved</td>
                <td className="px-4 py-3">Not loaded, so a check returns 404</td>
                <td className="px-4 py-3">Shown as still leaving, because nothing is enforced</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p className="text-sm leading-6 text-muted-foreground">
          Answer here does not open a connection to Amazon on this proof of concept. The 200 is a local stand-in so you can see that the route was accepted. A real proxy to the vendor is the later inline box.
        </p>
      </section>

      <section className="grid gap-6 lg:grid-cols-[1.2fr_0.8fr]">
        <div className="max-w-3xl">
          <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">What the route picture is standing in for</h2>
          <p className="mt-3 text-sm leading-6">
            The route page draws the home layout this slice is practicing: speaker, Wi-Fi, Ano, home router, modem, internet. On that future box, Ano is in front of the access point, DNS is the first gate, and Refuse is a real packet drop in nftables. Envoy keeps only the names you chose with Answer here.
          </p>
          <p className="mt-3 text-sm leading-6 text-muted-foreground">
            This computer is not that gateway. It does not sit in front of your Wi-Fi, it does not install firewall rules, and it does not store speaker audio or any payload. The advisor here is the Go classifier. A small local model can replace the sentence writer later. It still runs when a card is created, not on each packet.
          </p>
        </div>
        <div>
          <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">Ports</h2>
          <ul className="mt-3 divide-y divide-border rounded-md border text-sm">
            <Port name="Review screen" value="4317" />
            <Port name="Go processor" value="8647" />
            <Port name="Envoy listen" value="10080" />
            <Port name="Envoy admin" value="19001" />
          </ul>
        </div>
      </section>
    </div>
  );
}

function Box({ kicker, title, port, lines, accent }: { kicker: string; title: string; port: string; lines: string[]; accent?: boolean }) {
  return (
    <article className={`min-w-0 flex-1 rounded-xl border px-3 py-2.5 ${accent ? "border-primary/40 bg-card" : "border bg-card"}`}>
      <p className="text-[10px] tracking-wide text-muted-foreground uppercase">{kicker}</p>
      <h3 className="mt-0.5 text-base font-semibold tracking-tight">{title}</h3>
      <p className="font-mono text-xs text-muted-foreground">{port}</p>
      <ul className="mt-3 space-y-1.5 text-sm leading-5">
        {lines.map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ul>
    </article>
  );
}

function Arrow({ label }: { label: string }) {
  return (
    <div className="flex items-center justify-center gap-2 py-1 lg:w-24 lg:flex-col">
      <div className="relative h-px w-16 bg-border lg:h-10 lg:w-px">
        <span className="system-dot" />
      </div>
      <p className="text-center text-[11px] leading-4 text-muted-foreground">{label}</p>
    </div>
  );
}

function Step({ n, title, children }: { n: number; title: string; children: string }) {
  return (
    <li className="flex gap-3">
      <span className="mt-0.5 grid size-6 shrink-0 place-items-center rounded-full border text-xs font-medium">{n}</span>
      <span>
        <span className="block text-sm font-medium">{title}</span>
        <span className="mt-1 block text-sm leading-6 text-muted-foreground">{children}</span>
      </span>
    </li>
  );
}

function Port({ name, value }: { name: string; value: string }) {
  return (
    <li className="flex items-baseline justify-between gap-3 px-4 py-2.5">
      <span>{name}</span>
      <span className="font-mono text-xs text-muted-foreground">{value}</span>
    </li>
  );
}
