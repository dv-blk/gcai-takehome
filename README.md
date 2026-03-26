# Contract Clause Analyzer

A web application that analyzes vendor contracts for aggressive or risky terms using LLM-powered multi-agent analysis. Upload a contract (text or PDF), and the system examines it across 22 legal/business categories, identifying problematic clauses with specific risks and suggested fixes.

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

## Tech Stack

- **Backend:** Go 1.21+ with Chi router
- **Frontend:** HTMX + Tailwind CSS (CDN)
- **Database:** SQLite (pure Go driver, no CGO)
- **PDF Parsing:** github.com/ledongthuc/pdf
- **Markdown Rendering:** goldmark
- **LLM Integration:** Node.js bridge wrapping OpenCode SDK → GitHub Copilot → Claude

## Prerequisites

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
   npm install -g @anthropic/opencode
   
   # Verify installation
   opencode --version
   ```

4. **GitHub Copilot authentication** configured in OpenCode
   ```bash
   # Run opencode and authenticate with GitHub Copilot
   opencode
   # Follow the prompts to authenticate via github-copilot provider
   ```
   
   This creates `~/.local/share/opencode/auth.json` with your credentials.

## Installation

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd contractanalyzer
   ```

2. **Install Go dependencies**
   ```bash
   go mod download
   ```

3. **Install Node.js bridge dependencies**
   ```bash
   cd opencode-bridge
   npm install
   cd ..
   ```

## Running

1. **Build and run**
   ```bash
   go build -o contractanalyzer .
   ./contractanalyzer
   ```

2. **Open in browser**
   ```
   http://localhost:8080
   ```

The application automatically:
- Creates the `data/` directory for SQLite database and contract files
- Spawns the Node.js bridge as a subprocess
- Starts 4 analysis workers for parallel processing

Press `Ctrl+C` to gracefully shut down (stops the bridge and cleans up).

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OPENCODE_BRIDGE_URL` | Use an external bridge instead of auto-spawning | (auto-spawn) |

**Using an external bridge** (useful for development/debugging):
```bash
# Terminal 1: Start bridge manually
cd opencode-bridge
node bridge.mjs --port=9123

# Terminal 2: Run app pointing to external bridge
OPENCODE_BRIDGE_URL=http://127.0.0.1:9123 ./contractanalyzer
```

## How It Works

1. **Submit a contract** via the web form (paste text or upload PDF)
2. **Contract is saved** to SQLite + flat file storage
3. **Analysis begins** - the system discovers all prompt files from `prompts/*.txt`
4. **Single LLM session** is created per submission - the contract is read once, then each category prompt runs sequentially in the same session
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
└── data/                   # Runtime data (gitignored)
    ├── contractanalyzer.db
    └── contracts/
```

## License

MIT
