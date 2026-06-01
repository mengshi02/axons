// Package api provides HTTP API handlers for the axons daemon.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/i18n"
)

// LLMModel represents a single LLM model configuration.
type LLMModel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	BaseURL    string `json:"base_url"`
	Multimodal bool   `json:"multimodal"`
}

const llmModelsKey = "llm_models"

// handleGetLLMModels handles GET /api/llm-models - returns all LLM model configs.
func (s *Server) handleGetLLMModels(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	models, err := s.loadLLMModels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetLLMModels"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"models": models})
}

// handleCreateLLMModel handles POST /api/llm-models - creates a new LLM model config.
func (s *Server) handleCreateLLMModel(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var m LLMModel
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	m.ID = fmt.Sprintf("%d%04d", time.Now().UnixMilli(), rand.Intn(10000))
	models, err := s.loadLLMModels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedLoadLLMModels"))
		return
	}
	models = append(models, m)
	if err := s.saveLLMModels(models); err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedSaveLLMModels"))
		return
	}
	if err := s.syncFirstModelToLLMSettings(models); err != nil {
		log.Printf("warn: syncFirstModelToLLMSettings: %v", err)
	}
	s.eventBroker.BroadcastConfigChange("llm_models", "LLM model created")
	writeJSON(w, http.StatusOK, map[string]interface{}{"model": m})
}

// handleUpdateLLMModel handles PUT /api/llm-models/:id - updates an LLM model config.
func (s *Server) handleUpdateLLMModel(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	var updated LLMModel
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	models, err := s.loadLLMModels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedLoadLLMModels"))
		return
	}
	found := false
	for i, m := range models {
		if m.ID == id {
			updated.ID = id
			models[i] = updated
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.llmModelNotFound"))
		return
	}
	if err := s.saveLLMModels(models); err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedSaveLLMModels"))
		return
	}
	if err := s.syncFirstModelToLLMSettings(models); err != nil {
		log.Printf("warn: syncFirstModelToLLMSettings: %v", err)
	}
	s.eventBroker.BroadcastConfigChange("llm_models", "LLM model updated")
	writeJSON(w, http.StatusOK, map[string]interface{}{"model": updated})
}

// handleDeleteLLMModel handles DELETE /api/llm-models/:id - deletes an LLM model config.
func (s *Server) handleDeleteLLMModel(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.ByName("id")
	models, err := s.loadLLMModels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedLoadLLMModels"))
		return
	}
	newModels := models[:0]
	for _, m := range models {
		if m.ID != id {
			newModels = append(newModels, m)
		}
	}
	if err := s.saveLLMModels(newModels); err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedSaveLLMModels"))
		return
	}
	if err := s.syncFirstModelToLLMSettings(newModels); err != nil {
		log.Printf("warn: syncFirstModelToLLMSettings: %v", err)
	}
	s.eventBroker.BroadcastConfigChange("llm_models", "LLM model deleted")
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "deleted"})
}

// loadLLMModels loads LLM model configs from the settings table.
func (s *Server) loadLLMModels() ([]LLMModel, error) {
	val, err := s.repo.GetSetting(llmModelsKey)
	if err != nil {
		return nil, err
	}
	if val == "" {
		return []LLMModel{}, nil
	}
	var models []LLMModel
	if err := json.Unmarshal([]byte(val), &models); err != nil {
		return []LLMModel{}, nil
	}
	return models, nil
}

// saveLLMModels saves LLM model configs to the settings table.
func (s *Server) saveLLMModels(models []LLMModel) error {
	data, err := json.Marshal(models)
	if err != nil {
		return err
	}
	return s.repo.SetSettingWithMeta(llmModelsKey, string(data), "llm", "LLM model list")
}

// syncFirstModelToLLMSettings syncs the first model's configuration to the legacy
// llm_* settings fields so that ReinitAgentFromDB can pick them up.
// If models is empty, llm_enabled is set to "false".
func (s *Server) syncFirstModelToLLMSettings(models []LLMModel) error {
	if len(models) == 0 {
		return s.repo.SetSettingWithMeta("llm_enabled", "false", "llm", "LLM enabled")
	}
	m := models[0]
	settings := map[string]string{
		"llm_enabled":  "true",
		"llm_provider": m.Provider,
		"llm_model":    m.Model,
		"llm_api_key":  m.APIKey,
		"llm_base_url": m.BaseURL,
	}
	if err := s.repo.UpdateSettingsByCategory("llm", settings); err != nil {
		return err
	}
	return s.ReinitAgentFromDB()
}

// Settings API handlers

// handleGetSettings handles GET /v1/settings - returns all settings.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	settings, err := s.repo.GetAllSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetSettings"))
		return
	}

	// Group settings by category
	grouped := make(map[string]map[string]interface{})
	for key, setting := range settings {
		if grouped[setting.Category] == nil {
			grouped[setting.Category] = make(map[string]interface{})
		}
		grouped[setting.Category][key] = map[string]interface{}{
			"value":       setting.Value,
			"description": setting.Description,
			"updated_at":  setting.UpdatedAt,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"settings": grouped,
	})
}

// handleGetSettingsByCategory handles GET /v1/config/:category - returns settings by category.
// Also handles special categories: embedding, llm, rerank, rag with masked API keys.
func (s *Server) handleGetSettingsByCategory(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	category := ps.ByName("category")

	settings, err := s.repo.GetSettingsByCategory(category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetSettings"))
		return
	}

	// Mask sensitive information based on category
	switch category {
	case "embedding":
		if apiKey, ok := settings["embedding_api_key"]; ok && len(apiKey) > 4 {
			settings["embedding_api_key"] = apiKey[:4] + "****"
		}
	case "llm":
		if apiKey, ok := settings["llm_api_key"]; ok && len(apiKey) > 4 {
			settings["llm_api_key"] = apiKey[:4] + "****"
		}
	case "rerank":
		if apiKey, ok := settings["rerank_api_key"]; ok && len(apiKey) > 4 {
			settings["rerank_api_key"] = apiKey[:4] + "****"
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"category": category,
		"settings": settings,
	})
}

// SettingsUpdateRequest represents a settings update request.
type SettingsUpdateRequest struct {
	Category string            `json:"category"`
	Settings map[string]string `json:"settings"`
}

// handleUpdateSettings handles PUT /v1/settings - updates settings.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req SettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if len(req.Settings) == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.noSettingsProvided"))
		return
	}

	// If category is provided, use category-specific update
	if req.Category != "" {
		if err := s.repo.UpdateSettingsByCategory(req.Category, req.Settings); err != nil {
			writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedUpdateSettings"))
			return
		}
	} else {
		// Otherwise, use general update
		if err := s.repo.UpdateSettings(req.Settings); err != nil {
			writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedUpdateSettings"))
			return
		}
	}

	// Check if embedding configuration changed
	if _, ok := req.Settings["embedding_provider"]; ok {
		// Broadcast embedding provider change event
		s.eventBroker.BroadcastConfigChange("embedding", "Embedding provider changed, re-embedding may be required")
	}

	// Check if LLM configuration changed; reinitialize agent service if so
	llmChanged := req.Category == "llm"
	if !llmChanged {
		for k := range req.Settings {
			if len(k) >= 4 && k[:4] == "llm_" {
				llmChanged = true
				break
			}
		}
	}
	if llmChanged {
		// If llm_enabled is being turned on, sync first model's fields first
		if v, ok := req.Settings["llm_enabled"]; ok && v == "true" {
			if models, err := s.loadLLMModels(); err == nil && len(models) > 0 {
				m := models[0]
				extra := map[string]string{
					"llm_enabled":  "true",
					"llm_provider": m.Provider,
					"llm_model":    m.Model,
					"llm_api_key":  m.APIKey,
					"llm_base_url": m.BaseURL,
				}
				if syncErr := s.repo.UpdateSettingsByCategory("llm", extra); syncErr != nil {
					log.Printf("[Settings] Failed to sync first model settings: %v", syncErr)
				}
			}
		}
		if err := s.ReinitAgentFromDB(); err != nil {
			log.Printf("[Settings] Failed to reinit agent after LLM config update: %v", err)
		}
	}

	// Check if locale setting changed; sync i18n locale
	if req.Category == "locale" {
		if localeVal, ok := req.Settings["locale"]; ok {
			i18n.SetLocale(localeVal)
			log.Printf("[Settings] Locale updated to: %s", localeVal)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "success",
		"updated": len(req.Settings),
	})
}

// handleGetSetting handles GET /v1/settings/key/:key - returns a single setting.
func (s *Server) handleGetSetting(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	key := ps.ByName("key")

	setting, err := s.repo.GetSettingWithMeta(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetSetting"))
		return
	}

	if setting == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.settingNotFound"))
		return
	}

	writeJSON(w, http.StatusOK, setting)
}

// handleSetSetting handles PUT /v1/settings/key/:key - sets a single setting.
func (s *Server) handleSetSetting(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	key := ps.ByName("key")

	var req struct {
		Value       string `json:"value"`
		Category    string `json:"category,omitempty"`
		Description string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	var err error
	if req.Category != "" || req.Description != "" {
		err = s.repo.SetSettingWithMeta(key, req.Value, req.Category, req.Description)
	} else {
		err = s.repo.SetSetting(key, req.Value)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedSetSetting"))
		return
	}

	// Check if this is an embedding setting change
	if key == "embedding_provider" || key == "embedding_api_key" || key == "embedding_model" {
		s.eventBroker.BroadcastConfigChange("embedding", "Embedding configuration changed: "+key)
	}

	// Check if this is a LLM setting change; reinitialize agent service if so
	if len(key) >= 4 && key[:4] == "llm_" {
		if reinitErr := s.ReinitAgentFromDB(); reinitErr != nil {
			log.Printf("[Settings] Failed to reinit agent after LLM key update (%s): %v", key, reinitErr)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
		"key":    key,
		"value":  req.Value,
	})
}

// handleDeleteSetting handles DELETE /v1/settings/key/:key - deletes a setting.
func (s *Server) handleDeleteSetting(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	key := ps.ByName("key")

	if err := s.repo.DeleteSetting(key); err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedDeleteSetting"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "deleted",
		"key":    key,
	})
}

// handleCheckEmbeddingConfig handles GET /v1/settings/embedding/check - checks embedding configuration status.
func (s *Server) handleCheckEmbeddingConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	configured, err := s.repo.IsEmbeddingConfigured()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedCheckEmbedConfig"))
		return
	}

	config, err := s.repo.GetEmbeddingConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetEmbedConfig"))
		return
	}

	// Mask sensitive information
	if apiKey, ok := config["embedding_api_key"]; ok && len(apiKey) > 4 {
		config["embedding_api_key"] = apiKey[:4] + "****"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"configured": configured,
		"config":     config,
	})
}

// handleGetEmbeddingConfig handles GET /v1/settings/embedding - returns embedding configuration.
func (s *Server) handleGetEmbeddingConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	config, err := s.repo.GetEmbeddingConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetEmbedConfig"))
		return
	}

	// Mask sensitive information
	if apiKey, ok := config["embedding_api_key"]; ok && len(apiKey) > 4 {
		config["embedding_api_key"] = apiKey[:4] + "****"
	}

	writeJSON(w, http.StatusOK, config)
}

// handleGetLLMConfig handles GET /v1/settings/llm - returns LLM configuration.
func (s *Server) handleGetLLMConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	config, err := s.repo.GetLLMConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetLLMConfig"))
		return
	}

	// Mask sensitive information
	if apiKey, ok := config["llm_api_key"]; ok && len(apiKey) > 4 {
		config["llm_api_key"] = apiKey[:4] + "****"
	}

	writeJSON(w, http.StatusOK, config)
}

// handleGetRerankConfig handles GET /v1/settings/rerank - returns rerank configuration.
func (s *Server) handleGetRerankConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	config, err := s.repo.GetRerankConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetRerankConfig"))
		return
	}

	// Mask sensitive information
	if apiKey, ok := config["rerank_api_key"]; ok && len(apiKey) > 4 {
		config["rerank_api_key"] = apiKey[:4] + "****"
	}

	writeJSON(w, http.StatusOK, config)
}

// handleGetRAGConfig handles GET /v1/settings/rag - returns RAG configuration.
func (s *Server) handleGetRAGConfig(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	config, err := s.repo.GetRAGConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", i18n.T("api.error.failedGetRAGConfig"))
		return
	}

	writeJSON(w, http.StatusOK, config)
}

// TestConnectionRequest represents a connection test request.
type TestConnectionRequest struct {
	Type    string `json:"type"`    // "embedding", "llm", "rerank"
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
}

// handleTestConnection handles POST /v1/settings/test-connection.
// Proxies a minimal request to the target endpoint from the server side to avoid CORS.
func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req TestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.baseUrlRequired"))
		return
	}

	baseURL := strings.TrimRight(req.BaseURL, "/")
	model := req.Model
	if model == "" {
		model = "test"
	}

	var (
		targetURL string
		body      []byte
	)

	switch req.Type {
	case "llm":
		targetURL = baseURL + "/chat/completions"
		body, _ = json.Marshal(map[string]interface{}{
			"model":      model,
			"messages":   []map[string]string{{"role": "user", "content": "hi"}},
			"max_tokens": 10,
			"stream":     false,
		})
	case "rerank":
		targetURL = baseURL + "/rerank"
		body, _ = json.Marshal(map[string]interface{}{
			"model":     model,
			"query":     "test",
			"documents": []string{"hello world"},
			"top_n":     1,
		})
	default: // "embedding"
		targetURL = baseURL + "/embeddings"
		body, _ = json.Marshal(map[string]interface{}{
			"model": model,
			"input": []string{"test"},
		})
	}

	// Use an independent context so that disconnecting the frontend does not cancel the backend request.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      false,
			"message": fmt.Sprintf("Failed to build request: %v", err),
		})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}

	log.Printf("[TestConnection] type=%s url=%s", req.Type, targetURL)
	start := time.Now()

	// 使用自定义 Transport：禁用系统代理、设置 Dial 超时、禁用 keepalive
	transport := &http.Transport{
		Proxy:               nil, // 完全不走代理
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 0,
			DualStack: false,
		}).DialContext,
	}
	client := &http.Client{Transport: transport}
	resp, err := client.Do(httpReq)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("[TestConnection] FAILED type=%s url=%s elapsed=%v err=%v", req.Type, targetURL, elapsed, err)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      false,
			"message": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	log.Printf("[TestConnection] type=%s url=%s status=%d elapsed=%v", req.Type, targetURL, resp.StatusCode, elapsed)

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	log.Printf("[TestConnection] type=%s url=%s status=%d body=%s", req.Type, targetURL, resp.StatusCode, string(respBody))

	// 服务可达的状态码：2xx / 400 / 401 / 403 / 404 / 405 / 422 均视为连通
	// 401/403 说明服务在线但鉴权失败；400/422 说明参数问题；404/405 说明端点路径差异
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": "Connection successful",
		})
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": "Service reachable (authentication required - check your API key)",
		})
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": "Service reachable",
		})
	default:
		var errMsg struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if json.Unmarshal(respBody, &errMsg) == nil && errMsg.Error.Message != "" {
			msg = errMsg.Error.Message
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      false,
			"message": msg,
		})
	}
}
