import { createServer } from "node:http";
import { createOpencode, createOpencodeClient } from "@opencode-ai/sdk/v2";

// Parse CLI args
const args = process.argv.slice(2);
function getArg(name) {
  const arg = args.find((a) => a.startsWith(`--${name}=`));
  return arg ? arg.split("=").slice(1).join("=") : undefined;
}

const port = parseInt(getArg("port") || "0", 10);
const opencodePort = parseInt(getArg("opencode-port") || "0", 10);
const opencodeURL = getArg("opencode-url") || process.env.OPENCODE_URL;
const logLevel = getArg("log-level") || "WARN";

if (!port) {
  console.error("Usage: node bridge.mjs --port=PORT [--opencode-port=PORT] [--opencode-url=URL] [--log-level=LEVEL]");
  process.exit(1);
}

const log = (msg, ...a) => console.error(`[bridge] ${msg}`, ...a);

// Initialize OpenCode client (either connect to external server or spawn our own)
let client;
let server = null;

if (opencodeURL) {
  // External mode: connect to an existing OpenCode server
  log(`Connecting to external OpenCode server at ${opencodeURL}...`);
  client = createOpencodeClient({ baseUrl: opencodeURL });
  log(`Connected to external OpenCode server at ${opencodeURL}`);
} else {
  // Embedded mode: start our own OpenCode server
  log("Starting embedded OpenCode server...");
  const result = await createOpencode({
    port: opencodePort || undefined,
    config: {
      logLevel,
      lsp: false,
      formatter: false,
      permission: {
        read: "allow",
        edit: "deny",
        bash: "deny",
        glob: "deny",
        grep: "deny",
        list: "deny",
        external_directory: "deny",
        task: "deny",
        webfetch: "deny",
      },
    },
  });
  client = result.client;
  server = result.server;
  log(`Embedded OpenCode server started at ${server.url}`);
}

// Read JSON body from request
function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => {
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString()));
      } catch (e) {
        reject(new Error("Invalid JSON body"));
      }
    });
    req.on("error", reject);
  });
}

// Send JSON response
function sendJSON(res, status, data) {
  const body = JSON.stringify(data);
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Content-Length": Buffer.byteLength(body),
  });
  res.end(body);
}

// Extract text from message parts
function extractText(parts) {
  const textParts = [];
  for (const part of parts || []) {
    if (part.type === "text" && part.text) {
      textParts.push(part.text);
    }
  }
  return textParts.join("").trim();
}

// Extract assistant response text from a list of session messages
function extractAssistantResponse(messages) {
  if (!messages || messages.length === 0) return "";
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i];
    if (msg.info.role === "assistant") {
      const text = extractText(msg.parts);
      if (text) return text;
    }
  }
  return "";
}

// Default prompt timeout in ms (can be overridden per-request via body.timeoutMs).
// Opus models are slow; 120s is a reasonable upper bound.
const DEFAULT_PROMPT_TIMEOUT_MS = 120_000;

// Handle POST /prompt
//
// Strategy: always use the async prompt path with SSE event streaming.
// 1. Subscribe to SSE events (before prompting, to avoid missing the idle event)
// 2. Fire promptAsync() — returns immediately (fire-and-forget)
// 3. Iterate the SSE stream waiting for session.idle (success) or session.error
// 4. Fetch session.messages() to retrieve the assistant response
//
// Session persistence options:
// - sessionID: if provided, reuse an existing session instead of creating a new one
// - deleteSession: if false, don't delete the session after the prompt (default: true)
async function handlePrompt(req, res) {
  const body = await readBody(req);
  const { text, providerID, modelID, directory, timeoutMs, sessionID: existingSessionID, deleteSession = true } = body;
  const timeout = timeoutMs || DEFAULT_PROMPT_TIMEOUT_MS;

  if (!text) {
    return sendJSON(res, 400, { error: "missing 'text' field" });
  }

  log(`Prompt request: provider=${providerID}, model=${modelID}, timeout=${timeout}ms, sessionID=${existingSessionID || "(new)"}, deleteSession=${deleteSession}, text=${text.substring(0, 80)}...`);

  // Create or reuse session
  let sessionID;
  if (existingSessionID) {
    sessionID = existingSessionID;
    log(`Reusing existing session: ${sessionID}`);
  } else {
    const { data: session } = await client.session.create({ directory });
    sessionID = session.id;
    log(`Session created: ${sessionID}`);
  }

  // Timeout — we abort everything if this fires
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), timeout);

  try {
    // 1. Subscribe to SSE events BEFORE firing the prompt (avoids race)
    log("Subscribing to SSE events...");
    const { stream } = await client.event.subscribe({ directory });

    // 2. Fire async prompt (returns immediately)
    const promptParams = {
      sessionID,
      directory,
      parts: [{ type: "text", text }],
    };
    if (providerID && modelID) {
      promptParams.model = { providerID, modelID };
    }

    log("Firing promptAsync...");
    await client.session.promptAsync(promptParams);
    log("promptAsync accepted");

    // 3. Wait for session.idle or session.error on the SSE stream
    log("Waiting for session.idle on SSE stream...");
    for await (const event of stream) {
      if (ac.signal.aborted) {
        throw new Error(`Timed out after ${timeout}ms waiting for model response`);
      }

      if (event.type === "session.error") {
        if (event.properties.sessionID === sessionID) {
          const err = event.properties.error;
          const errMsg = typeof err === "string" ? err : JSON.stringify(err) || "unknown session error";
          throw new Error(`Session error from model: ${errMsg}`);
        }
      }

      if (event.type === "session.idle") {
        if (event.properties.sessionID === sessionID) {
          log("Received session.idle event");
          break;
        }
      }
    }

    if (ac.signal.aborted) {
      throw new Error(`Timed out after ${timeout}ms waiting for model response`);
    }

    // 4. Fetch messages to get the assistant response
    log("Fetching session messages...");
    const { data: messages } = await client.session.messages({
      sessionID,
      directory,
    });

    log(`Got ${messages ? messages.length : 0} messages`);

    const responseText = extractAssistantResponse(messages);
    if (!responseText) {
      throw new Error("Session completed but no assistant response text found");
    }

    log(`Response received (${responseText.length} chars)`);
    sendJSON(res, 200, { text: responseText, sessionID });
  } finally {
    clearTimeout(timer);

    // Clean up session only if deleteSession is true
    if (deleteSession) {
      try {
        await client.session.delete({ sessionID, directory });
        log(`Session ${sessionID} deleted`);
      } catch (e) {
        log(`Failed to delete session ${sessionID}:`, e.message);
      }
    } else {
      log(`Session ${sessionID} preserved (deleteSession=false)`);
    }
  }
}

// Handle DELETE /session
// Explicitly delete a session (for cleanup after errors or when done with a session)
async function handleDeleteSession(req, res) {
  const body = await readBody(req);
  const { sessionID, directory } = body;

  if (!sessionID) {
    return sendJSON(res, 400, { error: "missing 'sessionID' field" });
  }

  log(`Delete session request: sessionID=${sessionID}`);

  try {
    await client.session.delete({ sessionID, directory });
    log(`Session ${sessionID} deleted`);
    sendJSON(res, 200, { ok: true, sessionID });
  } catch (e) {
    log(`Failed to delete session ${sessionID}:`, e.message);
    sendJSON(res, 500, { error: e.message, sessionID });
  }
}

// HTTP server
const httpServer = createServer(async (req, res) => {
  try {
    if (req.method === "GET" && req.url === "/health") {
      return sendJSON(res, 200, { ok: true, opencodeUrl: server?.url || opencodeURL });
    }

    if (req.method === "POST" && req.url === "/prompt") {
      return await handlePrompt(req, res);
    }

    if (req.method === "DELETE" && req.url === "/session") {
      return await handleDeleteSession(req, res);
    }

    sendJSON(res, 404, { error: "not found" });
  } catch (e) {
    log("Request error:", e.message, e.stack);
    sendJSON(res, 500, { error: e.message });
  }
});

httpServer.listen(port, "127.0.0.1", () => {
  const addr = httpServer.address();
  // This line is parsed by the Go test harness to know the bridge is ready
  console.log(`bridge listening on http://127.0.0.1:${addr.port}`);
});

// Graceful shutdown
function shutdown() {
  log("Shutting down...");
  httpServer.close();
  if (server) {
    server.close();
  }
  process.exit(0);
}

process.on("SIGTERM", shutdown);
process.on("SIGINT", shutdown);
