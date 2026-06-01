/**
 * LLM Provider Types
 *
 * Type definitions for multi-provider LLM support.
 * Supports OpenAI, Anthropic, Google Gemini, Ollama, OpenRouter, and Custom providers.
 */

/**
 * Supported LLM providers
 */
export type LLMProvider = 'openai' | 'anthropic' | 'gemini' | 'ollama' | 'openrouter' | 'custom';

/**
 * Base configuration shared by all providers
 */
export interface BaseProviderConfig {
  provider: LLMProvider;
  model: string;
  temperature?: number;
  maxTokens?: number;
}

/**
 * OpenAI specific configuration
 */
export interface OpenAIConfig extends BaseProviderConfig {
  provider: 'openai';
  apiKey: string;
  model: string;
  baseUrl?: string;
}

/**
 * Anthropic (Claude) configuration
 */
export interface AnthropicConfig extends BaseProviderConfig {
  provider: 'anthropic';
  apiKey: string;
  model: string;
}

/**
 * Google Gemini configuration
 */
export interface GeminiConfig extends BaseProviderConfig {
  provider: 'gemini';
  apiKey: string;
  model: string;
}

/**
 * Ollama configuration
 */
export interface OllamaConfig extends BaseProviderConfig {
  provider: 'ollama';
  baseUrl?: string;
  model: string;
}

/**
 * OpenRouter configuration
 */
export interface OpenRouterConfig extends BaseProviderConfig {
  provider: 'openrouter';
  apiKey: string;
  model: string;
}

/**
 * Union type for all provider configs
 */
export type ProviderConfig = 
  | OpenAIConfig 
  | AnthropicConfig 
  | GeminiConfig 
  | OllamaConfig 
  | OpenRouterConfig;

/**
 * Settings stored in localStorage
 */
export interface LLMSettings {
  provider: LLMProvider;
  apiKey?: string;
  baseUrl?: string;
  model: string;
  maxTokens: number;
  temperature: number;
  enableSemantic: boolean;
}

/**
 * Default settings
 */
export function getDefaultSettings(): LLMSettings {
  return {
    provider: 'ollama',
    model: '',
    maxTokens: 4096,
    temperature: 0.7,
    enableSemantic: true,
  };
}

/**
 * Available models per provider
 */
export const AVAILABLE_MODELS: Record<LLMProvider, string[]> = {
  openai: ['gpt-4o', 'gpt-4o-mini', 'gpt-4-turbo', 'gpt-4', 'gpt-3.5-turbo'],
  anthropic: ['claude-sonnet-4-20250514', 'claude-3-5-sonnet-20241022', 'claude-3-opus-20240229'],
  gemini: ['gemini-2.0-flash-exp', 'gemini-1.5-pro', 'gemini-1.5-flash'],
  ollama: ['llama3.2', 'llama3.1', 'codellama', 'mistral', 'qwen2.5'],
  openrouter: ['openai/gpt-4o', 'anthropic/claude-3.5-sonnet', 'google/gemini-2.0-flash-exp', 'meta-llama/llama-3.1-70b-instruct'],
  custom: [], // User-defined models
};

/**
 * Provider display names
 */
export const PROVIDER_LABELS: Record<LLMProvider, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic (Claude)',
  gemini: 'Google Gemini',
  ollama: 'Ollama (Local)',
  openrouter: 'OpenRouter',
  custom: 'Custom (OpenAI Compatible)',
};

/**
 * Check if provider requires API key
 */
export function requiresApiKey(provider: LLMProvider): boolean {
  return provider !== 'ollama';
}

/**
 * Check if provider requires base URL
 */
export function requiresBaseUrl(provider: LLMProvider): boolean {
  return provider === 'custom' || provider === 'ollama';
}

/**
 * Stream chunk from agent
 */
export interface AgentStreamChunk {
  type: 'token' | 'tool_start' | 'tool_end' | 'error' | 'done' | 'thinking' | 'heartbeat';
  content?: string;
  toolName?: string;
  toolInput?: Record<string, unknown>;
  toolOutput?: string;
  durationMs?: number; // Tool execution duration in milliseconds
  error?: string;
  errorType?: string; // Error type: rate_limit, auth_error, server_error, unknown
  retryable?: boolean; // Whether the error is retryable
  modifiedFiles?: string[]; // Files modified by tool (e.g., write_file, replace_file)
}