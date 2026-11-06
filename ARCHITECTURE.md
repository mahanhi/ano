# Ano architecture

Ano is one Go process, a Next.js screen, a local model, and Envoy. On the Orange Pi, nftables and dnsmasq join that set while the gateway is on. This document is the structure of those parts. Household behavior is in [SYSTEM-DESIGN.md](SYSTEM-DESIGN.md). The checklist is in [REQUIREMENTS.md](REQUIREMENTS.md).

## Processes

| Process | Role | Lifetime |
| --- | --- | --- |
| `ano` | API, store, review loop, Envoy supervisor, gateway | systemd `ano.service` on Ano. `go run` on the Mac. Restarts if it stops. |
| `ano-web` | The screen. Proxies `/api` to Ano on 127.0.0.1:8647. On the Pi, `next start` listens on port 80 and `/etc/hosts` maps `ano.home.security.ai` to the Pi. The Mac development server uses port 4317 and does not publish that name. | systemd `ano-web.service`, or `npm run dev` on the Mac |
| `ollama` | Local model on 127.0.0.1:11434 | systemd on Ano. The Mac runs it separately. |
| `envoy` | Name check, and TCP/UDP egress when the gateway is on | Child of Ano. A new decision rewrites `envoy.yaml` and reloads it. |
| `dnsmasq` | DHCP and DNS for the access-point port | Enabled only while the gateway is on |

SQLite is the only shared store. Ano is the only writer. The file is `data/ano.db` on the Mac and `/var/lib/ano/ano.db` on Ano. The database uses WAL and one writer lock.

```text
browser  -->  screen :80  -->  Ano :8647  -->  SQLite
                                  |    \
                                  |     -->  Ollama :11434
                                  -->  Envoy :10080, :10082, :10083
                                  -->  nftables and dnsmasq, when the gateway is on
```

## Two paths

Control and packets are separate.

The control path is the screen, Ask Ano, the schedule, and the model. It reads and writes SQLite. When a saved choice must be enforced, Ano reloads Envoy and, if the gateway is on, refreshes nftables and dnsmasq. The model returns text. It never receives a packet and never writes a firewall rule itself.

The packet path exists when the gateway is on. A device packet hits nftables on the access-point port. A refused address is dropped there. Allowed TCP and UDP are redirected to Envoy, which connects onward and appends a line to the access log. Ano reads that log after the connection and writes metadata. The packet does not wait for SQLite or for the model.

On the Mac, and on Ano before the gateway starts, the packet path is idle. The name-check listener on port 10080 still answers so the Envoy screen can show 403, 200, or 404.

## Components

```text
cmd/ano                 HTTP API and process wiring
internal/agent          2 second collect tick, 1 minute schedule check, model review
internal/chat           Ask Ano: model reply, then an allowlisted action
internal/llm            Ollama chat for a verdict and for Ask Ano
internal/store          devices, cards, rules, decisions, samples, blocks, schedule
internal/model          name classifier and the card sentences
internal/envoycfg       Envoy YAML
internal/envoyrun       find Envoy, write the file, reload
internal/gate           plan the two ports, render nftables and dnsmasq, follow the access log
internal/via            HTTP proxy on 10081 for an added device
web                     Next.js screens
deploy/orangepi         installer, ano.service, ano-web.service, ollama drop-in
```

`internal/via` also contains the older Mac redirect. That path stays off. Loading it restarted the Mac, so `EnableSteer` and `RunSteer` return that error and install no rules.

## Screen and API

The screen is one Next.js page. The menu switches the view in the browser. There is no separate route per screen. The page loads `/api/state` and renders Overview, Route, System, Devices, Admin, Decisions, Envoy, or Proxy. Ask Ano posts to `/api/chat` and, when `changed` is true, reloads the page state.

The screen treats a missing device, card, or decision list as an empty list.

Ano listens on `:8647`. The handlers are listed in [REQUIREMENTS.md](REQUIREMENTS.md). A handler that changes enforcement calls the same reload: Envoy first, then the gateway refresh. Gateway refresh does nothing while the gateway is off.

## Model

`ANO_LLM_KIND`, `ANO_LLM_URL`, and `ANO_LLM_MODEL` select the model. The default kind is `ollama`, the default URL is `http://127.0.0.1:11434/v1`, and the default model is `qwen2.5:0.5b`. The Ano unit sets `llama3.2:1b`.

A verdict asks for JSON with `action` of `drop` or `allow`, plus reason, cadence, send time, similar traffic, and protocol. Allow is stored as `allow_direct`, which the screen calls Send. The call times out at 90 seconds.

Ask Ano uses a different instruction and the same client. The model returns JSON with an action and a sentence. Ano then decides whether that action is complete:

- The person's sentence already names a known change. That change is applied. A cadence in the sentence, such as "every hour", fills the schedule fields the model left out.
- The sentence is a greeting, a question, or an incomplete change. Ano returns the model's sentence, or one question with the matching choices. The store is unchanged.
- A schedule confirmation is formatted from the saved row.

The ready check is `GET {ANO_LLM_URL}/models`. The sidebar uses that check. Ask Ano calls the model directly.

## Review loop

`agent.Loop` has two ticks.

- Every 2 seconds it collects counters and observations.
- Every minute it loads the schedule. When the cadence is due, it reviews names observed in the past 24 hours.

The schedule row is `review_enabled`, `review_every`, `review_unit`, `review_hour`, `review_minute`, `review_weekday`, `review_monthday`, `review_last`, and `review_detail`. Units are `minute`, `hour`, `day`, `week`, and `month`. The default row is enabled, every 1 day, at 03:00.

`Due` runs a review once for each matching slot. A minute cadence matches every N minutes. An hour cadence matches every N hours at the saved minute. A day cadence matches the clock time every N days. A week cadence matches the weekday and clock time. A month cadence matches the day of the month and the clock time.

Each reviewed name is sent to the model with that device's own counts. `RecordVerdict` skips a row the person already decided. In Passive mode the verdict is stored as a suggestion. In Active mode Refuse and Send are enforced, and the reload runs once after the batch. An empty observed set marks the review and skips the model. A failure sets the next try 15 minutes later.

## Store

One card is one device plus one destination hostname. Repeating a lookup updates that card.

A rule is one of `drop`, `allow_direct`, `allow_envoy`, or `ignore`. Status distinguishes a suggestion from an approved choice. `source=customer` marks a person's button. A Proxy target is `reroute_host` on an `allow_envoy` rule. Leaving Answer here clears it.

Samples back the charts and are capped near 400 points. An empty database is backfilled with 24 points so the first screen has a line.

The drop list is the `blocks` table, unique on name and port. Port 0 means all ports. Refuse rows are added when the list is read. Their id is `refuse:` plus the name, and they are not stored in `blocks`.

Forwarded connections are notes with category `seen`. They require a known device address, a public IPv4 destination, and a byte count above zero. They do not create a Send decision.

## Enforcement

Reload builds the Envoy file from approved Refuse and Answer here rules. Send, Set aside, and suggestions are omitted from the name-check listener.

The name-check listener is `ano_speaker` on `0.0.0.0:10080`. Refuse is HTTP 403 with `x-ano-action: drop`. Answer here with no target is HTTP 200 with `x-ano-action: allow_envoy`. Answer here with a target rewrites the host and connects with TLS on port 443, and adds `x-ano-reroute`. Any other host is HTTP 404 with `x-ano-action: unmatched`.

Egress listeners are emitted on Linux.

- TCP `ano_egress` on `0.0.0.0:10082` uses the original destination, an optional RBAC deny of literal drop addresses, and a TCP proxy cluster marked so the forwarded packet is not redirected again.
- UDP `ano_udp` on `0.0.0.0:10083` uses a UDP proxy matcher. It has no original-destination listener filter. QUIC is UDP on port 443.

Access-log lines end in ` TCP` or ` UDP`. The follower starts at the end of the file, batches notes about every 200 milliseconds, and writes them through the store. On Linux the conntrack reader used by the gateway returns empty, so the same flow is not counted twice.

## Gateway

`gate.Choose` picks the WAN port from the default route and a LAN port on a different subnet, preferring a private address. Loopback is skipped. `canStart` is true when those two subnets exist and the host can apply rules. The Mac reports that the gateway starts on Ano.

Enable writes `gate.nft` and the dnsmasq file, loads the nftables rules, turns on IP forwarding, and restarts dnsmasq. Disable removes the Ano dnsmasq file and try-restarts dnsmasq. dnsmasq is disabled at boot so a reboot does not claim port 53 before the gateway is wanted.

`RenderNFT` builds table `inet ano`:

- Prerouting on the LAN port drops the all-port address set, then port-specific drops, then TCP/UDP ports 53 and 853 to a non-LAN address, then redirects other public TCP to 10082 and UDP to 10083.
- Forward drops the all-port set, accepts ICMP from LAN to WAN, drops other LAN-to-WAN traffic, and accepts established WAN-to-LAN traffic.
- Postrouting masquerades the LAN network out the WAN port.

There is no `route-to`. Broadcast and multicast addresses are left on the LAN.

`RenderDHCP` serves DHCP on the LAN port only. The router option and the DNS option are the LAN address. An all-port refused name is an empty `address=` line, which is NXDOMAIN. Port-specific names are resolved and then dropped only on that port. An all-port rule wins when both exist. Addresses for those names are resolved over IPv4 with a 1 second timeout and refreshed about every 10 minutes while the gateway is on.

The gateway loop collects every 2 seconds and refreshes the rendered rules about every 10 minutes.

## Added-device proxy

`internal/via` listens on TCP 10081 and can relay DNS on UDP 10053 for a device that was pointed at Ano directly. That path is independent of the two-port gateway. It is how a single added device can be recorded before the access point is rewired.

## Boot on Ano

`deploy/orangepi/install.sh` installs nftables, conntrack, dnsmasq, Envoy 1.40.2, Ollama, Llama 3.2 1B, Node 20 when needed, the screen, and the two systemd units. It disables dnsmasq, enables Ano, the screen, and Ollama, and does not start the gateway.

Both Ano units use `Restart=always`, `RestartSec=3`, and `StartLimitIntervalSec=0`. Ano starts after the network and Ollama. The screen starts after Ano. The Ollama drop-in binds `OLLAMA_HOST=127.0.0.1:11434` and uses the same restart policy.

Replacing a running `/opt/ano/ano` needs the service stopped first. The binary is copied aside, Ano is stopped, the new file is moved into place, and Ano is started again.

## Configuration files

| File | Written by |
| --- | --- |
| `ano.db` | Ano, on first open and after every change |
| `envoy.yaml` | Ano, on each enforcement reload |
| `envoy-access.log` | Envoy, read by Ano |
| `gate.nft` | Ano, when the gateway is applied |
| dnsmasq drop-in | Ano, when the gateway is applied |

Envoy's admin port 19001 and Ollama's port 11434 listen on localhost. The screen and the Ano API are the household-facing ports.
