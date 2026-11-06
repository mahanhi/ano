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
          Ano collects and asks AI Inference. Passive watches. Active enforces. Your button wins. The model stays off the packet path.
        </p>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          {agent
            ? `AI Inference. ${agent.model.ready ? "Ready" : "Not ready"}. Mode ${agent.mode}. ${agent.detail.replaceAll("Qwen", "AI Inference")}`
            : "AI Inference runs locally."}
        </p>
      </header>

      <section className="space-y-3">
        <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">On Ano</h2>
        <div className="flex flex-col gap-2 lg:flex-row lg:items-stretch">
          <Box
            kicker="Screen"
            title="Next.js"
            port="0.0.0.0:80"
            lines={["Overview, route, and charts", "Sends Refuse, Send, Answer here, and Set aside", "Reads /api/state every 2 seconds"]}
          />
          <Arrow label="Forwards /api" />
          <Box
            kicker="Control"
            title="Go Ano"
            port=""
            lines={["Collects every device and the protocol it sends", "AI Inference reviews each device from the past 24 hours", "Enforces only in active mode"]}
            accent
          />
          <Arrow label="Approved names" />
          <Box
            kicker="The way out"
            title="Envoy"
            port="0.0.0.0:10082"
            lines={["Carries TCP and UDP, including HTTPS and QUIC", "Drops a refused endpoint for every protocol", "Ano stores the record afterwards"]}
          />
        </div>
        <div className="grid gap-3 lg:grid-cols-[1fr_1fr]">
          <div className="rounded-md border bg-card px-4 py-3">
            <p className="text-xs tracking-wide text-muted-foreground uppercase">Memory</p>
            <p className="mt-1 text-base font-medium">SQLite · data/ano.db</p>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              Devices, names, protocols, rules, and the decision log.
            </p>
          </div>
          <div className="rounded-md border bg-card px-4 py-3">
            <p className="text-xs tracking-wide text-muted-foreground uppercase">Envoy files</p>
            <p className="mt-1 text-base font-medium">data/envoy.yaml</p>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              Rewritten after Refuse or Answer here. Send and Set aside stay out.
            </p>
          </div>
        </div>
      </section>

      <section className="grid gap-6 lg:grid-cols-2">
        <div>
          <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">When a name first appears</h2>
          <ol className="mt-3 space-y-3">
            <Step n={1} title="One card per hostname">
              One card per device and name, with the protocol. Repeats stay one card.
            </Step>
            <Step n={2} title="Ano writes a sentence">
              Ano classifies the name and suggests drop or allow. An earlier choice wins.
            </Step>
            <Step n={3} title="Nothing is enforced yet">
              Envoy does not learn the suggestion. The route still shows it leaving.
            </Step>
          </ol>
        </div>
        <div>
          <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">When you press Refuse</h2>
          <ol className="mt-3 space-y-3">
            <Step n={1} title="The choice is stored">
              Refuse becomes an approved drop. The before counts freeze.
            </Step>
            <Step n={2} title="The log and the chart update">
              Refused lookups and bytes kept here increase. Bytes that left stay put.
            </Step>
            <Step n={3} title="Envoy reloads">
              That name returns 403.
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
                <th className="px-4 py-2 font-medium">On Ano</th>
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
                <td className="px-4 py-3">200 on Ano, or the Proxy target</td>
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
          Answer here is a local 200, not a connection to the vendor.
        </p>
      </section>

      <section className="grid gap-6 lg:grid-cols-[1.2fr_0.8fr]">
        <div className="max-w-3xl">
          <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">Route picture</h2>
          <p className="mt-3 text-sm leading-6">
            Envoy carries TCP and UDP, including HTTPS and QUIC. A refused name gets no address. A refused address is dropped. DNS stays on Ano.
          </p>
          <p className="mt-3 text-sm leading-6 text-muted-foreground">
            Start stays off until the second Ethernet jack is connected. The model runs on the schedule, not on each packet.
          </p>
        </div>
        <div>
          <h2 className="text-sm font-medium tracking-wide text-muted-foreground uppercase">Ports</h2>
          <ul className="mt-3 divide-y divide-border rounded-md border text-sm">
            <Port name="Review screen" value="80" />
            <Port name="Go processor" value="8647" />
            <Port name="Envoy TCP" value="10082" />
            <Port name="Envoy UDP" value="10083" />
            <Port name="Envoy name check" value="10080" />
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
      {port && !port.startsWith("127.0.0.1:") ? <p className="font-mono text-xs text-muted-foreground">{port}</p> : null}
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
