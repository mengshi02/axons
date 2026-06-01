/**
 * LLM Module
 * 
 * Provides backend Agent client for code analysis.
 */

// Types
export type {
  LLMProvider,
  LLMSettings,
  ProviderConfig,
  OpenAIConfig,
  AnthropicConfig,
  GeminiConfig,
  OllamaConfig,
  OpenRouterConfig,
  AgentStreamChunk,
} from './types';

export {
  getDefaultSettings,
  AVAILABLE_MODELS,
  PROVIDER_LABELS,
  requiresApiKey,
} from './types';

// Agent (backend client)
export {
  chatWithAgent,
  chatSimple,
  clearSession,
  checkAgentAvailability,
} from './agent';