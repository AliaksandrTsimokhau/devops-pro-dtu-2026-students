# Docker — Self-Paced Labs

Self-paced lab series based on practical examples from *Docker Deep Dive* (Nigel Poulton, May 2025).

## Contents

| File | Purpose |
|------|---------|
| `docker-labs-slides.md` | MARP slide deck — for the lecturer / quick visual reference. Run a slide-by-slide overview, then send students to the lab handout. |
| `docker-labs.md` | Student handout — full step-by-step labs with every command, expected output, troubleshooting, cleanup, and verification checklists. **This is the document students follow.** |
| `docker-labs-slides.html` *(generated)* | HTML export of the deck — presenter mode, viewable in any browser. |
| `docker-labs-slides.pdf` *(generated)* | PDF export for offline distribution. |

## Lab roadmap

| Module | Lab | Title | Time |
|--------|-----|-------|------|
| 1 — Foundations | 0 | Install Docker | 30 min |
|                 | 1 | First container | 20 min |
|                 | 2 | Container lifecycle | 25 min |
| 2 — Images      | 3 | Working with images | 30 min |
|                 | 4 | Containerize a Node.js app | 40 min |
|                 | 5 | Multi-stage builds | 35 min |
| 3 — Compose     | 6 | Multi-container with Compose | 40 min |
|                 | 7 | Bridge networks deep dive | 30 min |
|                 | 8 | Volumes & persistent data | 25 min |
| 4 — Cluster     | 9 | Build a Swarm + deploy a stack | 45 min |
|                 | 10 | Overlay networking across hosts | 35 min |
| 5 — Security    | 11 | Vulnerability scanning with Docker Scout | 25 min |
|                 | 12 | Content Trust + Swarm Secrets | 35 min |
| 6 — Advanced    | 13 | Docker Model Runner + chatbot *(optional)* | 50 min |
|                 | 14 | Wasm containers *(optional)* | 35 min |

Total time (core labs 0–12): **~6.5 hours**. Optional labs add ~1.5 hours.

## Suggested delivery patterns

- **2-day workshop:** Day 1 — Modules 1–3 (foundations, images, compose). Day 2 — Modules 4–5 (cluster, security), plus optional labs.
- **Self-study, 1 lab per evening:** Two weeks to complete.
- **Lecture + lab pairing:** Present 2–3 slide sections, then let students work through the corresponding labs in `docker-labs.md`.

## Required environments

| Lab | Docker Desktop OK? | Multipass / PWD needed? |
|-----|-------------------|-------------------------|
| 0–8, 11 | Yes | No |
| 7 (bridge deep dive) | Works, but Linux/Multipass gives full `brctl`/`ip` views | Recommended |
| 9, 10 (Swarm + overlay) | No — single node only | **Required** (2–5 nodes) |
| 12 (DCT + secrets) | DCT yes; Swarm secrets need a Swarm | Swarm part requires multi-node |
| 13 (DMR) | Docker Desktop 4.41+ required | No |
| 14 (Wasm) | Docker Desktop 4.37+ required | Not supported on Multipass |

[Play With Docker](https://labs.play-with-docker.com/) is the fastest way to get multi-node Swarms for Labs 9 and 10.

## Exporting the slides

From this folder:

```bash
# HTML (presenter mode, fast)
npx @marp-team/marp-cli@latest --no-stdin docker-labs-slides.md -o docker-labs-slides.html --allow-local-files --html

# PDF
npx @marp-team/marp-cli@latest --no-stdin --pdf docker-labs-slides.md -o docker-labs-slides.pdf --allow-local-files

# PPTX (text rendered as images by default)
npx @marp-team/marp-cli@latest --no-stdin --pptx docker-labs-slides.md -o docker-labs-slides.pptx --allow-local-files --html
```

`--html` is required because the deck uses inline SVG diagrams and HTML cards.

## Sources

- Nigel Poulton, *Docker Deep Dive — Zero to Docker in a single book* (May 2025) — [leanpub.com/dockerdeepdive](https://leanpub.com/dockerdeepdive)
- Companion notes vault: `JD Daily/20-29 Knowledge Base/20.09 - Docker Deep Dive Book Notes.md`
