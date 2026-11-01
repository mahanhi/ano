# Ano requirements

Ano is a home privacy appliance. It sits in front of the path out of the house, explains speaker and other device names in household language, and enforces only the choices a person makes. This file is the requirement for the proof of concept that already runs in this workspace. Use it to rebuild and run the same app on a Mac.

The longer product plan, including the later Raspberry Pi, is in [Ano.md](Ano.md). This proof of concept does not use a Pi. It runs on the computer in front of you.

## Run it on a Mac

The cloud session cannot write to `/Users/maha/Development/ano`. On the Mac:

```bash
mkdir -p /Users/maha/Development/ano
```

Copy this project into that folder, then:

```bash
cd /Users/maha/Development/ano
brew install go node envoy
go test ./internal/... ./cmd/...
```

Terminal 1:

```bash
cd /Users/maha/Development/ano
HBAR_ENVOY_BIN=$(which envoy) go run ./cmd/hbar
```

Terminal 2:

```bash
cd /Users/maha/Development/ano/web
npm install
npm run dev
```

Open http://127.0.0.1:4317.

On Linux, install Envoy with `bash scripts/install-envoy.sh` and run `go run ./cmd/hbar`. The binary is expected at `bin/envoy` or in `HBAR_ENVOY_BIN`.

## What this slice must do

- Show speaker traffic before anyone decides, then show what changed after a choice.
- Update the charts from live counters every 2 seconds.
- Let a person enforce one of four actions. A suggestion never changes routing by itself.
- Enforce with Envoy on this machine. A drop is HTTP 403. A name sent out never enters Envoy. Answer here is either a local HTTP 200 or a real re-route.
- Draw the path Speaker, Wi-Fi, Ano, router, modem, internet. Dropped names stop at Ano. Other names continue. The picture fills the width of the screen and stacks on a narrow one.
- Group every identified device under one Devices menu. Opening a device shows its names, the action buttons, and the charts.
- Provide a Proxy screen that maps any identified name to an upstream hostname.

Ano collects every device into SQLite. Passive mode is the default and does not enforce. After counts are stored, Ano asks AI Inference for drop or allow on each name. Active mode enforces those choices. A person's button overrides AI Inference. AI Inference does not sit on the packet path.

## Stack

| Part | Choice |
| --- | --- |
| Screen | Next.js 15, TypeScript, Tailwind, shadcn/ui, Recharts |
| Control | Go |
| Store | SQLite at `data/ano.db` (`modernc.org/sqlite`) |
| Enforcement | Envoy static config written to `data/envoy.yaml` |

Ports:

| Service | Port |
| --- | --- |
| Review screen | 4317, bound on all interfaces |
| Go API | 8647 |
| Envoy listen | 10080 |
| Envoy admin | 19001, localhost only |

The Next.js dev server rewrites `/api/:path*` to `http://127.0.0.1:8647/api/:path*`. The first HTML response of the dashboard must already contain the overview, not only the words “Loading the dashboard…”. The dev server must accept both `127.0.0.1` and `localhost` (`::1`).

## Seeded home

Devices:

| Id | Name | Kind | Room |
| --- | --- | --- | --- |
| `dev-kitchen-speaker` | Kitchen speaker | speaker | Kitchen |
| `dev-hall-phone` | Hall phone | phone | Hall |

Names, one card each. Nothing is enforced until a person decides.

| Id | Device | Destination | Kind of traffic | Suggestion |
| --- | --- | --- | --- | --- |
| `pat-metrics-us` | Kitchen speaker | `device-metrics-us.amazon.com` | usage reports | drop |
| `pat-metrics-us-2` | Kitchen speaker | `device-metrics-us-2.amazon.com` | usage reports | drop |
| `pat-voice` | Kitchen speaker | `avs-alexa-na.amazon.com` | voice answers | allow direct |
| `pat-phone-ads` | Hall phone | `app-measurement.com` | ad measurement | drop |

Display names on the screen: Metrics US, Metrics US 2, Voice answers, Ad measurement.

Speakers only is on by default. Overview, Route, and the speaker charts follow that filter. Devices still lists the phone. Proxy lists every name even while Speakers only is on.

## Screens

Left menu, attached to the content, full height. The main pane scrolls. The menu does not. Wide screens show labels. Narrow screens keep the same menu as icons.

Items: Overview, Route, System, Devices, Decisions, Envoy, Proxy. A Speakers only checkbox sits at the bottom of the menu.

The visual style is a light analytics dashboard: cool gray canvas, white cards, a row of metric cards, then charts. Orange is bytes that left the house. Green is bytes kept after a drop. Red is a refused name. Blue is a name answered through Envoy.

### Overview

- Four cards: bytes left, bytes kept, lookups refused, Envoy running or down.
- A line chart of bytes left and bytes kept. The line is drawn immediately. Live updates must stay visible, so chart animation is off.
- Two share charts: bytes that left, by name, and bytes kept, by name.
- A table of names. Each row opens that name. Status is Dropped, Allowed, or Needs a decision.
- Latest decisions beside the table.
- A compact path picture, with a link to the Route page.

### Route

The path is Speaker (Kitchen), Wi-Fi, Ano, Router, Modem, Internet. Inside Ano the lanes are Drop, Out, and Envoy. Moving dots follow the connectors and use the width of the panel. Below about 820 pixels of panel width the path stacks in a column and each node uses the full width.

Red stops inside Ano. Orange is still undecided and leaves. Green was sent out. Blue goes through Envoy.

### System

Show the three programs on this computer: Next.js on 4317, Go on 8647, Envoy on 10080, plus SQLite `data/ano.db` and `data/envoy.yaml`. Explain that this machine is not the house gateway and does not store payloads.

The action table must use the same names as the device buttons:

| Action | Stored rule | Envoy on this machine | Route picture |
| --- | --- | --- | --- |
| Refuse the name | approved drop | 403 | Red dot stops inside Ano |
| Send it out | approved allow direct | Left out of the config, so a check returns 404 | Green dot continues |
| Answer here | approved allow envoy | 200 on this machine, or re-routed when Proxy has a target | Blue dot through the Envoy lane |
| Set aside | set aside, not approved | Not loaded, so a check returns 404 | Still leaving, because nothing is enforced |

Answer here with no Proxy target does not call the vendor. The 200 is a local stand-in.

### Devices

Group devices by kind: Speakers, TVs, Thermostats, Phones. A device with no names says so. Opening a device shows each name, the four action buttons, and the charts for that device.

The buttons are one line high. The action in force is filled, and a label in the row says Now Dropped, Now Going direct, Now Through Envoy, Now Set aside, or Now Not enforced.

| Button | Meaning |
| --- | --- |
| Refuse the name | Drop it. This name never reaches the router. |
| Send it out | Let it go direct. It skips Ano and Envoy. |
| Answer here | This machine answers, or Proxy re-routes it. It does not call the vendor unless a target is mapped. |
| Set aside | Not enforced. The route still leaves. |

Opening a name shows its own sentence, before and after counts, byte chart, lookup chart, and the same buttons.

### Decisions

Newest first. Each row has when (shown in America/Los_Angeles), device, name, outcome, and what changed.

### Envoy

Show whether the process is running, the listen address, and the number of routes. List approved names. A form checks a hostname through Envoy on 127.0.0.1:10080 and shows the HTTP status and body. The check times out after 4 seconds.

### Proxy

For every identified name, show `original host → Envoy → target`.

- Map it saves the hostname, sets the name to Answer here, and reloads Envoy. Envoy rewrites the host and connects to that hostname on port 443 with TLS.
- Clear removes the target. The name stays Answer here and Envoy returns the local 200.
- A target must be a hostname. An empty clear is allowed. A bad string is rejected.

## Behavior

- One card per device and hostname. Repeating the same lookup does not create another card.
- Unknown speaker name: suggest drop, do not enforce.
- Voice names such as `avs-alexa-na.amazon.com` suggest allow direct, because dropping them stops spoken answers.
- Metrics and counts tick every 2 seconds for speaker names. Before a drop, lookups and bytes count as left the house. After Refuse the name, those before counts freeze and the after counts grow as refused.
- Chart samples are stored, capped near 400 points. If the database is empty, backfill 24 points so the lines are visible immediately.
- Only approved Refuse the name and Answer here routes are written into Envoy. Send it out, Set aside, and suggestions are absent.
- Changing an action away from Answer here clears a Proxy target. Set aside clears it too.
- Go rewrites `data/envoy.yaml` and restarts Envoy after every decision and every Proxy save.

Envoy answers:

| Case | Status | `x-hbar-action` |
| --- | --- | --- |
| Refuse the name | 403 | `drop` |
| Answer here, no target | 200, local body | `allow_envoy` |
| Answer here, with a target | proxied to that host, port 443 | `allow_envoy`, plus `x-hbar-reroute` |
| Anything else | 404 | `unmatched` |

## HTTP API

All JSON.

| Method | Path | Body | Result |
| --- | --- | --- | --- |
| GET | `/api/health` | | `{ "ok": "true", "product": "Ano" }` |
| GET | `/api/state` | | settings, cards, devices, decisions, series, envoy. `?all=1` returns every card even when Speakers only is on. |
| PUT | `/api/settings` | `{ "speakerOnly": true }` | saves the filter |
| POST | `/api/cards/{id}/decide` | `{ "action": "drop" \| "allow_direct" \| "allow_envoy" \| "ignore" }` | stores the rule and reloads Envoy |
| POST | `/api/cards/{id}/reroute` | `{ "host": "logs.example.net" }` | maps Answer here to that host. `""` clears the target. |
| POST | `/api/probe` | `{ "host": "device-metrics-us.amazon.com" }` | `{ status, action, body }` from Envoy |
| POST | `/api/simulate` | `{ "deviceId", "host" }` | records a lookup or opens one suggested card |

`/api/state` cards include `reroute` (the upstream host, or empty).

## Code layout

```text
cmd/hbar/main.go           HTTP API and the 2 second ticker
internal/store/            SQLite devices, cards, rules, decisions, samples
internal/model/            classifier and card sentences
internal/envoycfg/         Envoy YAML
internal/envoyrun/         write the file and restart Envoy
web/                       Next.js screen
web/src/components/home.tsx
web/src/components/route-map.tsx
web/src/components/system-design.tsx
web/src/components/proxy-map.tsx
web/src/components/charts.tsx
data/ano.db               created on first run
data/envoy.yaml            rewritten after each decision
```

## Tests

```bash
go test ./internal/... ./cmd/...
```

Cover at least:

- Speaker cards stay suggested until a person decides.
- Refuse the name freezes the before counts and grows the after counts.
- Set aside removes the name from Envoy.
- A Proxy target puts any name on Answer here and stores the hostname. Clearing the target leaves Answer here with an empty target.
- Envoy config for Answer here with no target is a local 200. With a target it is a cluster, `host_rewrite_literal`, and TLS on port 443. Send it out never appears in the file.

## Out of scope for this slice

- Raspberry Pi, nftables, and whole-house DNS. Those stay in Ano.md.
- A local LLM. Phase 2 replaces the sentence writer only. The rules stay.
- Storing audio, payloads, or credentials.
- Calling the vendor unless a person mapped a Proxy target.
