# Architecture

> **Status:** This document describes the intended architecture of
> distributed-monero. Only the `cluster help` and `cluster version` CLI
> commands are implemented today. Everything else described below —
> the coordinator, the worker agent, the control plane, the telemetry
> plane, and the mining data plane — is planned and not yet built.

## Goals

distributed-monero coordinates multiple heterogeneous, commodity CPUs —
starting with Windows, Linux, and macOS laptops — into a single cluster for
Monero RandomX proof-of-work. The project is a distributed-systems exercise
first and a miner second: the core question it exists to answer is how
closely a cluster's measured aggregate hashrate can approach the sum of its
workers' standalone hashrates, not how fast any single CPU can be made to
hash.

```text
Ideal aggregate hashrate = sum of every worker's standalone baseline
Scaling efficiency        = measured cluster hashrate / ideal aggregate hashrate
```

Because workers are expected to have different hardware, the project
deliberately avoids `speedup / node count` as an efficiency metric — that
metric assumes homogeneous nodes, which this cluster does not have.

## Runtime roles

A single codebase and binary (`cluster`) plays one of two runtime roles,
selected by the command used to start it.

### Coordinator

The coordinator owns cluster state and decision-making. It is responsible
for:

- Cluster initialization and enrollment information (join tokens/addresses)
- Worker authentication
- The worker registry and each worker's reported capabilities
- Worker health tracking (heartbeats, failure detection, reconnection)
- Work coordination and scheduling across heterogeneous workers
- Preventing unnecessary overlap in the RandomX search space
- Asynchronous telemetry collection and cluster-wide metric aggregation
- Mining-result coordination

The coordinator does not mine. It is explicitly excluded from contributing
CPU cycles to the workload in the initial implementation. A future
`--also-mine` option may let the coordinator machine participate as a
worker as well, but that option does not exist yet.

### Worker

A worker joins a cluster and executes work only when explicitly told to.
It is responsible for:

- Joining an existing cluster and authenticating with the coordinator
- Reporting its CPU capabilities (cores, cache, NUMA topology, AES-NI,
  large-page support, etc.)
- Remaining idle after joining until it receives an explicit instruction
- Executing RandomX computation only when instructed
- Reporting mining results and sending asynchronous telemetry and heartbeats
- Reconnecting after temporary network failures
- Respecting CPU, thermal, and battery policies

Joining a cluster must never, by itself, start mining.

## Planes

The system is organized around three logical planes that run over the same
authenticated LAN transport but serve different purposes.

### Control plane

Carries enrollment, authentication, capability discovery, scheduling
decisions, and explicit start/stop commands between the coordinator and
workers. This plane must authenticate both ends and protect credentials in
transit before it is described as secure — no such claim is made until that
is actually implemented. It is scoped to a trusted local network first;
public internet exposure, NAT traversal, and cloud/Kubernetes deployment are
explicitly out of scope for the initial implementation.

### Telemetry plane

Carries asynchronous, one-way status data from workers to the coordinator:
worker ID, sequence number, CPU model/topology, CPU and memory utilization,
temperature (where available), current/average hashrate, active thread
count, accepted/rejected/stale result counts, errors, heartbeats, and event
timestamps. Telemetry is designed to tolerate duplication, delay,
reconnection, and out-of-order delivery — sequence numbers and timestamps
let the coordinator de-duplicate and re-order events rather than trusting
delivery order.

### Mining data plane

Carries the actual RandomX work: job/search-space assignment, nonce
distribution, and result submission. This plane is the least defined today.
Before any code is written for it, the tradeoffs between integrating the
reference RandomX library directly, integrating with an existing miner at
the process level, and implementing the required data-plane functionality
in-house are recorded in
[docs/decisions/0001-randomx-integration-strategy.md](decisions/0001-randomx-integration-strategy.md).

A small, clearly-labeled deterministic development workload may be used
ahead of real RandomX integration to exercise concurrency, networking,
scheduling, and telemetry — it is never a substitute for, or presented as,
real mining.

## Scheduling

Because workers are heterogeneous, the scheduler treats worker capacity as a
first-class input rather than assuming equal workers. Planned scheduling
inputs include standalone hashrate, logical/physical core counts, cache
capacity, available memory, NUMA topology, hardware AES support, large-page
support, CPU temperature, power-source state, user-defined CPU limits, and
hashrate per watt. The scheduler's job is resource allocation, worker
eligibility, and search-space coordination — not just handing out equal
shares of work.

## Safety constraints

The project will not implement hidden execution, unauthorized persistence,
silent mining, arbitrary remote shell functionality, credential collection,
authorization bypasses, security-control evasion, or self-propagating code.
Workers may only run on machines the operator owns or is authorized to use,
and mining requires an explicit authenticated coordinator command.
