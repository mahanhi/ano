# Ano requirements

Ano is a home privacy appliance. It records where each device sends, explains those names in household language, and enforces Refuse and Send when a person asks or when Active mode is on. This file is the requirement for the app in this repository. The household behavior is in [SYSTEM-DESIGN.md](SYSTEM-DESIGN.md). The programs and the packet path are in [ARCHITECTURE.md](ARCHITECTURE.md).

## Run it

On a Mac:

```bash
brew install go node envoy
go test ./internal/... ./cmd/...
```

Terminal 1:

```bash
ANO_ENVOY_BIN=$(which envoy) go run ./cmd/ano
```

Terminal 2:

```bash
cd web
npm install
npm run dev
```

Open http://127.0.0.1:4317.

On the Orange Pi, install with `deploy/orangepi/install.sh` after the arm64 binary is built. The screen is served by `next start` on port 80. Ano, the screen, and Ollama are enabled at boot, restart if they stop, and do not give up after repeated failures. The installer leaves the gateway off and leaves dnsmasq disabled. The Mac development server stays on port 4317.

The Mac is a development machine. Starting the gateway there is refused, and IP forwarding stays off. The gateway can start on Ano when two Ethernet ports are on different subnets.

## What Ano must do

- Show device traffic before anyone decides, then show what changed after a choice.
- Update the charts from live counters every 2 seconds.
- Default to Passive mode. Store each AI Inference suggestion. Leave routing unchanged.
- In Active mode, enforce the model's Refuse and Send. A person's button overrides the model.
- Keep the model off the packet path. The model runs on the review schedule and when Ask Ano is used.
- Enforce a person's Refuse. On this computer, Envoy answers HTTP 403 for a name check. On Ano with the gateway on, DNS answers NXDOMAIN for an all-port refuse and the kernel drops that address.
- Leave Send out of the Envoy name-check routes. Answer here is a local HTTP 200, or a real re-route when Proxy has a target.
- Draw the path Device, Wi-Fi, Ano, router, modem. A drop stops at Ano. The picture fills the width of the screen and stacks on a narrow one.
- Group devices under Devices. Opening a device shows its names, the action buttons, and the charts.
- Provide Admin for the review schedule and the DNS drop list.
- Provide Ask Ano on every screen, by text or by the browser's voice input.
- Provide a Proxy screen that maps an identified name to an upstream hostname.
- Record forwarded TCP and UDP, including HTTPS and QUIC, after Envoy finishes a connection. Streaming stays forwarded.
- Leave the gateway off until Ano has a router-side port and an access-point port on a different subnet.

## Stack

| Part | Choice |
| --- | --- |
| Screen | Next.js 15, TypeScript, Tailwind, shadcn/ui, Recharts |
| Control | Go |
| Store | SQLite (`modernc.org/sqlite`), WAL, one writer |
| Name check and egress | Envoy static config |
| Home forward path | nftables and dnsmasq on Ano, loaded only while the gateway is on |
| Model | Ollama on localhost. Mac default `qwen2.5:0.5b`. Ano default `llama3.2:1b`. |

| Service | Port |
| --- | --- |
| Review screen on Ano | 80, bound on all interfaces |
| Review screen on the Mac, during development | 4317, bound on all interfaces |
| Go API | 8647 |
| Envoy name check | 10080 |
| Envoy TCP egress | 10082 |
| Envoy UDP egress | 10083 |
| Envoy admin | 19001, localhost only |
| Ollama | 11434, localhost only |

The screen rewrites `/api/:path*` to `http://127.0.0.1:8647/api/:path*`. On the Pi, the screen name is `ano.home.security.ai`. Ano records it in the Pi's `/etc/hosts` and answers it from Ano's DNS when the gateway is on. Development machines keep `http://127.0.0.1:4317` and do not receive that hosts entry.

Environment:

| Variable | Meaning |
| --- | --- |
| `ANO_DATA` | Database directory. `data` on the Mac, `/var/lib/ano` on Ano |
| `ANO_ENVOY_BIN` | Envoy binary |
| `ANO_LLM_KIND` | `ollama` by default |
| `ANO_LLM_URL` | OpenAI-compatible base URL. Default `http://127.0.0.1:11434/v1` |
| `ANO_LLM_MODEL` | Model name |

## Seeded home

Devices:

| Id | Name | Kind | Room |
| --- | --- | --- | --- |
| `dev-kitchen-speaker` | Kitchen speaker | speaker | Kitchen |
| `dev-hall-phone` | Hall phone | phone | Hall |

Names, one card each. Nothing is enforced until a person decides or Active mode applies a stored suggestion.

| Id | Device | Destination | Kind of traffic | Suggestion |
| --- | --- | --- | --- | --- |
| `pat-metrics-us` | Kitchen speaker | `device-metrics-us.amazon.com` | usage reports | drop |
| `pat-metrics-us-2` | Kitchen speaker | `device-metrics-us-2.amazon.com` | usage reports | drop |
| `pat-voice` | Kitchen speaker | `avs-alexa-na.amazon.com` | voice answers | allow direct |
| `pat-phone-ads` | Hall phone | `app-measurement.com` | ad measurement | drop |

Display names: Metrics US, Metrics US 2, Voice answers, Ad measurement.

Speakers only is on by default. Overview, Route, and the speaker charts follow that filter. Devices still lists the phone. Proxy lists every name even while Speakers only is on.

## Screens

Left menu, attached to the content, full height. The main pane scrolls. The menu does not. Wide screens show labels. Narrow screens keep the same menu as icons.

Items: Overview, Route, System, Devices, Admin, Decisions, Envoy, Proxy. Passive and Active, and a Speakers only checkbox, sit in the menu. Ask Ano floats on every screen.

The visual style is a light analytics dashboard: cool gray canvas, white cards, a row of metric cards, then charts. Orange is bytes that left the house. Green is bytes kept after a drop. Red is a refused name. Blue is a name answered through Envoy.

### Overview

- Four cards: bytes left, bytes kept, lookups refused, Envoy running or down.
- A line chart of bytes left and bytes kept. Chart animation is off.
- Two share charts: bytes that left, by name, and bytes kept, by name.
- A table of names. Each row opens that name. Status is Dropped, Allowed, or Needs a decision.
- Latest decisions beside the table.
- A compact path picture, with a link to the Route page.
- Passive and Active state, and whether AI Inference is ready.

Empty device and decision lists render as empty lists. The Devices screen says "No devices yet."

### Route

The path is Device, Wi-Fi, Ano, router, modem. Inside Ano the lanes are Drop, Out, and Envoy. Below about 820 pixels of panel width the path stacks in a column.

Red stops inside Ano. Orange is still undecided and leaves. Green was sent out. Blue goes through Envoy.

### System

Show the programs on this machine: the screen on 80, Ano on 8647, Envoy on 10080, SQLite, and the Envoy config. State that payloads are not stored. On the Mac, state that the gateway starts on Ano. On Ano, show the router-side port and the access-point port, and keep Start disabled until both exist.

| Action | Stored rule | Name check | With the gateway on | Route picture |
| --- | --- | --- | --- | --- |
| Refuse the name | approved drop | 403 | NXDOMAIN, and the address is dropped | Red dot stops inside Ano |
| Send it out | approved allow direct | Left out of the name-check config | Forwarded | Green dot continues |
| Answer here | approved allow envoy | 200, or re-routed when Proxy has a target | Carried by Envoy | Blue dot through the Envoy lane |
| Set aside | set aside, not approved | Not loaded | Forwarded | Still leaving |

Answer here with no Proxy target does not call the vendor. The 200 is a local stand-in.

### Devices

Group devices by kind: Speakers, TVs, Thermostats, Phones. A device with no names says so. Opening a device shows each name, the four action buttons, and the charts for that device.

The action in force is filled. The row says Now Dropped, Now Going direct, Now Through Envoy, Now Set aside, or Now Not enforced.

| Button | Meaning |
| --- | --- |
| Refuse the name | Drop it. |
| Send it out | Let it go direct. |
| Answer here | Ano answers, or Proxy re-routes it. |
| Set aside | Not enforced. |

Opening a name shows its sentence, before and after counts, byte chart, lookup chart, and the same buttons.

### Admin

Schedule and the drop list.

The schedule is on or off, every N, and a unit of minute, hour, day, week, or month. Limits are 1–59 minutes, 1–24 hours, 1–31 days, 1–52 weeks, and 1–12 months. Day, week, and month take a clock time. Hour takes the minute of the hour. Week takes a weekday, Sunday as 0. Month takes a day from 1 to 28. The default is on, every 1 day, at 03:00. Save stores it. Review now runs the past 24 hours immediately. The screen polls Admin and must not reset unsaved fields on each poll.

The drop list takes a DNS name and either all ports or one port. All ports refuses the name. One port drops only that port. The list shows admin rows and Refuse rows. An admin row can be removed. A Refuse row is changed from the device.

### Decisions

Newest first. Each row has when (shown in America/Los_Angeles), device, name, outcome, and what changed.

### Envoy

Show whether the process is running, the listen address, and the number of routes. List approved names. A form checks a hostname through Envoy on 127.0.0.1:10080 and shows the HTTP status and body. The check times out after 4 seconds.

### Proxy

For every identified name, show `original host → Envoy → target`.

- Map it saves the hostname, sets the name to Answer here, and reloads Envoy. Envoy rewrites the host and connects to that hostname on port 443 with TLS.
- Clear removes the target. The name stays Answer here and Envoy returns the local 200.
- A target must be a hostname. An empty clear is allowed. A bad string is rejected.

### Ask Ano

A floating panel on every screen. The person types or uses the browser speech API. Spoken replies use browser speech synthesis. A browser without speech input tells the person to type.

`POST /api/chat` takes `{ "text" }` of at most 500 characters and returns `{ "say", "changed" }`.

The local model writes the reply. Ano applies a change only when the request already names that change:

| Request | Result |
| --- | --- |
| Greeting or general question | The model answers. Nothing changes. |
| Incomplete change | One question, with the choices that fit. Nothing changes. |
| Passive or active | Mode changes. |
| A stated cadence, such as every hour | The schedule saves that cadence. |
| Review now | The past 24 hours are reviewed. |
| Drop or block, plus a DNS name | The name is listed. A stated port is used. Otherwise all ports. |
| Remove or undrop, plus a listed admin name | That admin row is removed. |
| Refuse, send, answer, or aside, plus a name on a device | That device choice is saved. |
| Speakers on or off | Speakers only changes. |
| Gateway on or off | The gateway is asked to start or stop. Start still fails until two subnets exist. |

A schedule confirmation is built from the saved values. A model reply that omits the unit does not rewrite the current schedule.

## Behavior

- One card per device and hostname. Repeating the same lookup does not create another card.
- Unknown speaker name: suggest drop, and do not enforce it in Passive mode.
- Voice names such as `avs-alexa-na.amazon.com` suggest allow direct, because dropping them stops spoken answers.
- Metrics and counts tick every 2 seconds for speaker names. Before a drop, lookups and bytes count as left the house. After Refuse the name, those before counts freeze and the after counts grow as refused.
- Chart samples are stored, capped near 400 points. If the database is empty, backfill 24 points so the lines are visible immediately.
- The review loop checks the schedule once a minute. It sends observed names from the past 24 hours to the model. An empty set marks the review and does not call the model. A failed review waits 15 minutes before the next try.
- Only approved Refuse the name and Answer here routes are written into the Envoy name-check listener. Send it out, Set aside, and suggestions are absent.
- Changing an action away from Answer here clears a Proxy target. Set aside clears it too.
- Ano rewrites the Envoy config and reloads Envoy after a decision, a Proxy save, a mode change, and a drop-list change. When the gateway is on, the same reload refreshes nftables and dnsmasq.
- Customer-chosen rows are kept when the model records a verdict.
- Streaming and already-seen forwarded flows cannot be refused from the device decision buttons.

Envoy name check:

| Case | Status | `x-ano-action` |
| --- | --- | --- |
| Refuse the name | 403 | `drop` |
| Answer here, no target | 200, local body | `allow_envoy` |
| Answer here, with a target | proxied to that host, port 443 | `allow_envoy`, plus `x-ano-reroute` |
| Anything else | 404 | `unmatched` |

Gateway, only while it is on:

- The access-point port is the DHCP gateway and the only DNS server.
- An all-port refuse or an all-port admin name is NXDOMAIN, and its addresses are dropped for every protocol.
- An admin name with one port is dropped on that port. Its name still resolves.
- TCP and UDP from the home port to a public address are redirected to Envoy on 10082 and 10083.
- DNS and DNS-over-TLS to anywhere except the home network are dropped.
- Other forwarded LAN-to-WAN traffic is dropped. ICMP to the WAN is accepted. Established replies are accepted.
- Envoy records the finished connection. Ano writes that metadata afterward.

## HTTP API

All JSON.

| Method | Path | Body | Result |
| --- | --- | --- | --- |
| GET | `/api/health` | | `{ "ok": "true", "product": "Ano" }` |
| GET | `/api/state` | | settings, cards, devices, decisions, series, envoy. `?all=1` returns every card even when Speakers only is on. Devices, cards, and decisions are arrays. |
| PUT | `/api/settings` | `{ "speakerOnly": true }` | saves the filter |
| GET | `/api/admin` | | schedule, drop list, and gateway fields |
| POST | `/api/admin/review-schedule` | `{ "on", "every", "unit", "hour", "minute", "weekday", "monthDay" }` | saves the schedule |
| POST | `/api/admin/review` | | runs the review now |
| GET | `/api/admin/blocks` | | drop names, including Refuse rows |
| POST | `/api/admin/blocks` | `{ "name", "port" }` | adds an admin drop. Port 0 is all ports. |
| DELETE | `/api/admin/blocks/{id}` | | removes an admin drop |
| POST | `/api/chat` | `{ "text" }` | `{ "say", "changed" }` |
| POST | `/api/cards/{id}/decide` | `{ "action": "drop" \| "allow_direct" \| "allow_envoy" \| "ignore" }` | stores the rule and reloads enforcement |
| POST | `/api/cards/{id}/reroute` | `{ "host": "logs.example.net" }` | maps Answer here to that host. `""` clears the target. |
| POST | `/api/probe` | `{ "host": "device-metrics-us.amazon.com" }` | `{ status, action, body }` from Envoy |
| GET | `/api/gate` | | gateway status. `canStart` is false until two subnets exist. |
| POST | `/api/gate` | `{ "on": true }` | starts or stops the gateway. The Mac refuses start. |
| POST | `/api/simulate` | `{ "deviceId", "host" }` | records a lookup or opens one suggested card |

`/api/state` cards include `reroute` (the upstream host, or empty). Errors are `{ "error" }`.

## Code layout

```text
cmd/ano/main.go                 HTTP API, review loop, gateway, Envoy
internal/agent/                 schedule and the model review
internal/chat/                  Ask Ano
internal/llm/                   Ollama client
internal/store/                 SQLite devices, cards, rules, decisions, samples, blocks
internal/model/                 classifier and card sentences
internal/envoycfg/              Envoy YAML
internal/envoyrun/              write the file and restart Envoy
internal/gate/                  nftables, dnsmasq, and forwarded-flow notes
internal/via/                   optional proxy for an added device; the Mac redirect stays off
web/                            Next.js screen
deploy/orangepi/                installer and systemd units
data/ano.db                     created on first run
data/envoy.yaml                 rewritten after each enforcement change
```

## Tests

```bash
go test ./internal/... ./cmd/...
```

Cover at least:

- Speaker cards stay suggested until a person decides.
- Refuse the name freezes the before counts and grows the after counts.
- Set aside removes the name from the Envoy name check.
- A Proxy target puts a name on Answer here and stores the hostname. Clearing the target leaves Answer here with an empty target.
- Envoy config for Answer here with no target is a local 200. With a target it is a cluster, `host_rewrite_literal`, and TLS on port 443. Send it out never appears in that file.
- UDP egress has a UDP proxy and no original-destination listener filter. TCP egress keeps original destination.
- The review is due once for a day, an hour, a week, and a month, and an invalid clock time is not due.
- The default schedule is every 1 day at 03:00. An hour of 24 is rejected.
- Ask Ano applies "every hour" even when the model omits the unit, asks a question for "update the schedule", and answers "hello" without a change.
- The Mac gateway start returns an error and loads no rules.
- The rendered nftables rules drop a refused address, redirect TCP to 10082 and UDP to 10083, and contain no `route-to`.

## Out of scope

- Putting the model on the packet path.
- Using the Orange Pi NPU.
- Starting the gateway on the Mac, or on Ano while only one subnet is connected.
- Loading a packet redirect that sends traffic through this Mac.
- Storing audio, payloads, or credentials.
- Calling the vendor unless a person mapped a Proxy target.
- Inventing device traffic for the screen.
