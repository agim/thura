# Deploy minimum-profile acceptance

On 2026-10-20 the actual Thura source was deployed through Līdza Deploy's Linux
agent into an isolated ARM64 VM with **2 CPUs, 4 GiB RAM and no swap**. This was
not the platform's toy orchestration fixture. The agent itself ran inside the
same VM. The original interactive demo was left on its separate Docker context.

Source: Thura `24e50db794ddf65de38473bb6fe3c713ba06b1f2` on
`feat/thura-deployment-profiles`; platform contract/runtime checkpoint `4cc4754`
on `feat/stack-profiles`. The manifest is `deploy/lidza.stack.json`. Machine
measurements and assertions are in
[deploy-stack-acceptance.json](benchmarks/deploy-stack-acceptance.json).

## What passed

- The actual agent provisioned a private managed PostgreSQL database at the
  minimum limit. Thura's three-service manifest attached it as the fourth
  running container; it did not create another PostgreSQL or cache container.
- A fresh production install reached **Setup required** with all service checks
  healthy. Its genuine `/readyz` remained 503 and normal APIs remained guarded.
  The real wizard claimed the operator, saved the environment-seeded draft,
  sent one provider probe to a private SMTP sink, and created/read/listed/deleted
  a real storage probe. Publication followed by coordinated agent reload reached
  genuine `/readyz` 200 and cleared the bootstrap flag. No test-only setup marker
  or `LIDZA_MODE=test` was used.
- Real browser tests uploaded DOCX and XLSX, edited each in Collabora, saved
  version 2, downloaded it and verified the document text/spreadsheet XML. Both
  tests passed again after rollback. Original versions stayed available.
- Actual SMTP ingress persisted an email in Thura. Receiving used Postfix while
  the outgoing probe used a separately configured SMTP provider sink. The public
  receiving port refused an arbitrary external relay recipient. Report spam
  moved the message to Junk and completed durable spam learning; Not spam restored
  Inbox and completed the ham correction.
- Clean antivirus scans passed; EICAR was detected. ClamAV signature reload,
  Rspamd scanning, repeated learning and reversed ham/spam corrections passed.
  The real `/rtc` signaling health route returned OK; the SFU process also passed
  its own health check.
- An agent rollback retained all four initial immutable version records, the
  exact SHA-256 values of the stored objects, learned feedback and canonical
  durable Bayes hash values. It reused the same managed database attachment.
  Runtime-only history keys and unordered Redis SCAN output are not retention
  assertions.

No paid model/provider request or external email recipient was used. Optional
AI screening was disabled for this deployment acceptance; its settings toggle
and normal configured-provider integration remain unchanged.

## Resource evidence

| Running service | Hard RAM cap | CPU cap | Recorded cgroup peak |
|---|---:|---:|---:|
| Thura + LiveKit | 512 MiB | 0.50 | 226.1 MiB |
| Mail + Rspamd + ClamAV + Valkey | 1792 MiB | 0.75 | 1267.0 MiB |
| Collabora | 1024 MiB | 0.50 | 886.6 MiB |
| Managed PostgreSQL | 256 MiB | 0.25 | 80.8 MiB |

Container caps total **3584 MiB / 2 CPUs**, leaving the nominal **512 MiB host
allowance**. Linux reported about 3904 MiB usable RAM; kernel-reserved RAM is
accounted within that allowance, not deducted from service caps. The Linux agent
used about 27 MiB RSS after checks. After checks, Linux reported about 1.49 GiB
available RAM. All service cgroups recorded zero OOM/kill/limit events and no
service container restarted unexpectedly. Office cgroup counters were read from
the Linux host because the vendor image is distroless.

The sum of individually recorded service peaks is about **2.403 GiB**. Those
peaks need not occur simultaneously; this is not a sampled simultaneous host
peak or a throughput/capacity claim. The entire VM remained constrained to the
stated budget throughout the real workload.

## Issues caught and corrected

Actual acceptance caught several gaps that simple orchestration fixtures did not:

- The initial scaffold's sealed developer credentials could not be decrypted by
  a fresh installation's key. Preserve installation keys and local sealed files;
  do not distribute a developer's scaffold ciphertext. The original was retained
  in Git history and a private backup before removing it from distribution.
- Wizard drafts now seed catalogued operator settings so managed storage and
  private service origins match the published snapshot. Private HTTP origins
  require an explicit operator allowlist; public HTTPS/WSS checks remain.
- Empty Rspamd volumes need service-account ownership initialization.
- Root app supervisors need the bounded KILL capability to signal workers after
  dropping their UID. The app/mail policies retain no-new-privileges.
- Collabora must keep its vendor non-root user and its narrowly bounded file
  capabilities. It gets an office-only exception to no-new-privileges, with only
  CHOWN/FOWNER/SYS_CHROOT. A static probe checks its real discovery endpoint;
  the distroless image does not provide curl. Real editing verified this policy.
- First-run setup needs an explicit bootstrap phase before normal readiness.
  Established deployments cannot use this phase to hide an outage.

## Reproduction and limits

See [the acceptance harness](../scripts/stack-acceptance/README.md). The final
application strict verification, all profile checks, four client/wizard harness
checks and both real Office save tests passed. Private agent state, keys, source
credentials and raw logs remain under `.lidza`, outside Git.

This is a functional small-workload acceptance. It does not establish a user
count, sustained email throughput, detection/false-positive rates, or support
for large concurrent workbooks. The committed vendor pin was exercised on
ARM64; x86-64 requires its matching tested image. Standard/performance profiles
remain sizing proposals. This run did not retest a physical Android, a media
call, TURN, public DNS/ACME/MX or Internet SMTP delivery. Production still needs
those deployment-specific checks.

After QA the owned acceptance agent and VM were stopped to release memory;
their private state and volumes were retained for reproduction. The original
interactive demo services remained running on their existing Docker context.
