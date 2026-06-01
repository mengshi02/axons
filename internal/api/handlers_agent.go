package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/agent"
	"github.com/mengshi02/axons/internal/agent/llm"
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/logger"
)

// buildAgentFromProfile dynamically constructs an Agent instance from a profile.
// mcpOverride optionally replaces the default mcpServer (e.g. scoped to a project DB).
func (s *Server) buildAgentFromProfile(profile agent.AgentProfile, mcpOverride ...*MCPServer) agent.Agent {
	// Get underlying LLM client and Memory from initialized agentService
	base, ok := s.agentService.(*agent.ReActAgent)
	if !ok || base == nil {
		return s.agentService
	}

	mcp := s.mcpServer
	if len(mcpOverride) > 0 && mcpOverride[0] != nil {
		mcp = mcpOverride[0]
	}

	tools := agent.WrapToolsWithFilter(mcp, profile.Tools)

	// Orchestrator (default) injects DelegateAgentTool
	if profile.ID == "default" {
		tools["delegate_to_agent"] = agent.NewDelegateAgentTool(s)
	}

	return agent.NewReActAgent(&agent.AgentOptions{
		LLM:          base.LLMClient(),
		Memory:       s.agentMemory,
		Tools:        tools,
		MaxRounds:    base.MaxRounds(),
		SystemPrompt: profile.SystemPrompt,
	})
}

// buildAgentWithModel constructs an Agent instance with a specific model.
func (s *Server) buildAgentWithModel(profile agent.AgentProfile, modelID string, mcpOverride ...*MCPServer) (agent.Agent, error) {
	mcp := s.mcpServer
	if len(mcpOverride) > 0 && mcpOverride[0] != nil {
		mcp = mcpOverride[0]
	}

	tools := agent.WrapToolsWithFilter(mcp, profile.Tools)

	// Orchestrator (default) injects DelegateAgentTool
	if profile.ID == "default" {
		tools["delegate_to_agent"] = agent.NewDelegateAgentTool(s)
	}

	// Create LLM client based on modelID
	llmClient, err := s.createLLMClientForModel(modelID)
	if err != nil {
		return nil, err
	}

	// Get memory
	var memory agent.Memory
	if s.agentMemory != nil {
		memory = s.agentMemory
	} else {
		memory, err = agent.NewSQLiteMemory(s.db)
		if err != nil {
			return nil, fmt.Errorf("failed to create agent memory: %w", err)
		}
	}

	// Get maxRounds
	maxRounds := 30
	if base, ok := s.agentService.(*agent.ReActAgent); ok && base != nil {
		maxRounds = base.MaxRounds()
	}

	return agent.NewReActAgent(&agent.AgentOptions{
		LLM:          llmClient,
		Memory:       memory,
		Tools:        tools,
		MaxRounds:    maxRounds,
		SystemPrompt: profile.SystemPrompt,
	}), nil
}

// ListAgentProfiles implements AgentRegistry interface, returns all available agents (built-in + custom)
func (s *Server) ListAgentProfiles() []agent.AgentProfile {
	result := make([]agent.AgentProfile, 0, len(agent.GetBuiltinProfiles()))
	result = append(result, agent.GetBuiltinProfiles()...)

	rows, err := s.repo.ListAgentProfiles()
	if err != nil {
		return result
	}
	for _, row := range rows {
		result = append(result, agent.AgentProfile{
			ID:           row.ID,
			Name:         row.Name,
			Description:  row.Description,
			Icon:         row.Icon,
			Tools:        row.Tools,
			SystemPrompt: row.SystemPrompt,
			IsBuiltin:    false,
			AllowWrite:   row.AllowWrite,
		})
	}
	return result
}

// BuildAgent implements AgentRegistry interface, builds an executable agent instance by ID.
// Options can specify projectID for scoping and modelID for custom LLM.
func (s *Server) BuildAgent(agentID string, opts ...agent.BuildAgentOption) agent.Agent {
	// Parse options
	var o agent.BuildAgentOptions
	for _, opt := range opts {
		opt(&o)
	}
	
	profile := s.resolveAgentProfile(agentID)
	
	// Build with model if specified
	if o.ModelID != "" {
		var mcpOverride *MCPServer
		if o.ProjectID != "" {
			if project, err := s.globalRepo.GetProject(o.ProjectID); err == nil && project != nil {
				if pRepo, err := s.projectRepo(o.ProjectID); err == nil {
					mcpOverride = s.mcpServer.WithRepo(pRepo, project.RootPath)
				}
			}
		}
		ag, err := s.buildAgentWithModel(profile, o.ModelID, mcpOverride)
		if err != nil {
			logger.S().Errorw("[BuildAgent] Failed to build agent with model",
				"agent_id", agentID,
				"model_id", o.ModelID,
				"error", err)
			// Fallback to default build
			return s.buildAgentFromProfile(profile)
		}
		return ag
	}
	
	// If projectID is provided, build project-scoped MCP context
	if o.ProjectID != "" {
		if project, err := s.globalRepo.GetProject(o.ProjectID); err == nil && project != nil {
			if pRepo, err := s.projectRepo(o.ProjectID); err == nil {
				scopedMCP := s.mcpServer.WithRepo(pRepo, project.RootPath)
				return s.buildAgentFromProfile(profile, scopedMCP)
			}
		}
	}
	
	return s.buildAgentFromProfile(profile)
}

// createLLMClientForModel creates an LLM client for a specific model ID.
func (s *Server) createLLMClientForModel(modelID string) (llm.Client, error) {
	models, err := s.loadLLMModels()
	if err != nil {
		return nil, fmt.Errorf("failed to load LLM models: %w", err)
	}

	var targetModel *LLMModel
	for i := range models {
		if models[i].ID == modelID {
			targetModel = &models[i]
			break
		}
	}

	if targetModel == nil {
		return nil, fmt.Errorf("model %q not found", modelID)
	}

	// Build LLM client based on provider
	switch targetModel.Provider {
	case "openai", "":
		if targetModel.BaseURL != "" {
			return llm.NewOpenAIClientWithBaseURL(targetModel.APIKey, targetModel.Model, targetModel.BaseURL), nil
		}
		return llm.NewOpenAIClient(targetModel.APIKey, targetModel.Model), nil
	case "anthropic":
		if targetModel.BaseURL != "" {
			return llm.NewAnthropicClientWithBaseURL(targetModel.APIKey, targetModel.Model, targetModel.BaseURL), nil
		}
		return llm.NewAnthropicClient(targetModel.APIKey, targetModel.Model), nil
	case "custom":
		if targetModel.BaseURL == "" {
			return nil, fmt.Errorf("base_url is required for custom provider")
		}
		return llm.NewOpenAIClientWithBaseURL(targetModel.APIKey, targetModel.Model, targetModel.BaseURL), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", targetModel.Provider)
	}
}


// agentProfileResponse is the unified response shape for agent profiles
type agentProfileResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	Tools       []string  `json:"tools"`
	SystemPrompt string   `json:"system_prompt"`
	IsBuiltin   bool      `json:"is_builtin"`
	AllowWrite  bool      `json:"allow_write"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

// handleListAgents GET /api/agents - 列出所有 Agent 角色（内置 + 自定义）
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var profiles []agentProfileResponse

	// 内置角色
	for _, p := range agent.GetBuiltinProfiles() {
		profiles = append(profiles, agentProfileResponse{
			ID:           p.ID,
			Name:         p.Name,
			Description:  p.Description,
			Icon:         p.Icon,
			Tools:        p.Tools,
			SystemPrompt: p.SystemPrompt,
			IsBuiltin:    true,
			AllowWrite:   p.AllowWrite,
		})
	}

	// 自定义角色
	rows, err := s.repo.ListAgentProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to list agent profiles: %v", err))
		return
	}
	for _, row := range rows {
		profiles = append(profiles, agentProfileResponse{
			ID:           row.ID,
			Name:         row.Name,
			Description:  row.Description,
			Icon:         row.Icon,
			Tools:        row.Tools,
			SystemPrompt: row.SystemPrompt,
			IsBuiltin:    false,
			AllowWrite:   row.AllowWrite,
			CreatedAt:    row.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agents": profiles,
		"count":  len(profiles),
	})
}

// handleGetAgent GET /api/agents/:id - 获取单个 Agent 角色
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")

	// 先查内置
	if p, ok := agent.GetBuiltinProfile(id); ok {
		writeJSON(w, http.StatusOK, agentProfileResponse{
			ID: p.ID, Name: p.Name, Description: p.Description,
			Icon: p.Icon, Tools: p.Tools, SystemPrompt: p.SystemPrompt,
			IsBuiltin: true, AllowWrite: p.AllowWrite,
		})
		return
	}

	// 再查自定义
	row, err := s.repo.GetAgentProfile(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.agentNotFound"))
		return
	}
	writeJSON(w, http.StatusOK, agentProfileResponse{
		ID: row.ID, Name: row.Name, Description: row.Description,
		Icon: row.Icon, Tools: row.Tools, SystemPrompt: row.SystemPrompt,
		IsBuiltin: false, AllowWrite: row.AllowWrite, CreatedAt: row.CreatedAt,
	})
}

// handleCreateAgent POST /api/agents - 创建自定义 Agent 角色
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		Icon         string   `json:"icon"`
		Tools        []string `json:"tools"`
		SystemPrompt string   `json:"system_prompt"`
		AllowWrite   bool     `json:"allow_write"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.nameRequired"))
		return
	}
	if req.Icon == "" {
		req.Icon = "bot"
	}
	if req.Tools == nil {
		req.Tools = []string{}
	}

	id := fmt.Sprintf("custom_%d", time.Now().UnixMilli())
	row := &repository.AgentProfileRow{
		ID:           id,
		Name:         req.Name,
		Description:  req.Description,
		Icon:         req.Icon,
		Tools:        req.Tools,
		SystemPrompt: req.SystemPrompt,
		AllowWrite:   req.AllowWrite,
	}
	if err := s.repo.CreateAgentProfile(row); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "created"})
}

// handleUpdateAgent PUT /api/agents/:id - 更新自定义 Agent 角色
func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if _, ok := agent.GetBuiltinProfile(id); ok {
		writeError(w, http.StatusForbidden, "FORBIDDEN", i18n.T("api.error.cannotModifyBuiltin"))
		return
	}

	var req struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		Icon         string   `json:"icon"`
		Tools        []string `json:"tools"`
		SystemPrompt string   `json:"system_prompt"`
		AllowWrite   bool     `json:"allow_write"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.nameRequired"))
		return
	}
	if req.Tools == nil {
		req.Tools = []string{}
	}

	row := &repository.AgentProfileRow{
		ID:           id,
		Name:         req.Name,
		Description:  req.Description,
		Icon:         req.Icon,
		Tools:        req.Tools,
		SystemPrompt: req.SystemPrompt,
		AllowWrite:   req.AllowWrite,
	}
	if err := s.repo.UpdateAgentProfile(row); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleDeleteAgent DELETE /api/agents/:id - 删除自定义 Agent 角色
func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	if _, ok := agent.GetBuiltinProfile(id); ok {
		writeError(w, http.StatusForbidden, "FORBIDDEN", i18n.T("api.error.cannotDeleteBuiltin"))
		return
	}
	if err := s.repo.DeleteAgentProfile(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleListAgentTools GET /api/agents/tools - 列出所有可用工具
func (s *Server) handleListAgentTools(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	tools := make([]toolInfo, 0, len(agent.DefaultMCPToolDefinitions))
	for _, def := range agent.DefaultMCPToolDefinitions {
		tools = append(tools, toolInfo{Name: def.Name, Description: def.Description})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tools": tools,
		"count": len(tools),
	})
}