# Ano system design

Ano is the household's privacy appliance. It watches where devices send, asks a local model what that pattern looks like, and applies Refuse or Send when a person chooses or when Active mode is on. This document describes the household-facing system. [ARCHITECTURE.md](ARCHITECTURE.md) describes the programs and the packet path. [REQUIREMENTS.md](REQUIREMENTS.md) is the checklist for the running app.

## Purpose

Home devices look up vendor names and send small regular uploads: speaker usage reports, TV identification, thermostat check-ins. Ano gives the household a name for each destination, a suggestion, and a button.

Ano keeps three promises:

- The model stays off the packet path. A packet is forwarded or dropped without waiting for the model.
- A suggestion is stored. It changes routing only in Active mode, and a person's button overrides it.
- Ano stores metadata: device, name, port, protocol, counts, and the decision. It does not store audio, page contents, or credentials.

## Where Ano sits

Ano is meant to sit between the home Wi-Fi and the existing router.

```text
modem or existing router -- router-side jack
Ano
access-point-side jack -- Wi-Fi access point -- devices
```

The access point is in access-point mode. Devices use Ano as their gateway and as their only DNS server. Streaming stays forwarded. A refused destination stops at Ano.

Until the second Ethernet jack is connected, Ano is a screen and a reviewer on the one network it already has. The gateway stays off. This Mac is the development machine. It can show the same screen, and it refuses to become the gateway.

## Modes

| Mode | What the household gets |
| --- | --- |
| Passive | Devices, names, counts, and the model's suggestion. Routing stays as it was. |
| Active | The same record, and the model's Refuse and Send are enforced. |

The default is Passive.

A person's button wins over the model:

| Button | Household meaning |
| --- | --- |
| Refuse the name | That destination stops. |
| Send it out | That destination goes out. |
| Answer here | Ano answers locally, or Proxy sends the name to a host the person typed. |
| Set aside | Ano stops enforcing that name. |

Answer here with no Proxy target does not call the vendor. It is a local stand-in, so the household can see the path before a real target exists.

Voice destinations, such as a speaker's answer service, are suggested as Send. Refusing them stops spoken answers. Usage-report names are a separate choice.

## What the household sees

The screen has one menu and one scrolling page.

| Screen | Question it answers |
| --- | --- |
| Overview | How much left, how much was kept, and which names need a decision |
| Route | Where a drop, a send, and an Envoy answer stop |
| System | Which programs are running, and whether the gateway can start |
| Devices | Each device, its names, and the four buttons |
| Admin | When the review runs, and which DNS names are dropped |
| Decisions | The log of choices, newest first |
| Envoy | Whether the name check is up, and what one hostname returns |
| Proxy | Where an Answer here name is sent |

Speakers only is on by default. Overview and Route follow it. Devices still lists the other devices. Proxy lists every name.

Charts use one palette. Orange left the house. Green was kept after a drop. Red was refused. Blue went through Envoy. Counts move every 2 seconds. Empty lists stay empty, and the screen says when there is nothing to show.

## Review schedule

The model reads the past 24 hours on a schedule. It does not read each packet.

Admin is the schedule. It is on or off, every N, and one unit:

| Unit | What the person sets |
| --- | --- |
| Minute | Every 1 to 59 minutes |
| Hour | Every 1 to 24 hours, at a minute of the hour |
| Day | Every 1 to 31 days, at a clock time |
| Week | Every 1 to 52 weeks, on a weekday, at a clock time |
| Month | Every 1 to 12 months, on a day from 1 to 28, at a clock time |

The default is on, every day, at 03:00. Review now runs the same past-day review immediately. A review with no new traffic is marked done and does not call the model. A failed review waits 15 minutes. An earlier choice for a name stays in place when the model looks again.

The suggestion names the device, the destination, how often it sends, which hours it sends, similar names on that same device, and the protocol. Another device's sends are not part of that judgment.

## Ask Ano

Ask Ano is on every screen. The person types, or speaks when the browser can listen. A spoken request can be spoken back.

Three kinds of reply:

- A greeting or a general question gets an answer. Ano does not change anything.
- A request that is missing a detail gets one question and the choices that fit. "Update the schedule" asks for minute, hour, day, week, or month.
- A complete request is applied, and the reply says what was saved.

Ask Ano can change the mode, the schedule, a DNS drop, a device choice, Speakers only, or the gateway. It can run the review now. The gateway request still waits for the second Ethernet jack. A schedule reply is taken from the saved schedule, so a vague model reply cannot quietly keep the old cadence.

## Drop list

Admin can add a DNS name for all ports, or for one port.

- All ports refuses the name. With the gateway on, that name gets no address, and the address is dropped if a device already knows it.
- One port drops only that port. The name still resolves.
- A name refused from a device is shown on the same list. Removing it is done from the device, because that row is the person's Refuse button.

## Gateway

The gateway is the moment Ano is actually in front of the home network.

It stays off in two cases:

- This Mac. Forwarding stays off.
- Ano with one subnet. Start stays disabled until a router-side port and an access-point port are on different networks.

When it is on:

- Devices are told to use Ano as the gateway and as DNS.
- A refused name gets no DNS answer, and the refused address is dropped before it is forwarded.
- Allowed TCP, including HTTPS, and allowed UDP, including QUIC, are carried by Envoy and then forwarded.
- DNS and DNS-over-TLS aimed past Ano are dropped, so a device uses Ano's DNS.
- Streaming stays forwarded.
- The model is still not in that path. Enforcement uses the choices already saved.

Start is a deliberate button in Admin, or a complete Ask Ano request, after the second jack exists.

## Boot

On Ano, three services start on boot and come back if they stop: Ano, the screen, and the model. Envoy is started by Ano. dnsmasq stays disabled until the gateway is turned on, because the DNS port is part of the gateway.

## What this design leaves out

- A cloud model or a cloud account.
- Fine-tuning the model on household traffic.
- Installing a certificate on TVs and speakers. Those devices pin certificates, so the working choices are Refuse and Send.
- The on-board NPU.
- A packet redirect that sends the home network through the development Mac.
