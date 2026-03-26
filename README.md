# Contract Clause Analyzer

A web application that analyzes vendor contracts for aggressive or risky terms using LLM-powered multi-agent analysis. Upload a contract (text or PDF), and the system examines it across 22 legal/business categories, identifying problematic clauses with specific risks and suggested fixes.

## Quick Start (Docker)

The easiest way to run the analyzer is with Docker Compose.

### Prerequisites

- Docker and Docker Compose

### Step 1: Clone and Start

```bash
git clone <repository-url>
cd gcai-takehome
docker compose up -d
```

This starts two containers:
- **opencode** — LLM server (web UI at http://localhost:4097)
- **app** — Contract Analyzer (web UI at http://localhost:8080)

### Step 2: Authenticate with GitHub Copilot

The analyzer uses GitHub Copilot as its LLM provider. You must authenticate before submitting contracts.

1. Open http://localhost:4097 in your browser (OpenCode web UI)
2. Click the **model selector button** in the interface
3. Select **GitHub Copilot** as the provider
4. A device code will appear — follow the link to https://github.com/login/device
5. Enter the code and authorize the application
6. Once authenticated, the credentials are saved to a persistent Docker volume

> **Note:** The OpenCode web UI is on port 4097 (not the default 4096) because the compose file maps host port 4097 to container port 4096.

### Step 3: Use the Application

Open http://localhost:8080 to access the Contract Clause Analyzer.

1. Paste contract text or upload a PDF
2. Click "Analyze Contract"
3. Watch as analysis results stream in across 22 categories
4. Results are saved and can be viewed later from the sidebar

## Analysis Categories

The analyzer checks contracts for issues in:

- Assignment & Change of Control
- Audit & Reporting
- Business Continuity & DR
- Confidentiality
- Data Ownership & Use
- Data Privacy
- Dispute Resolution & Governing Law
- Exit & Transition
- Force Majeure
- Implementation & Support
- Insurance
- Intellectual Property
- Liability & Indemnification
- Licensing Scope
- Marketing & Publicity
- Pricing & Payment Terms
- Product Changes & Roadmap
- Security & Compliance
- Service Levels (SLA)
- Subcontractors & Third Parties
- Term & Termination
- Warranties & Disclaimers

## How It Works

1. **Submit a contract** via the web form (paste text or upload PDF)
2. **Contract is saved** to SQLite + flat file storage
3. **Analysis begins** — the system loads all prompt files from `prompts/*.txt`
4. **Single LLM session** is created per submission — the contract is read once, then each category prompt runs sequentially in the same session
5. **Results stream** to the frontend via HTMX polling (every 2s), with new sections fading in as they complete
6. **Final results** are persisted and available for later viewing

### Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────────┐
│   Browser   │────▶│  Go Server   │────▶│  Node.js Bridge │
│   (HTMX)    │◀────│  (Chi + SQL) │◀────│  (OpenCode SDK) │
└─────────────┘     └──────────────┘     └─────────────────┘
                           │                      │
                           ▼                      ▼
                    ┌────────────┐         ┌─────────────┐
                    │  SQLite +  │         │   GitHub    │
                    │ Flat Files │         │   Copilot   │
                    └────────────┘         └─────────────┘
```

## Local Development

If you prefer to run without Docker, you can build and run locally.

### Prerequisites

1. **Go 1.21 or later**
   ```bash
   go version
   ```

2. **Node.js 18 or later**
   ```bash
   node --version
   ```

3. **OpenCode CLI** installed and in PATH
   ```bash
   # Install via npm
   npm install -g opencode
   
   # Verify installation
   opencode --version
   ```

4. **GitHub Copilot authentication** configured in OpenCode
   ```bash
   # Run opencode and authenticate with GitHub Copilot
   opencode
   # Use /connect command, select GitHub Copilot, follow device code flow
   ```
   
   This creates `~/.local/share/opencode/auth.json` with your credentials.

### Installation

```bash
# Clone the repository
git clone <repository-url>
cd contractanalyzer

# Install Go dependencies
go mod download

# Install Node.js bridge dependencies
cd opencode-bridge
npm install
cd ..
```

### Running

```bash
# Build and run
go build -o contractanalyzer .
./contractanalyzer
```

Open http://localhost:8080 in your browser.

The application automatically:
- Creates the `data/` directory for SQLite database and contract files
- Spawns the Node.js bridge as a subprocess
- Starts 4 analysis workers for parallel processing

Press `Ctrl+C` to gracefully shut down.

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OPENCODE_BRIDGE_URL` | Use an external bridge instead of auto-spawning | (auto-spawn) |
| `OPENCODE_SERVER_URL` | Connect bridge to external OpenCode server | (embedded) |
| `OPENCODE_PROJECT_DIR` | Working directory for LLM sessions | Current directory |

**Using an external bridge** (useful for development/debugging):
```bash
# Terminal 1: Start bridge manually
cd opencode-bridge
node bridge.mjs --port=9123

# Terminal 2: Run app pointing to external bridge
OPENCODE_BRIDGE_URL=http://127.0.0.1:9123 ./contractanalyzer
```

## Docker Architecture

The Docker setup uses two containers with a shared volume for cross-container file access:

```
┌─────────────────────────────────────────────────────────────────┐
│                         Host Machine                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────────────────┐       ┌─────────────────────────────┐  │
│  │   contractanalyzer  │       │      smanx/opencode         │  │
│  │      (app:8080)     │──────▶│       (opencode:4096)       │  │
│  │                     │       │                             │  │
│  │  Go Server + Bridge │       │   OpenCode LLM Server       │  │
│  │                     │       │   Web UI on host:4097       │  │
│  └─────────┬───────────┘       └──────────────┬──────────────┘  │
│            │                                  │                 │
│            ▼                                  ▼                 │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    app-data volume                      │    │
│  │         (shared: /app/data ←→ /workspace/data)          │    │
│  │                                                         │    │
│  │  • contractanalyzer.db (SQLite)                         │    │
│  │  • contracts/ (uploaded contract files)                 │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                 │
│  ┌───────────────────┐    ┌───────────────────┐                 │
│  │  opencode-data    │    │  opencode-config  │                 │
│  │  (auth.json)      │    │  (settings)       │                 │
│  └───────────────────┘    └───────────────────┘                 │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

The `app-data` volume is mounted in both containers:
- **app container:** `/app/data` — where contracts are saved
- **opencode container:** `/workspace/data` — where the LLM agent reads them

The `opencode.json` file is bind-mounted into the OpenCode container to configure read-only agent permissions.

### Docker Volumes

| Volume | Purpose |
|--------|---------|
| `opencode-data` | OpenCode auth credentials and session data |
| `opencode-config` | OpenCode configuration |
| `app-data` | SQLite database and stored contract files (shared) |

### Building Without Compose

```bash
# Build the image
docker build -t contractanalyzer .

# Run with an external OpenCode server
docker run -p 8080:8080 \
  -e OPENCODE_SERVER_URL=http://host.docker.internal:4096 \
  -e OPENCODE_PROJECT_DIR=/workspace \
  -v contractanalyzer-data:/app/data \
  contractanalyzer
```

## Adding/Modifying Analysis Categories

Analysis prompts are loaded dynamically from the `prompts/` directory. To add a new category:

1. Create a new `.txt` file in `prompts/` (e.g., `prompts/new_category.txt`)
2. Follow the existing prompt format:
   ```
   Now analyze the contract for aggressive **Category Name** clauses...
   ```
3. Restart the application

No code changes required. Prompts are sorted alphabetically by filename for consistent ordering.

## Project Structure

```
contractanalyzer/
├── main.go                 # Entry point, routing, server setup
├── agent/                  # LLM agent and bridge management
│   ├── agent.go            # HTTP client to bridge
│   └── bridge.go           # Bridge subprocess spawning
├── analyzer/               # Analysis orchestration
│   ├── prompts.go          # Prompt discovery from prompts/
│   └── queue.go            # Worker pool, session management
├── database/               # SQLite operations
├── handlers/               # HTTP handlers
├── models/                 # Data models
├── pdfutil/                # PDF text extraction
├── storage/                # Flat file storage for contracts
├── opencode-bridge/        # Node.js bridge server
│   ├── bridge.mjs          # HTTP server wrapping OpenCode SDK
│   └── package.json
├── prompts/                # Analysis prompt files (22 categories)
├── templates/              # Go HTML templates
│   ├── layout.html
│   └── partials/
├── static/css/             # Custom CSS
├── opencode.json           # Permission config for Docker OpenCode server
├── Dockerfile              # Multi-stage Docker build
├── docker-compose.yml      # Compose with smanx/opencode
└── data/                   # Runtime data (gitignored)
    ├── contractanalyzer.db
    └── contracts/
```

## Tech Stack

- **Backend:** Go 1.21+ with Chi router
- **Frontend:** HTMX + Tailwind CSS (CDN)
- **Database:** SQLite (pure Go driver, no CGO)
- **PDF Parsing:** github.com/ledongthuc/pdf
- **Markdown Rendering:** goldmark (server-side)
- **LLM Integration:** Node.js bridge wrapping OpenCode SDK → GitHub Copilot → Claude Sonnet 4

## License

MIT
