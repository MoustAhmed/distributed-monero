# 1. RandomX integration strategy

- Status: Accepted (design decision; implementation is not started)
- Date: 2026-09-12

## Context

The worker's mining data plane must eventually execute real Monero RandomX
proof-of-work and report verifiable results. RandomX is a memory-hard
algorithm with meaningful setup cost (dataset/cache initialization,
optional huge pages, JIT-compiled programs) and a reference implementation
already exists (`tevador/RandomX`, C++, BSD-3-Clause). A mature, widely used
open-source miner (XMRig, GPL-3.0) also already implements RandomX mining
end-to-end, including pool/stratum handling.

Three integration strategies were considered before writing any mining
code:

1. **Direct integration with the reference RandomX library** — bind to
   `librandomx` (e.g., via cgo) and call its hashing API directly from Go,
   while this project owns the surrounding data plane: job assignment,
   nonce/search-space partitioning across workers, and result submission.
2. **Process-level integration with an existing open-source miner** —
   run an existing miner (e.g., XMRig) as a subprocess per worker and drive
   it through its existing API/config surface, treating it as a black box
   that performs hashing and reports results back to this project.
3. **Implementing the required mining data-plane functionality around
   RandomX ourselves** — reimplement the subset of RandomX's data-plane
   behavior (dataset/cache setup, program execution, result formatting)
   needed for correct mining, without depending on the reference C++
   library or an existing miner binary.

## Comparison

| Criterion | 1. Reference library (cgo) | 2. Process-level (existing miner) | 3. Custom implementation |
|---|---|---|---|
| Performance | Matches the reference implementation; the reference library is the performance baseline everyone else is measured against. | Matches whichever miner is chosen; typically also near-optimal, but performance is opaque and version-dependent. | Unlikely to match reference performance without a large, RandomX-specific optimization effort (JIT codegen, dataset layout, huge pages). |
| Engineering ownership | This project owns the data plane end-to-end; hashing internals are delegated to a well-reviewed library. | This project owns almost nothing about how hashing happens; behavior changes whenever the external miner updates. | This project owns everything, including consensus-sensitive hashing correctness — the highest ownership burden and risk. |
| Cross-platform packaging | Requires cgo and a C/C++ toolchain (and prebuilt or vendored `librandomx`) on Windows, Linux, and macOS — nontrivial but well precedented. | Requires shipping or requiring a separate miner binary per platform, plus version pinning. | Pure Go is possible but must still solve the same cross-platform performance primitives (huge pages, cache/NUMA awareness) the reference library already solves. |
| Protocol complexity | Moderate: this project defines its own internal job/result protocol between coordinator and worker. | Higher in practice: must translate this project's job/result model into whatever interface the external miner exposes (HTTP API, stratum, config files), and handle its process lifecycle. | Same protocol complexity as option 1, plus the added complexity of the hashing implementation itself. |
| Licensing | RandomX reference library is BSD-3-Clause — permissive, compatible with linking into an MIT-licensed project. | XMRig is GPL-3.0. Running it as a separate, arm's-length subprocess avoids linking concerns, but tightly coupling to its internals still creates licensing and compliance considerations for the overall system. | No third-party mining code license to manage, at the cost of all the ownership risk above. |
| Observability | Full control: this project can expose per-thread, per-job, and per-nonce telemetry directly from the hashing loop. | Limited to whatever the external miner exposes (e.g., XMRig's HTTP API); anything not exposed there is not observable without patching the miner. | Full control, same as option 1. |
| Development time | Moderate: cgo bindings plus a data-plane implementation, but no algorithm work. | Low up front (an existing miner already works), but ongoing integration/maintenance cost as the external project evolves independently. | Highest: effectively re-deriving a correct, reasonably fast RandomX implementation from the specification. |

## Decision

Adopt **Option 1: direct integration with the reference RandomX library**
via cgo bindings, with this project implementing the mining data plane
(job assignment, search-space partitioning, telemetry, and result
submission) around it in Go.

This is chosen because it keeps consensus-sensitive hashing behavior tied
to the same reference implementation the wider Monero ecosystem trusts,
avoids the licensing and black-box observability costs of embedding or
shelling out to a GPL-licensed full miner, and avoids the correctness risk
and development cost of reimplementing RandomX from scratch. The
cross-platform cgo packaging cost is accepted as the main tradeoff, since
Windows/Linux/macOS laptops are the project's first supported targets and
each has a workable C/C++ toolchain story.

## Consequences

- The worker package will need a cgo build path for `librandomx` on
  Windows, Linux, and macOS, which adds toolchain requirements to CI and
  developer setup beyond plain Go.
- The project takes on responsibility for the entire mining data plane
  (job/nonce assignment, result validation and submission) rather than
  inheriting it from an existing miner.
- Until this integration is implemented, any concurrency/networking/
  scheduling/telemetry work is validated using a small, explicitly labeled
  deterministic development workload instead of real RandomX hashing.
