package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/agent"
	"github.com/mengshi02/axons/internal/cce"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/logger"
)

// resolveAgentProfile resolves an AgentProfile by agent_id: built-in first, then custom from DB.
func (s *Server) resolveAgentProfile(agentID string) agent.AgentProfile {
	if agentID == "" {
		agentID = "default"
	}
	// 内置角色优先
	if p, ok := agent.GetBuiltinProfile(agentID); ok {
		return *p
	}
	// 查数据库自定义角色
	row, err := s.repo.GetAgentProfile(agentID)
	if err == nil && row != nil {
		return agent.AgentProfile{
			ID:           row.ID,
			Name:         row.Name,
			Description:  row.Description,
			Icon:         row.Icon,
			Tools:        row.Tools,
			SystemPrompt: row.SystemPrompt,
			IsBuiltin:    false,
			AllowWrite:   row.AllowWrite,
		}
	}
	// 兜底使用 default
	p, _ := agent.GetBuiltinProfile("default")
	return *p
}

// injectSubAgentList 为 Orchestrator (default) 动态注入当前可用的 sub-agent 列表到 system prompt
func (s *Server) injectSubAgentList(profile agent.AgentProfile) agent.AgentProfile {
	if profile.ID != "default" {
		return profile
	}
	profiles := s.ListAgentProfiles()
	var sb strings.Builder
	sb.WriteString("## Available Sub-Agents\n")
	for _, p := range profiles {
		if p.ID == "default" {
			continue
		}
		sb.WriteString(fmt.Sprintf("- `%s` (%s): %s\n", p.ID, p.Name, p.Description))
	}
	sb.WriteString("\n")
	profile.SystemPrompt = sb.String() + profile.SystemPrompt
	return profile
}

// handleChatStream POST /api/chat/stream - SSE streaming chat
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		SessionID string   `json:"session_id"`
		Message   string   `json:"message"`
		Context   string   `json:"context"`
		AgentID   string   `json:"agent_id"`
		ProjectID string   `json:"project_id"`
		ModelID   string   `json:"model_id"` // model ID selected from frontend
		Images    []string `json:"images"`   // base64 dataUrl list for multimodal
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	logger.S().Infow("[handleChatStream] Request received",
		"session_id", req.SessionID,
		"agent_id", req.AgentID,
		"project_id", req.ProjectID,
		"model_id", req.ModelID,
		"message_length", len(req.Message),
		"has_images", len(req.Images) > 0)

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.messageRequired"))
		return
	}

	if req.SessionID == "" {
		req.SessionID = "default"
	}

	// Check if Agent service is initialized
	if s.agentService == nil {
		logger.S().Errorw("[handleChatStream] Agent service not configured")
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", i18n.T("api.error.agentNotConfigured"))
		return
	}

	// Resolve Agent Profile, build tool set and prompt on demand
	logger.S().Debugw("[handleChatStream] Resolving agent profile", "agent_id", req.AgentID)
	profile := s.resolveAgentProfile(req.AgentID)

	// Orchestrator: dynamically inject current available sub-agent list
	profile = s.injectSubAgentList(profile)
	logger.S().Debugw("[handleChatStream] Agent profile resolved", "profile_id", profile.ID, "profile_name", profile.Name)

	// Inject current project context into System Prompt
	if req.ProjectID != "" {
		if project, err := s.globalRepo.GetProject(req.ProjectID); err == nil && project != nil {
			projectCtx := fmt.Sprintf("## Current Project\nName: %s\nRoot: %s\n\n", project.Name, project.RootPath)
			profile.SystemPrompt = projectCtx + profile.SystemPrompt
		}
	}

	// Switch MCP repo to project-specific database based on project_id
	var scopedMCP *MCPServer
	if req.ProjectID != "" {
		if project, err := s.globalRepo.GetProject(req.ProjectID); err == nil && project != nil {
			if pRepo, err := s.projectRepo(req.ProjectID); err == nil {
				scopedMCP = s.mcpServer.WithRepo(pRepo, project.RootPath)
			}
		}
	}

	// CCE: Inject Cognitive Context Engine context if available
	var cceBanner string
	if req.Message != "" && req.ProjectID != "" {
		if pRepo, err := s.projectRepo(req.ProjectID); err == nil {
			if cceEmbedder := s.createEmbedder(); cceEmbedder != nil {
				// Get project root path for source code resolution
				var projectRootPath string
				if project, pErr := s.globalRepo.GetProject(req.ProjectID); pErr == nil && project != nil {
					projectRootPath = project.RootPath
				}
				cceEngine := cce.NewEngine(pRepo, cceEmbedder, projectRootPath)
				contextText, banner, err := cceEngine.GetContextForChat(r.Context(), req.Message, req.ProjectID)
				if err != nil {
					logger.S().Warnw("[handleChatStream] CCE context retrieval failed", "error", err)
				} else if contextText != "" {
					// Prepend CCE context to the request context
					cceSection := fmt.Sprintf("## Code Context (powered by axons-cce)\n%s\n", contextText)
					if req.Context != "" {
						req.Context = cceSection + "\n" + req.Context
					} else {
						req.Context = cceSection
					}
					cceBanner = banner
					logger.S().Infow("[handleChatStream] CCE context injected",
						"banner", banner,
						"project_id", req.ProjectID)
				}
			}
		}
	}

	// Build Agent instance
	logger.S().Debugw("[handleChatStream] Building agent instance",
		"model_id", req.ModelID,
		"has_scoped_mcp", scopedMCP != nil)
	var agentInstance agent.Agent
	var agentErr error
	if req.ModelID != "" && scopedMCP != nil {
		agentInstance, agentErr = s.buildAgentWithModel(profile, req.ModelID, scopedMCP)
	} else if req.ModelID != "" {
		agentInstance, agentErr = s.buildAgentWithModel(profile, req.ModelID)
	} else if scopedMCP != nil {
		agentInstance = s.buildAgentFromProfile(profile, scopedMCP)
	} else {
		agentInstance = s.buildAgentFromProfile(profile)
	}
	
	if agentErr != nil {
		logger.S().Errorw("[handleChatStream] Failed to build agent", "error", agentErr)
		writeError(w, http.StatusInternalServerError, "AGENT_ERROR", fmt.Sprintf("Failed to build agent: %v", agentErr))
		return
	}
	logger.S().Infow("[handleChatStream] Agent instance built successfully")

	// Set SSE response headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.streamingNotSupported"))
		return
	}

	// Execute Agent - inject project ID, model ID, session ID, and agent ID into context
	ctx := r.Context()
	if req.ProjectID != "" {
		ctx = context.WithValue(ctx, agent.ProjectIDKey, req.ProjectID)
	}
	if req.ModelID != "" {
		ctx = context.WithValue(ctx, agent.ModelIDKey, req.ModelID)
	}
	// Inject session ID for sub-agent delegation tracking
	ctx = context.WithValue(ctx, agent.SessionIDKey, req.SessionID)
	// Inject agent ID for memory isolation
	agentID := req.AgentID
	if agentID == "" {
		agentID = "default"
	}
	ctx = context.WithValue(ctx, agent.AgentIDKey, agentID)

	// Monitor context cancellation in a separate goroutine
	go func() {
		<-ctx.Done()
		logger.S().Warnw("[handleChatStream] Context canceled",
			"session_id", req.SessionID,
			"agent_id", req.AgentID,
			"error", ctx.Err())
	}()

	// Use recover to catch panics and ensure proper SSE closure
	defer func() {
		if rec := recover(); rec != nil {
			logger.S().Errorw("[handleChatStream] PANIC recovered",
				"error", rec,
				"session_id", req.SessionID,
				"agent_id", req.AgentID,
				"stack", string(debug.Stack()))
			fmt.Fprintf(w, "data: {\"type\":\"error\",\"error\":\"Internal server error\"}\n\n")
			flusher.Flush()
		}
		logger.S().Infow("[handleChatStream] Stream handler exited", "session_id", req.SessionID)
	}()

	logger.S().Infow("[handleChatStream] Starting agent run",
		"session_id", req.SessionID,
		"message_preview", truncateMessage(req.Message, 100))

	// Send CCE banner as an SSE event before the agent stream starts
	if cceBanner != "" {
		bannerData, _ := json.Marshal(map[string]string{
			"type":    "cce_banner",
			"content": cceBanner,
		})
		fmt.Fprintf(w, "data: %s\n\n", bannerData)
		flusher.Flush()
	}

	eventChan := agentInstance.Run(ctx, &agent.RunRequest{
		SessionID: req.SessionID,
		Message:   req.Message,
		Context:   req.Context,
		Images:    req.Images,
	})

	// Stream events with heartbeat to prevent timeout during long operations
	eventCount := 0
	heartbeatInterval := 10 * time.Second // Send heartbeat every 10s
	heartbeatTimer := time.NewTimer(heartbeatInterval)
	defer heartbeatTimer.Stop()
	
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				// Channel closed, exit loop
				goto done
			}
			// Reset heartbeat timer on receiving real event
			heartbeatTimer.Reset(heartbeatInterval)
			
			eventCount++
			logger.S().Debugw("[handleChatStream] Received event",
				"event_type", event.Type,
				"event_count", eventCount,
				"session_id", req.SessionID)
			
			data, err := json.Marshal(event)
			if err != nil {
				logger.S().Errorw("[handleChatStream] Failed to marshal event",
					"error", err,
					"event_type", event.Type,
					"event_count", eventCount)
				fmt.Fprintf(w, "data: {\"type\":\"error\",\"error\":\"%s\"}\n\n", err.Error())
				flusher.Flush()
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			
		case <-heartbeatTimer.C:
			// Send heartbeat to keep connection alive
			logger.S().Debugw("[handleChatStream] Sending heartbeat",
				"session_id", req.SessionID,
				"event_count", eventCount)
			fmt.Fprintf(w, "data: {\"type\":\"heartbeat\"}\n\n")
			flusher.Flush()
			heartbeatTimer.Reset(heartbeatInterval)
			
		case <-ctx.Done():
			// Context canceled, exit loop
			logger.S().Warnw("[handleChatStream] Context canceled during event streaming",
				"session_id", req.SessionID,
				"error", ctx.Err())
			goto done
		}
	}
done:

	// Send done marker
	logger.S().Infow("[handleChatStream] Event stream closed, sending [DONE]",
		"total_events", eventCount,
		"session_id", req.SessionID)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleChat POST /api/chat - non-streaming chat
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
		Context   string `json:"context"`
		AgentID   string `json:"agent_id"`
		ProjectID string `json:"project_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.messageRequired"))
		return
	}

	if req.SessionID == "" {
		req.SessionID = "default"
	}

	// 检查 Agent 是否已初始化
	if s.agentService == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", i18n.T("api.error.agentNotConfigured"))
		return
	}

	profile := s.resolveAgentProfile(req.AgentID)

	// Orchestrator: 动态注入当前可用 sub-agent 列表
	profile = s.injectSubAgentList(profile)

	// 注入当前项目上下文到 System Prompt
	if req.ProjectID != "" {
		if project, err := s.globalRepo.GetProject(req.ProjectID); err == nil && project != nil {
			projectCtx := fmt.Sprintf("## Current Project\nName: %s\nRoot: %s\n\n", project.Name, project.RootPath)
			profile.SystemPrompt = projectCtx + profile.SystemPrompt
		}
	}

	// 按 project_id 切换 MCP repo 到对应的项目数据库
	var scopedMCPNS *MCPServer
	if req.ProjectID != "" {
		if project, err := s.globalRepo.GetProject(req.ProjectID); err == nil && project != nil {
			if pRepo, err := s.projectRepo(req.ProjectID); err == nil {
				scopedMCPNS = s.mcpServer.WithRepo(pRepo, project.RootPath)
			}
		}
	}

	agentInstance := s.buildAgentFromProfile(profile, scopedMCPNS)

	eventChan := agentInstance.Run(r.Context(), &agent.RunRequest{
		SessionID: req.SessionID,
		Message:   req.Message,
		Context:   req.Context,
	})

	var response struct {
		Content   string   `json:"content"`
		ToolCalls []string `json:"tool_calls,omitempty"`
		Error     string   `json:"error,omitempty"`
	}

	for event := range eventChan {
		switch event.Type {
		case "token":
			response.Content += event.Content
		case "tool_start":
			response.ToolCalls = append(response.ToolCalls, event.ToolName)
		case "error":
			response.Error = event.Error
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// truncateMessage truncates a message for logging purposes
func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen] + "..."
}

// handleChatClear POST /api/chat/clear - 清除会话历史
func (s *Server) handleChatClear(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		SessionID string `json:"session_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.SessionID == "" {
		req.SessionID = "default"
	}

	if s.agentService == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", i18n.T("api.error.agentNotConfigured"))
		return
	}

	if s.agentMemory != nil {
		if err := s.agentMemory.Clear(r.Context(), req.SessionID); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to clear session: %v", err))
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "cleared",
	})
}

// handleListSessions GET /api/chat/sessions - 列出会话列表（包含消息）
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	logger.S().Infow("[handleListSessions] Request received", "project_id", projectID)

	if s.agentMemory == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", i18n.T("api.error.agentMemoryNotConfigured"))
		return
	}

	memory, ok := s.agentMemory.(*agent.SQLiteMemory)
	if !ok {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.agentMemoryNoListSessions"))
		return
	}

	sessions, err := memory.ListSessions(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to list sessions: %v", err))
		return
	}

	logger.S().Infow("[handleListSessions] Found sessions", "count", len(sessions))

	// Load messages for each session
	type SessionWithMessages struct {
		SessionID    string          `json:"session_id"`
		AgentID      string          `json:"agent_id"`
		MessageCount int             `json:"message_count"`
		CreatedAt    string          `json:"created_at"`
		UpdatedAt    string          `json:"updated_at"`
		Messages     []agent.Message `json:"messages"`
	}

	sessionsWithMessages := make([]SessionWithMessages, 0, len(sessions))
	for _, session := range sessions {
		messages, err := memory.GetSessionHistory(r.Context(), session.SessionID)
		if err != nil {
			// Log error but continue with empty messages
			logger.S().Warnw("[handleListSessions] Failed to get session history",
				"session_id", session.SessionID,
				"error", err)
			messages = []agent.Message{}
		}
		logger.S().Debugw("[handleListSessions] Session loaded",
			"session_id", session.SessionID,
			"agent_id", session.AgentID,
			"message_count", len(messages))
		sessionsWithMessages = append(sessionsWithMessages, SessionWithMessages{
			SessionID:    session.SessionID,
			AgentID:      session.AgentID,
			MessageCount: session.MessageCount,
			CreatedAt:    session.CreatedAt,
			UpdatedAt:    session.UpdatedAt,
			Messages:     messages,
		})
	}

	logger.S().Infow("[handleListSessions] Returning sessions", "count", len(sessionsWithMessages))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessionsWithMessages,
	})
}

// handleGetSessionHistory GET /api/chat/sessions/:id/history - 获取会话历史
func (s *Server) handleGetSessionHistory(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	sessionID := ps.ByName("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.sessionIdRequired"))
		return
	}

	if s.agentMemory == nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", i18n.T("api.error.agentMemoryNotConfigured"))
		return
	}

	memory, ok := s.agentMemory.(*agent.SQLiteMemory)
	if !ok {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.agentMemoryNoSessionHistory"))
		return
	}

	messages, err := memory.GetSessionHistory(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to get session history: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sessionID,
		"messages":   messages,
	})
}