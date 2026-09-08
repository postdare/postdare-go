package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hellodeveye/postdare-go/internal/mcp"
	"github.com/hellodeveye/postdare-go/internal/middleware"
	"github.com/hellodeveye/postdare-go/internal/util"
)

// mcpMaxRequestBytes mirrors the line cap the stdio transport gives its
// scanner, so the same message is accepted over either transport.
const mcpMaxRequestBytes = 4 << 20

// MCPEndpoint serves the MCP Streamable HTTP transport at POST /mcp. Every
// response is a single JSON-RPC message: none of the tools is long-running, so
// there is nothing to stream and no session to carry between calls. The
// endpoint therefore answers with application/json, which the transport allows
// in place of an SSE stream.
func (h *Handler) MCPEndpoint(server *mcp.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.mcpOriginAllowed(c.GetHeader("Origin")) {
			util.Error(c, http.StatusForbidden, "MCP_ORIGIN_REJECTED", "Origin is not allowed", nil)
			return
		}
		if version := strings.TrimSpace(c.GetHeader("MCP-Protocol-Version")); version != "" && !mcp.IsSupportedProtocolVersion(version) {
			util.Error(c, http.StatusBadRequest, "MCP_PROTOCOL_VERSION_UNSUPPORTED", "Unsupported MCP protocol version",
				gin.H{"supported": mcp.SupportedProtocolVersions()})
			return
		}
		if !middleware.IsMCP(c) {
			util.Error(c, http.StatusForbidden, "MCP_TOKEN_REQUIRED", "The MCP API token is required for this endpoint", nil)
			return
		}
		if !mcpAcceptsJSON(c.GetHeader("Accept")) {
			util.Error(c, http.StatusNotAcceptable, "MCP_ACCEPT_UNSUPPORTED", "Accept must allow application/json", nil)
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, mcpMaxRequestBytes+1))
		if err != nil {
			util.Error(c, http.StatusBadRequest, "MCP_BODY_UNREADABLE", "Failed to read request body", nil)
			return
		}
		if len(body) > mcpMaxRequestBytes {
			util.Error(c, http.StatusRequestEntityTooLarge, "MCP_BODY_TOO_LARGE", "Request body is too large", nil)
			return
		}
		message := bytes.TrimSpace(body)
		// Batching was dropped in the 2025-06-18 revision, and answering one
		// element of a batch with a lone response would be worse than saying so.
		if len(message) > 0 && message[0] == '[' {
			c.Data(http.StatusBadRequest, "application/json",
				[]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"JSON-RPC batching is not supported"}}`))
			return
		}
		raw, ok := server.HandleLine(message)
		if !ok {
			// A notification has no reply, and the transport asks for a bare 202.
			c.Status(http.StatusAccepted)
			return
		}
		c.Data(http.StatusOK, "application/json", raw)
	}
}

// MCPMethodNotAllowed answers the transport's other verbs. GET opens the
// server-to-client notification stream and DELETE ends a session; this server
// sends no notifications and keeps no session, so both are declined.
func MCPMethodNotAllowed(c *gin.Context) {
	c.Header("Allow", "POST")
	util.Error(c, http.StatusMethodNotAllowed, "MCP_METHOD_NOT_ALLOWED", "Only POST is supported on this endpoint", nil)
}

// mcpOriginAllowed guards against DNS rebinding: a browser can be steered into
// posting to a server it should not reach, and the MCP token would ride along.
// Non-browser clients send no Origin at all and are unaffected.
func (h *Handler) mcpOriginAllowed(origin string) bool {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return true
	}
	for _, allowed := range h.Config.Server.CORSOrigins {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(allowed), "/"), origin) {
			return true
		}
	}
	return h.Config.Server.PublicURL != "" && strings.EqualFold(h.Config.Server.PublicURL, origin)
}

// mcpAcceptsJSON reports whether the client will take a JSON response. A
// client following the transport spec offers both application/json and
// text/event-stream; one that sends nothing is assumed to take anything.
func mcpAcceptsJSON(accept string) bool {
	if strings.TrimSpace(accept) == "" {
		return true
	}
	for _, part := range strings.Split(accept, ",") {
		media := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if media == "application/json" || media == "application/*" || media == "*/*" {
			return true
		}
	}
	return false
}
