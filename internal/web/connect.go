package web

import (
	"net/http"
	"strings"
)

// connectText tells people, and AI assistants that read web pages for them,
// how to connect Renoxium. Assistants cannot add connectors on their own, so
// the text asks them to pass the steps on.
const connectText = `# Connect Renoxium to your AI assistant

Renoxium builds and runs online stores and other web apps for you from a chat. To use it, add Renoxium as a connector (also called an app or MCP server) in your assistant once.

- Connector address: {{MCP}}
- Authentication: OAuth (you sign in to Renoxium in a browser window; no API key)
- Transport: Streamable HTTP
- Cost: Renoxium has no credits. Building and changing your apps is not counted or charged per request, on any plan.

Note for AI assistants reading this page: you cannot add this connector yourself. Opening the address above in a browser or fetch tool returns 401 Unauthorized; that is expected, because the connector must be added in the user's settings, after which the user signs in. Tell the user the steps below for the assistant they are using.

## ChatGPT

1. On chatgpt.com in a web browser, open Settings, then Apps (or Apps & Connectors), then Advanced settings.
2. Turn on Developer mode.
3. Choose Create (or Add app) and enter Name: Renoxium, MCP Server URL: {{MCP}}, Authentication: OAuth.
4. Save. A Renoxium window opens: create an account or sign in, then choose Allow.
5. In a new chat, turn on Renoxium in the tools menu (+) and ask, for example: "Build me an online store for my products."
6. When Renoxium gets new tools, ChatGPT does not pick them up by itself: open Renoxium in ChatGPT's Settings (Apps) and choose Refresh tools.

Developer mode is on ChatGPT's paid plans and may only be available on the web. On a free ChatGPT account, use Claude instead: its free plan is enough.

## Claude

1. On claude.ai, open Settings, then Connectors.
2. Choose Add custom connector and enter Name: Renoxium, URL: {{MCP}}.
3. Choose Connect, sign in to Renoxium, then choose Allow.
4. In a chat, make sure Renoxium is turned on in the tools menu and ask for your app.

This works on Claude's free plan, which allows one custom connector.

## Other MCP clients

Add a remote MCP server with the address {{MCP}}. The client discovers OAuth from the server, registers itself and opens the Renoxium sign-in page.

## Templates

Finished websites for UAE businesses (online store, appointments and classes, home services, tours and rentals, quotes and client portal, property listings, delivery tracking) can be tried live, with the owner's admin panel, at {{BASE}}/templates. Ask your assistant to build from one by name.

## After connecting

- Your apps: {{BASE}}
- Manage an app's data (orders, products): {{BASE}}/apps/<app-name>
- Long chats: on free plans a chat fills up after a few long messages. Start a new chat and name your app, for example "Continue with my app al-noor-perfumes": your apps, their versions and data are kept in Renoxium, not in the chat.
- Photos: attach them in the chat (free plans allow only a few per message), or upload them in your app's admin panel; before the first publish, choose Edit preview data there, then tell your assistant which photo goes where.
`

func (s *Site) connectBody() string {
	return strings.NewReplacer("{{MCP}}", s.base+"/mcp", "{{BASE}}", s.base).Replace(connectText)
}

func (s *Site) connectPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Blocks": markdownBlocks(s.connectBody())}
	// Signed in, the page sits in the console like the other account pages.
	if u, ok := s.currentUser(r); ok {
		data["Shell"] = s.ownerShell(u, "Connect and help", "/connect", s.csrfToken(w, r))
	}
	render(w, http.StatusOK, "connect", data)
}

// block is one rendered piece of the small markdown subset used above.
type block struct {
	Kind  string // h1, h2, p, ol, ul
	Text  string
	Items []string
}

// markdownBlocks parses headings, paragraphs and simple lists. Text stays
// plain and is escaped by the template.
func markdownBlocks(md string) []block {
	var out []block
	for _, para := range strings.Split(strings.TrimSpace(md), "\n\n") {
		lines := strings.Split(para, "\n")
		switch {
		case strings.HasPrefix(para, "## "):
			out = append(out, block{Kind: "h2", Text: strings.TrimPrefix(para, "## ")})
		case strings.HasPrefix(para, "# "):
			out = append(out, block{Kind: "h1", Text: strings.TrimPrefix(para, "# ")})
		case strings.HasPrefix(para, "- "):
			b := block{Kind: "ul"}
			for _, l := range lines {
				b.Items = append(b.Items, strings.TrimPrefix(l, "- "))
			}
			out = append(out, b)
		case len(para) > 2 && para[0] >= '1' && para[0] <= '9' && para[1] == '.':
			b := block{Kind: "ol"}
			for _, l := range lines {
				if _, item, ok := strings.Cut(l, ". "); ok {
					b.Items = append(b.Items, item)
				}
			}
			out = append(out, b)
		default:
			out = append(out, block{Kind: "p", Text: strings.Join(lines, " ")})
		}
	}
	return out
}

func (s *Site) llmsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(s.connectBody()))
}

// mcpEndpoint answers a plain browser or fetch-tool visit to /mcp with
// instructions instead of a bare 401, keeping the 401 and the OAuth
// challenge that MCP clients rely on. MCP traffic goes to the MCP handler.
func (s *Site) mcpEndpoint(w http.ResponseWriter, r *http.Request) {
	isMCP := r.Method != http.MethodGet || r.Header.Get("Authorization") != "" ||
		strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	if isMCP {
		cors(s.mcp.ServeHTTP)(w, r)
		return
	}
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+s.base+`/.well-known/oauth-protected-resource/mcp"`)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte("401 Unauthorized: this is the Renoxium MCP endpoint. It is not a web page and cannot be\n" +
		"connected from a chat message. Add it as a connector in your assistant's settings.\n" +
		"Instructions: " + s.base + "/connect\n\n" + s.connectBody()))
}
