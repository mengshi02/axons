import React, { useState, useRef, useEffect, useCallback } from 'react';
import {
  X, Send, Loader2, Sparkles, MessageSquare, Search,
  Code, FileText, ChevronDown, Plus, AlertTriangle,
  Layers, ShieldCheck, GitBranch, Code2, Bot, Trash2,
  History, PenSquare, Brain, Image,
  Database, Terminal, TestTube2, Cloud, Cpu, Globe, FlaskConical,
  Microscope, Zap, Package, Puzzle, BookOpen, Network, Wand2,
  Fingerprint, Lock, Workflow, BarChart3, Settings, RefreshCw,
  Compass, Square, Wrench,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import i18n from '../i18n';
import { ConfirmDialog } from './ConfirmDialog';
import { Modal } from './Modal';
import { useAppState } from '../hooks/useAppState';
import type { PanelComponentProps } from '../lib/panelRegistry';
import { semanticSearch, triggerEmbed, listChatSessions } from '../services/api';
import type { SemanticSearchResult } from '../services/api';
import {
  chatWithAgent,
  chatSimple,
  type AgentStreamChunk,
} from '../llm';
import { ChangeList } from './ChangeList';
import { ChatComposer, type ChatComposerHandle } from './chat/ChatComposer';
import { MessageItem } from './chat/MessageItem';
import { invalidateMarkdown } from './chat/markdownCache';

/**
 * Filter out context markers from message content for display
 * Context markers like [Context: Looking at ...] are useful for AI but should not be shown to users
 */
function filterContextMarker(content: string): string {
  // Match [Context: ...] at the beginning of the message, followed by optional newlines
  const contextPattern = /^\[Context:.*?\]\s*\n*/;
  return content.replace(contextPattern, '');
}

interface Message {
  id: string;
  role: 'user' | 'assistant';
  type: 'text' | 'thinking' | 'tool'; // Message type
  content: string;
  timestamp: Date;
  // For tool messages
  toolName?: string;
  toolInput?: Record<string, unknown>;
  toolOutput?: string;
  toolStatus?: 'running' | 'done' | 'error';
  toolDurationMs?: number;
  // For user messages with images
  images?: string[]; // base64 dataUrl list (user messages only)
  // For error messages
  errorType?: string; // rate_limit, auth_error, server_error, unknown
  retryable?: boolean; // Whether the error is retryable
}

interface Conversation {
  id: string;
  title: string;
  agentId: string;
  messages: Message[];
  createdAt: Date;
  updatedAt: Date;
}

const ICON_MAP: Record<string, React.ComponentType<{ className?: string }>> = {
  sparkles: Sparkles,
  layers: Layers,
  'shield-check': ShieldCheck,
  'git-branch': GitBranch,
  'code-2': Code2,
  bot: Bot,
  database: Database,
  terminal: Terminal,
  'test-tube-2': TestTube2,
  cloud: Cloud,
  cpu: Cpu,
  globe: Globe,
  flask: FlaskConical,
  microscope: Microscope,
  zap: Zap,
  package: Package,
  puzzle: Puzzle,
  'book-open': BookOpen,
  network: Network,
  'wand-2': Wand2,
  fingerprint: Fingerprint,
  lock: Lock,
  workflow: Workflow,
  'bar-chart-3': BarChart3,
  compass: Compass,
};

function AgentIcon({ icon, className }: { icon: string; className?: string }) {
  const Icon = ICON_MAP[icon] || Bot;
  return <Icon className={className} />;
}

function generateConvId(): string {
  return `conv-${Date.now()}-${Math.random().toString(36).substring(2, 7)}`;
}

function generateTitle(firstMessage: string): string {
  // Filter out context marker before generating title
  const cleanMessage = filterContextMarker(firstMessage);
  return cleanMessage.length > 30
    ? cleanMessage.slice(0, 30) + '...'
    : cleanMessage || i18n.t('chat:newConversation');
}

// LocalStorage key for conversations (base key, project ID will be appended)
const CONVERSATIONS_KEY_BASE = 'axons-conversations';

// Helper function for localStorage persistence (project-scoped)
function getStorageKey(projectId: string | undefined): string {
  return projectId ? `${CONVERSATIONS_KEY_BASE}-${projectId}` : CONVERSATIONS_KEY_BASE;
}

function saveConversationsToStorage(projectId: string | undefined, conversations: Conversation[], currentConvId: string): void {
  try {
    const key = getStorageKey(projectId);
    localStorage.setItem(key, JSON.stringify({ conversations, currentConvId }));
  } catch (e) {
    console.error('Failed to save conversations to localStorage:', e);
  }
}

export const RightPanel = React.memo(function RightPanel({ onClose }: PanelComponentProps) {
  const { t } = useTranslation('chat');
  const {
    graph, selectedNode,
    agents, currentAgentId, setCurrentAgentId, currentProject,
    clearFileCache, configVersion,
    openPanels,
  } = useAppState();

  const [activeTab, setActiveTab] = useState<'chat' | 'search'>('chat');

  // LLM models for model selector
  interface LLMModelInfo { id: string; name: string; provider: string; model: string; multimodal: boolean; }
  const [llmModels, setLLMModels] = useState<LLMModelInfo[]>([]);
  const [selectedModelId, setSelectedModelId] = useState<string>('');
  const [modelDropdownOpen, setModelDropdownOpen] = useState(false);
  const modelDropdownRef = useRef<HTMLDivElement>(null);

  // Image attachment state
  const [attachedImages, setAttachedImages] = useState<{ dataUrl: string; mimeType: string }[]>([]);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);

  // Multi-conversation state
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [currentConvId, setCurrentConvId] = useState<string>('');
  const [showHistory, setShowHistory] = useState(false);
  const [deleteConvId, setDeleteConvId] = useState<string | null>(null);
  const [changeListRefreshKey, setChangeListRefreshKey] = useState(0); // Used to trigger ChangeList refresh

  // Input state is held inside ChatComposer (self-managed) to avoid re-rendering
  // the whole chat panel on every keystroke. We only track an "isEmpty" boolean
  // here so the Send button's disabled state can flip.
  const composerRef = useRef<ChatComposerHandle>(null);
  const [composerEmpty, setComposerEmpty] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const abortControllerRef = useRef<AbortController | null>(null);
  // Semantic search state
  const [semanticQuery, setSemanticQuery] = useState('');
  const [semanticResults, setSemanticResults] = useState<SemanticSearchResult[]>([]);
  const [isSemanticSearching, setIsSemanticSearching] = useState(false);
  const [embeddingStatus, setEmbeddingStatus] = useState<{
    configured: boolean;
    embedding_count: number;
    needs_reembedding: boolean;
    model: string;
    status: string;
  } | null>(null);
  const [embeddingStatusLoading, setEmbeddingStatusLoading] = useState(false);
  const [isTriggeringEmbed, setIsTriggeringEmbed] = useState(false);
  const [embedMessage, setEmbedMessage] = useState<string | null>(null);
  const [agentDropdownOpen, setAgentDropdownOpen] = useState(false);
  const [showAgentManager, setShowAgentManager] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const historyRef = useRef<HTMLDivElement>(null);

  const currentAgent = agents.find(a => a.id === currentAgentId) || agents[0];

  const selectedModel = llmModels.find(m => m.id === selectedModelId) || llmModels[0] || null;
  const isMultimodal = selectedModel?.multimodal ?? false;

  // Load LLM models (refresh when panel opens or config changes)
  useEffect(() => {
    fetch('/api/llm-models').then(r => r.json()).then(data => {
      const models: LLMModelInfo[] = data.models || [];
      setLLMModels(models);
      if (models.length > 0 && !models.find(m => m.id === selectedModelId)) {
        setSelectedModelId(models[0].id);
      }
    }).catch(() => { });
  }, [configVersion]); // eslint-disable-line react-hooks/exhaustive-deps

  // Close model dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (modelDropdownRef.current && !modelDropdownRef.current.contains(e.target as Node)) {
        setModelDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  // Get current conversation
  const currentConversation = conversations.find(c => c.id === currentConvId);
  const messages = currentConversation?.messages || [];

  // Create a new conversation
  const createNewConversation = useCallback((agentId?: string) => {
    const id = generateConvId();
    const newConv: Conversation = {
      id,
      title: t('newConversation'),
      agentId: agentId || currentAgentId,
      messages: [],
      createdAt: new Date(),
      updatedAt: new Date(),
    };
    setConversations(prev => [newConv, ...prev]);
    setCurrentConvId(id);
    setShowHistory(false);
    return id;
  }, [currentAgentId]);

  // Load conversations from backend when project changes
  const lastLoadedProjectRef = useRef<string | null | undefined>(undefined);
  const [isLoadingSessions, setIsLoadingSessions] = useState(false);

  useEffect(() => {
    const projectId = currentProject?.id;

    // Treat undefined (no project selected yet) as null
    // Still proceed to load sessions even if projectId is undefined
    const effectiveProjectId = projectId || null;

    // Only load when project changes or on first mount
    // Use undefined as initial state so first mount always triggers load
    if (lastLoadedProjectRef.current !== effectiveProjectId) {
      lastLoadedProjectRef.current = effectiveProjectId;

      // Load sessions from backend
      setIsLoadingSessions(true);
      // Use empty string if projectId is undefined to match backend expectation
      const apiProjectId = projectId || '';
      console.log('[RightPanel] Loading sessions for project:', apiProjectId);
      listChatSessions(apiProjectId).then((sessions) => {
        console.log('[RightPanel] Loaded sessions:', sessions);
        if (sessions.length > 0) {
          // Convert backend sessions to frontend conversations
          // Messages are now included in the response
          const convs = sessions.map((session) => {
            const messages = session.messages || [];
            console.log('[RightPanel] Session:', session.session_id, 'agent_id:', session.agent_id, 'messages:', messages.length);
            return {
              id: session.session_id,
              title: generateTitleFromMessages(messages),
              agentId: session.agent_id || 'default', // Restore agent_id from backend
              messages: messages.map((msg: { role: string; content: string; created_at?: string }, idx: number) => ({
                id: `${session.session_id}-${idx}`,
                role: msg.role as 'user' | 'assistant',
                type: 'text' as const, // Historical messages are text type
                content: msg.content,
                timestamp: new Date(msg.created_at || session.created_at),
              })),
              createdAt: new Date(session.created_at),
              updatedAt: new Date(session.updated_at),
            };
          });

          console.log('[RightPanel] Setting conversations:', convs);
          setConversations(convs);
          setCurrentConvId(sessions[0].session_id);
        } else {
          // No sessions in backend for this project, create a new conversation
          setConversations([]);
          setCurrentConvId('');
          createNewConversation();
        }
      }).catch((err) => {
        console.error('Failed to load sessions from backend:', err);
        // On error, create a new conversation instead of using stale localStorage data
        setConversations([]);
        setCurrentConvId('');
        createNewConversation();
      }).finally(() => {
        setIsLoadingSessions(false);
      });
    }
  }, [currentProject?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // Helper function to generate title from messages
  function generateTitleFromMessages(messages: { role: string; content: string }[]): string {
    const firstUserMsg = messages.find(m => m.role === 'user');
    if (firstUserMsg) {
      // Filter out context marker before generating title
      const cleanContent = filterContextMarker(firstUserMsg.content);
      return cleanContent.length > 30
        ? cleanContent.slice(0, 30) + '...'
        : cleanContent;
    }
    return t('newConversation');
  }

  // Persist conversations to localStorage whenever they change (as backup)
  useEffect(() => {
    // Only save after initial load is complete (lastLoadedProjectRef is not null or undefined)
    // and we have a valid project context
    if (conversations.length > 0 && lastLoadedProjectRef.current !== undefined && !isLoadingSessions) {
      saveConversationsToStorage(currentProject?.id, conversations, currentConvId);
    }
  }, [conversations, currentConvId, currentProject?.id, isLoadingSessions]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // Scroll to bottom when panel becomes visible (e.g. user switches back from fileTree)
  const isPanelVisible = openPanels.has('rightPanel');
  useEffect(() => {
    if (isPanelVisible && messages.length > 0) {
      // Use requestAnimationFrame to ensure DOM has been made visible first
      requestAnimationFrame(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      });
    }
  }, [isPanelVisible]); // eslint-disable-line react-hooks/exhaustive-deps

  // Close dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setAgentDropdownOpen(false);
      }
      if (historyRef.current && !historyRef.current.contains(e.target as Node)) {
        setShowHistory(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  // Clear messages when agent changes (new conversation)
  const handleSelectAgent = (id: string) => {
    setCurrentAgentId(id);
    setAgentDropdownOpen(false);
    createNewConversation(id);
  };

  // Switch to a conversation
  const handleSwitchConversation = (convId: string) => {
    setCurrentConvId(convId);
    setShowHistory(false);
    // Sync agent to conversation's agent
    const conv = conversations.find(c => c.id === convId);
    if (conv) {
      setCurrentAgentId(conv.agentId);
    }
  };

  // Delete a conversation
  const handleDeleteConversation = (convId: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setDeleteConvId(convId);
  };

  const confirmDeleteConversation = () => {
    if (!deleteConvId) return;
    const convId = deleteConvId;
    setDeleteConvId(null);
    setConversations(prev => {
      const target = prev.find(c => c.id === convId);
      // Drop cached markdown renderings for messages of the deleted conversation.
      if (target) {
        target.messages.forEach(m => invalidateMarkdown(m.id));
      }
      const updated = prev.filter(c => c.id !== convId);
      if (convId === currentConvId) {
        if (updated.length > 0) {
          setCurrentConvId(updated[0].id);
          setCurrentAgentId(updated[0].agentId);
        } else {
          // Create a new one if all deleted
          const id = generateConvId();
          const newConv: Conversation = {
            id,
            title: t('newConversation'),
            agentId: currentAgentId,
            messages: [],
            createdAt: new Date(),
            updatedAt: new Date(),
          };
          setCurrentConvId(id);
          return [newConv];
        }
      }
      return updated;
    });
  };

  const handleImageAttach = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || []);
    files.forEach(file => {
      const reader = new FileReader();
      reader.onload = ev => {
        const dataUrl = ev.target?.result as string;
        setAttachedImages(prev => [...prev, { dataUrl, mimeType: file.type }]);
      };
      reader.readAsDataURL(file);
    });
    if (fileInputRef.current) fileInputRef.current.value = '';
  };

  const handleRemoveImage = (index: number) => {
    setAttachedImages(prev => prev.filter((_, i) => i !== index));
  };

  // Cancel ongoing request
  const handleCancel = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
    setIsLoading(false);
  };

  const handleSend = async () => {
    const rawInput = composerRef.current?.getValue() ?? '';
    const trimmedInput = rawInput.trim();
    if (!trimmedInput || isLoading) return;

    // Ensure there's a current conversation
    let convId = currentConvId;
    if (!convId || !conversations.find(c => c.id === convId)) {
      convId = createNewConversation();
    }

    // Snapshot images before clearing
    const snapshotImages = attachedImages.map(img => img.dataUrl);

    const userMessage: Message = {
      id: Date.now().toString(),
      role: 'user',
      type: 'text',
      content: trimmedInput,
      timestamp: new Date(),
      images: snapshotImages.length > 0 ? snapshotImages : undefined,
    };

    // Text to send to agent
    const agentMessage = trimmedInput;

    // Update conversation title from first user message
    setConversations(prev => prev.map(c => {
      if (c.id === convId) {
        const isFirstMessage = c.messages.length === 0;
        return {
          ...c,
          title: isFirstMessage ? generateTitle(userMessage.content) : c.title,
          messages: [...c.messages, userMessage],
          updatedAt: new Date(),
        };
      }
      return c;
    }));

    composerRef.current?.clear();
    setComposerEmpty(true);
    setAttachedImages([]);
    setIsLoading(true);

    // Helper to add a new message to conversation
    const addMessage = (msg: Message) => {
      setConversations(prev => prev.map(c => {
        if (c.id === convId) {
          return {
            ...c,
            messages: [...c.messages, msg],
            updatedAt: new Date(),
          };
        }
        return c;
      }));
    };

    // Helper to update an existing message
    const updateMessage = (msgId: string, updater: (msg: Message) => Message) => {
      setConversations(prev => prev.map(c => {
        if (c.id === convId) {
          return {
            ...c,
            messages: c.messages.map(m => m.id === msgId ? updater(m) : m),
            updatedAt: new Date(),
          };
        }
        return c;
      }));
    };

    // Track current text message being built
    let currentTextMsgId: string | null = null;
    let currentTextContent = '';

    // Create abort controller for this request
    const abortController = new AbortController();
    abortControllerRef.current = abortController;

    try {
      let messageWithContext = agentMessage;
      if (graph && selectedNode) {
        const node = graph.nodes.find(n => n.id === selectedNode);
        if (node) {
          const name = node.properties.name || node.id;
          const type = node.label;
          const filePath = node.properties.filePath || '';
          messageWithContext = `[Context: Looking at ${name} (${type}) in ${filePath}]\n\n${userMessage.content}`;
        }
      }

      await chatWithAgent(
        messageWithContext,
        (chunk: AgentStreamChunk) => {
          if (chunk.type === 'thinking' && chunk.content) {
            // Skip thinking events
          } else if (chunk.type === 'token' && chunk.content) {
            // Clear thinking state when receiving tokens
            // Append to current text message or create new one
            if (!currentTextMsgId) {
              currentTextMsgId = `${Date.now()}-text`;
              const textMsg: Message = {
                id: currentTextMsgId,
                role: 'assistant',
                type: 'text',
                content: chunk.content,
                timestamp: new Date(),
              };
              addMessage(textMsg);
              currentTextContent = chunk.content;
            } else {
              currentTextContent += chunk.content;
              updateMessage(currentTextMsgId, m => ({ ...m, content: currentTextContent }));
            }
          } else if (chunk.type === 'tool_start' && chunk.toolName) {
            // Add tool start message
            const toolMsg: Message = {
              id: `${Date.now()}-tool-${chunk.toolName}`,
              role: 'assistant',
              type: 'tool',
              content: '',
              timestamp: new Date(),
              toolName: chunk.toolName,
              toolInput: chunk.toolInput,
              toolStatus: 'running',
            };
            addMessage(toolMsg);
            // Reset text tracking when tool starts
            currentTextMsgId = null;
            currentTextContent = '';
          } else if (chunk.type === 'tool_end' && chunk.toolName) {
            // Update tool message with result
            setConversations(prev => prev.map(c => {
              if (c.id === convId) {
                return {
                  ...c,
                  messages: c.messages.map(m => {
                    if (m.type === 'tool' && m.toolName === chunk.toolName && m.toolStatus === 'running') {
                      return {
                        ...m,
                        toolOutput: chunk.toolOutput,
                        toolStatus: (chunk.toolOutput || '').startsWith('Error:') ? 'error' : 'done',
                        toolDurationMs: chunk.durationMs,
                      };
                    }
                    return m;
                  }),
                  updatedAt: new Date(),
                };
              }
              return c;
            }));
            // Handle modified files: clear cache and refresh change list
            if (chunk.modifiedFiles && chunk.modifiedFiles.length > 0) {
              chunk.modifiedFiles.forEach((filePath: string) => {
                clearFileCache(filePath);
              });
              // Trigger ChangeList refresh
              setChangeListRefreshKey(prev => prev + 1);
            }
          } else if (chunk.type === 'error') {
            // Add error message with friendly display
            const errorType = chunk.errorType || 'unknown';
            const retryable = chunk.retryable || false;

            // Friendly error messages based on error type
            let friendlyMsg = chunk.error || '未知错误';
            if (errorType === 'rate_limit') {
              friendlyMsg = '请求频率超限，请稍后重试';
            } else if (errorType === 'auth_error') {
              friendlyMsg = '认证失败，请检查 API Key 设置';
            } else if (errorType === 'server_error') {
              friendlyMsg = '服务器内部错误，请稍后重试';
            }

            const errorMsg: Message = {
              id: `${Date.now()}-error`,
              role: 'assistant',
              type: 'text',
              content: friendlyMsg,
              timestamp: new Date(),
              errorType,
              retryable,
            };
            addMessage(errorMsg);
          }
        },
        convId,
        currentAgentId,
        currentProject?.id,
        snapshotImages.length > 0 ? snapshotImages : undefined,
        selectedModelId,
        abortController.signal,
      );
    } catch (err) {
      // Check if this was a user-initiated abort
      if (err instanceof Error && err.name === 'AbortError') {
        const cancelMsg: Message = {
          id: `${Date.now()}-cancel`,
          role: 'assistant',
          type: 'text',
          content: '[Request cancelled by user]',
          timestamp: new Date(),
        };
        addMessage(cancelMsg);
        return;
      }
      try {
        const response = await chatSimple(userMessage.content);
        const fallbackMsg: Message = {
          id: `${Date.now()}-fallback`,
          role: 'assistant',
          type: 'text',
          content: response,
          timestamp: new Date(),
        };
        addMessage(fallbackMsg);
      } catch {
        const errorMsg: Message = {
          id: `${Date.now()}-error`,
          role: 'assistant',
          type: 'text',
          content: t('llmError'),
          timestamp: new Date(),
        };
        addMessage(errorMsg);
      }
    } finally {
      setIsLoading(false);
      abortControllerRef.current = null;
    }
  };

  // Load embedding status when switching to search tab
  const loadEmbeddingStatus = useCallback(async () => {
    setEmbeddingStatusLoading(true);
    try {
      const res = await fetch('/v1/settings/check');
      if (res.ok) {
        const data = await res.json();
        setEmbeddingStatus(data);
      }
    } catch {
      setEmbeddingStatus(null);
    } finally {
      setEmbeddingStatusLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeTab === 'search') {
      loadEmbeddingStatus();
    }
  }, [activeTab]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleSemanticSearch = async () => {
    if (!semanticQuery.trim() || isSemanticSearching) return;
    setIsSemanticSearching(true);
    try {
      const data = await semanticSearch(semanticQuery.trim(), 20, currentProject?.id);
      setSemanticResults(data.results);
    } catch {
      setSemanticResults([]);
    } finally {
      setIsSemanticSearching(false);
    }
  };

  const handleTriggerEmbedding = async () => {
    setIsTriggeringEmbed(true);
    setEmbedMessage(null);
    try {
      await triggerEmbed({
        strategy: 'incremental',
        projectId: currentProject?.id,
      });
      setEmbedMessage(t('embedStarted'));
      // Reload status after a short delay
      setTimeout(() => loadEmbeddingStatus(), 2000);
    } catch (err) {
      setEmbedMessage(err instanceof Error ? err.message : t('embedFailed'));
    } finally {
      setIsTriggeringEmbed(false);
    }
  };

  // RightPanel is always rendered when open (App.tsx controls visibility via panelRegistry)
  const selectedNodeName = selectedNode && graph
    ? graph.nodes.find(n => n.id === selectedNode)?.properties.name
    : undefined;

  // Stable callbacks for MessageItem props (referential equality matters for memo).
  const handleImageClick = useCallback((src: string) => setLightboxSrc(src), []);

  // Refs to read latest state inside stable callbacks (avoid recreating callbacks per render).
  const conversationsRef = useRef(conversations);
  conversationsRef.current = conversations;
  const currentConvIdRef = useRef(currentConvId);
  currentConvIdRef.current = currentConvId;

  const handleRetry = useCallback((errorMessageId: string) => {
    const conv = conversationsRef.current.find(c => c.id === currentConvIdRef.current);
    if (!conv) return;
    const errorIdx = conv.messages.findIndex(m => m.id === errorMessageId);
    const lastUserMsg = conv.messages
      .slice(0, errorIdx)
      .reverse()
      .find(m => m.role === 'user');
    if (lastUserMsg) {
      composerRef.current?.setValue(lastUserMsg.content);
      composerRef.current?.focus();
      setComposerEmpty(lastUserMsg.content.trim().length === 0);
    }
  }, []);

  // Identify the streaming message (the assistant text message currently being built):
  // it's the last assistant text message while isLoading. Only this one renders without cache.
  let streamingMessageId: string | null = null;
  if (isLoading) {
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i];
      if (m.role === 'assistant' && m.type === 'text') {
        streamingMessageId = m.id;
        break;
      }
      // Stop if we hit a user message — no streaming text yet
      if (m.role === 'user') break;
    }
  }

  return (
    <div
      className="w-full h-full bg-surface flex flex-col relative"
    >
      {/* Header */}
      <div className="relative flex items-center justify-between px-3 py-2 h-[38px] border-b border-border-subtle">
        {/* Agent selector */}
        <div className="relative flex-1" ref={dropdownRef}>
          <button
            onClick={() => setAgentDropdownOpen(v => !v)}
            className="flex items-center gap-1.5 px-2 py-1 rounded hover:bg-hover transition-colors text-xs font-medium text-text-primary max-w-[200px]"
          >
            {currentAgent && (
              <AgentIcon icon={currentAgent.icon} className="w-3.5 h-3.5 text-accent shrink-0" />
            )}
            <span className="truncate">{currentAgent?.name || 'AI Assistant'}</span>
            <ChevronDown className="w-3 h-3 text-text-muted shrink-0" />
          </button>

          {agentDropdownOpen && (
            <div className="absolute top-full left-0 mt-1 w-64 bg-elevated border border-border-subtle rounded-lg shadow-lg z-50 overflow-hidden">
              <div className="p-1.5 max-h-72 overflow-y-auto">
                {agents.map(agent => (
                  <button
                    key={agent.id}
                    onClick={() => handleSelectAgent(agent.id)}
                    className={`w-full flex items-start gap-2.5 px-2.5 py-2 rounded text-left transition-colors ${agent.id === currentAgentId ? 'bg-accent/10 text-accent' : 'hover:bg-hover text-text-primary'
                      }`}
                  >
                    <AgentIcon icon={agent.icon} className="w-4 h-4 mt-0.5 shrink-0" />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-1">
                        <span className="text-sm font-medium truncate">{agent.name}</span>
                        {agent.is_builtin && (
                          <span className="text-xs px-1 py-0.5 bg-accent/10 text-accent rounded shrink-0">built-in</span>
                        )}
                      </div>
                      <p className="text-xs text-text-muted truncate">{agent.description}</p>
                    </div>
                  </button>
                ))}
              </div>
              <div className="border-t border-border-subtle p-1.5">
                <button
                  onClick={() => { setAgentDropdownOpen(false); setShowAgentManager(true); }}
                  className="w-full flex items-center gap-2 px-2.5 py-2 rounded text-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors"
                >
                  <Plus className="w-3.5 h-3.5" />
                  Customize Agent
                </button>
              </div>
            </div>
          )}
        </div>

        {/* History button + New chat button */}
        <div className="flex items-center gap-1 shrink-0 relative" ref={historyRef}>
          <button
            onClick={() => setShowHistory(v => !v)}
            title={t('chatHistory')}
            className={`p-1 rounded transition-colors ${showHistory ? 'text-accent bg-accent/10' : 'text-text-muted hover:text-text-primary hover:bg-hover'}`}
          >
            <History className="w-3.5 h-3.5" />
          </button>

          {showHistory && (
            <div className="absolute top-full right-0 mt-1 w-72 bg-elevated border border-border-subtle rounded-lg shadow-lg z-50 overflow-hidden">
              <div className="flex items-center justify-between px-3 py-2 border-b border-border-subtle">
                <span className="text-xs font-medium text-text-secondary">Chat History</span>
                <button
                  onClick={() => createNewConversation()}
                  className="flex items-center gap-1 text-xs text-accent hover:text-accent/80 transition-colors"
                >
                  <Plus className="w-3 h-3" />
                  New
                </button>
              </div>
              <div className="max-h-80 overflow-y-auto">
                {conversations.length === 0 ? (
                  <div className="px-3 py-4 text-xs text-text-muted text-center">No conversations yet</div>
                ) : (
                  conversations.map(conv => (
                    <button
                      key={conv.id}
                      onClick={() => handleSwitchConversation(conv.id)}
                      className={`w-full flex items-center gap-2 px-3 py-2.5 text-left transition-colors group ${conv.id === currentConvId
                        ? 'bg-accent/10 text-accent'
                        : 'hover:bg-hover text-text-primary'
                        }`}
                    >
                      <MessageSquare className="w-3.5 h-3.5 shrink-0 opacity-60" />
                      <div className="flex-1 min-w-0">
                        <div className="text-sm truncate">{conv.title}</div>
                        <div className="flex items-center gap-1 mt-0.5 min-w-0">
                          {(() => {
                            const agent = agents.find(a => a.id === conv.agentId);
                            return agent ? (
                              <span className="flex items-center gap-0.5 shrink-0 text-xs text-accent/70">
                                <AgentIcon icon={agent.icon} className="w-2.5 h-2.5" />
                                <span className="max-w-[70px] truncate">{agent.name}</span>
                              </span>
                            ) : null;
                          })()}
                          <span className="text-xs text-text-muted truncate">
                            · {conv.messages.length} msgs · {formatRelativeTime(conv.updatedAt)}
                          </span>
                        </div>
                      </div>
                      <button
                        onClick={(e) => handleDeleteConversation(conv.id, e)}
                        className="p-1 rounded opacity-0 group-hover:opacity-100 hover:bg-red-500/20 hover:text-red-400 transition-all"
                      >
                        <Trash2 className="w-3 h-3" />
                      </button>
                    </button>
                  ))
                )}
              </div>
            </div>
          )}

          <button
            onClick={() => createNewConversation()}
            title="New chat"
            className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors"
          >
            <PenSquare className="w-3.5 h-3.5" />
          </button>

          <button
            onClick={onClose}
            className="p-1 text-text-muted hover:text-text-primary hover:bg-hover rounded transition-colors"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-border-subtle">
        <button
          onClick={() => setActiveTab('chat')}
          className={`flex-1 flex items-center justify-center gap-1.5 px-4 py-1.5 text-xs font-medium transition-colors ${activeTab === 'chat' ? 'text-accent border-b-2 border-accent bg-accent/5' : 'text-text-muted hover:text-text-secondary'
            }`}
        >
          <MessageSquare className="w-3.5 h-3.5" />
          Chat
        </button>
        <button
          onClick={() => setActiveTab('search')}
          className={`flex-1 flex items-center justify-center gap-1.5 px-4 py-1.5 text-xs font-medium transition-colors ${activeTab === 'search' ? 'text-accent border-b-2 border-accent bg-accent/5' : 'text-text-muted hover:text-text-secondary'
            }`}
        >
          <Search className="w-3.5 h-3.5" />
          Semantic
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {activeTab === 'chat' && (
          <>
            {/* File change list - placed above messages */}
            <ChangeList
              sessionId={currentConvId}
              projectId={currentProject?.id || ''}
              refreshKey={changeListRefreshKey}
            />

            {/* Messages */}
            <div className="flex-1 overflow-y-auto p-4 space-y-4 scrollbar-thin">
              {messages.length === 0 && (
                <div className="flex flex-col items-center justify-center h-full text-center">
                  <div className="w-12 h-12 rounded-full bg-accent/10 flex items-center justify-center mb-3">
                    {currentAgent ? (
                      <AgentIcon icon={currentAgent.icon} className="w-6 h-6 text-accent" />
                    ) : (
                      <Sparkles className="w-6 h-6 text-accent" />
                    )}
                  </div>
                  <h3 className="text-sm font-medium text-text-primary mb-1">
                    {currentAgent?.name || t('defaultAgent.name')}
                  </h3>
                  <p className="text-xs text-text-muted max-w-[240px]">
                    {currentAgent?.description || t('defaultAgent.description')}
                  </p>
                </div>
              )}

              {messages.map(message => (
                <MessageItem
                  key={message.id}
                  message={message}
                  isStreaming={message.id === streamingMessageId}
                  t={t}
                  onImageClick={handleImageClick}
                  onRetry={handleRetry}
                />
              ))}

              <div ref={messagesEndRef} />
            </div>

            {/* Input */}
            <div className="p-3 border-t border-border-subtle">
              {/* Real-time execution status bar */}
              {isLoading && (() => {
                const lastMessage = messages[messages.length - 1];
                const isToolRunning = lastMessage?.type === 'tool' && lastMessage.toolStatus === 'running';

                // Show tool status or thinking status
                return (
                  <div className="flex items-center gap-2 mb-2 px-2 py-1.5 bg-elevated rounded text-xs text-text-muted">
                    {isToolRunning ? (
                      <>
                        <Loader2 className="w-3 h-3 animate-spin text-accent" />
                        <Wrench className="w-3 h-3 text-accent" />
                        <span className="text-accent font-medium">{lastMessage.toolName}</span>
                        <span>running...</span>
                      </>
                    ) : (
                      <>
                        <Brain className="w-3 h-3 animate-pulse text-accent" />
                        <span className="text-text-secondary">Executing task...</span>
                      </>
                    )}
                  </div>
                );
              })()}

              {selectedNodeName && (
                <div className="flex items-center gap-2 mb-2 px-2 py-1.5 bg-elevated rounded text-xs text-text-muted">
                  <Code className="w-3 h-3" />
                  <span className="truncate">Context: {selectedNodeName}</span>
                </div>
              )}

              {/* Composer box */}
              <div className="rounded-xl border border-border-subtle bg-elevated focus-within:border-accent transition-colors">
                {/* Attached image previews */}
                {attachedImages.length > 0 && (
                  <div className="flex gap-1.5 p-2 pb-0 flex-wrap">
                    {attachedImages.map((img, i) => (
                      <div key={i} className="relative group">
                        <img
                          src={img.dataUrl}
                          alt={`attachment ${i + 1}`}
                          className="w-8 h-8 object-cover rounded-md border border-border-subtle cursor-zoom-in"
                          onClick={() => setLightboxSrc(img.dataUrl)}
                        />
                        <button
                          onClick={() => handleRemoveImage(i)}
                          className="absolute -top-1 -right-1 w-3.5 h-3.5 bg-red-500 text-white rounded-full text-[10px] flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity leading-none"
                        >×</button>
                      </div>
                    ))}
                  </div>
                )}

                {/* Textarea */}
                <ChatComposer
                  ref={composerRef}
                  onSubmit={() => handleSend()}
                  onEmptyChange={setComposerEmpty}
                  disabled={isLoading}
                  placeholder={t('placeholder')}
                  rows={3}
                  className="w-full px-3 pt-3 pb-1 bg-transparent text-sm text-text-primary placeholder:text-text-muted resize-none focus:outline-none"
                />

                {/* Bottom toolbar */}
                <div className="flex items-center gap-1.5 px-2 pb-2">
                  {/* Model selector */}
                  {llmModels.length > 0 && (
                    <div className="relative" ref={modelDropdownRef}>
                      <button
                        onClick={() => setModelDropdownOpen(v => !v)}
                        className="flex items-center gap-1 px-2 py-1 text-xs text-text-muted rounded-lg hover:bg-hover hover:text-text-primary transition-colors"
                      >
                        <ChevronDown className="w-3 h-3" />
                        <span className="max-w-[90px] truncate">{selectedModel ? (selectedModel.name || selectedModel.model) : 'Model'}</span>
                        {isMultimodal && <span className="px-1 py-0.5 bg-accent/15 text-accent rounded text-[10px]">vision</span>}
                      </button>
                      {modelDropdownOpen && (
                        <div className="absolute bottom-full left-0 mb-1 w-52 bg-elevated border border-border-subtle rounded-lg shadow-lg z-50 overflow-hidden">
                          {llmModels.map(m => (
                            <button
                              key={m.id}
                              onClick={() => { setSelectedModelId(m.id); setModelDropdownOpen(false); }}
                              className={`w-full flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors ${m.id === selectedModelId ? 'bg-accent/10 text-accent' : 'hover:bg-hover text-text-primary'}`}
                            >
                              <span className="truncate flex-1">{m.name || m.model}</span>
                              {m.multimodal && <span className="text-[10px] px-1 py-0.5 bg-accent/15 text-accent rounded shrink-0">vision</span>}
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Image attach (multimodal only) */}
                  {isMultimodal && (
                    <>
                      <input ref={fileInputRef} type="file" accept="image/*" multiple onChange={handleImageAttach} className="hidden" />
                      <button
                        onClick={() => fileInputRef.current?.click()}
                        title={t('attachImage')}
                        className="p-1.5 text-text-muted hover:text-text-primary hover:bg-hover rounded-lg transition-colors"
                      >
                        <Image className="w-3.5 h-3.5" />
                      </button>
                    </>
                  )}

                  <div className="flex-1" />

                  {/* Send / Cancel */}
                  {isLoading ? (
                    <button
                      onClick={handleCancel}
                      className="p-1.5 bg-accent text-white rounded-lg hover:bg-accent/80 transition-colors animate-pulse"
                      title={t('stopGenerating')}
                    >
                      <Square className="w-3.5 h-3.5" fill="currentColor" />
                    </button>
                  ) : (
                    <button
                      onClick={handleSend}
                      disabled={composerEmpty}
                      className="p-1.5 bg-accent text-white rounded-lg disabled:opacity-40 disabled:cursor-not-allowed hover:bg-accent/90 transition-colors"
                    >
                      <Send className="w-3.5 h-3.5" />
                    </button>
                  )}
                </div>
              </div>
            </div>
          </>
        )}

        {activeTab === 'search' && (
          <>
            {/* Embedding status check */}
            {embeddingStatusLoading ? (
              <div className="flex-1 flex items-center justify-center">
                <Loader2 className="w-5 h-5 animate-spin text-accent" />
              </div>
            ) : !embeddingStatus?.configured ? (
              /* Not configured */
              <div className="flex-1 flex flex-col items-center justify-center p-6 text-center gap-4">
                <div className="w-12 h-12 rounded-full bg-yellow-500/10 flex items-center justify-center">
                  <AlertTriangle className="w-6 h-6 text-yellow-400" />
                </div>
                <div>
                  <p className="text-sm font-medium text-text-primary mb-1">Embedding model not configured</p>
                  <p className="text-xs text-text-muted max-w-[220px]">
                    Semantic search requires vector embeddings. Please configure an Embedding model in Settings first.
                  </p>
                </div>
                <button
                  onClick={() => { window.dispatchEvent(new CustomEvent('open-settings', { detail: { tab: 'embedding' } })); }}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-accent border border-accent/40 rounded-lg hover:bg-accent/10 transition-colors"
                >
                  <Settings className="w-3.5 h-3.5" />
                  Go to Settings &gt; Embedding
                </button>
              </div>
            ) : embeddingStatus.embedding_count === 0 ? (
              /* Configured but no embeddings yet */
              <div className="flex-1 flex flex-col items-center justify-center p-6 text-center gap-4">
                <div className="w-12 h-12 rounded-full bg-accent/10 flex items-center justify-center">
                  <Database className="w-6 h-6 text-accent" />
                </div>
                <div>
                  <p className="text-sm font-medium text-text-primary mb-1">No embeddings yet</p>
                  <p className="text-xs text-text-muted max-w-[220px]">
                    Embedding model is configured but no vectors have been generated. Trigger embedding to get started.
                  </p>
                </div>
                {embedMessage && (
                  <p className="text-xs text-accent bg-accent/10 px-3 py-1.5 rounded-lg">{embedMessage}</p>
                )}
                <button
                  onClick={handleTriggerEmbedding}
                  disabled={isTriggeringEmbed}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-white bg-accent rounded-lg hover:bg-accent/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                >
                  {isTriggeringEmbed
                    ? <><Loader2 className="w-3.5 h-3.5 animate-spin" />Triggering...</>
                    : <><RefreshCw className="w-3.5 h-3.5" />Trigger Embedding</>
                  }
                </button>
                <p className="text-[11px] text-text-muted">
                  You can monitor progress in Settings &gt; Embedding
                </p>
              </div>
            ) : (
              /* Ready: show semantic search */
              <>
                {/* Re-embedding warning */}
                {embeddingStatus.needs_reembedding && (
                  <div className="flex items-center gap-2 px-3 py-2 bg-yellow-500/10 border-b border-yellow-500/20 text-xs text-yellow-300">
                    <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
                    <span className="flex-1">Embedding model has changed. Re-embedding is recommended for best results.</span>
                    <button
                      onClick={handleTriggerEmbedding}
                      disabled={isTriggeringEmbed}
                      className="shrink-0 px-1.5 py-0.5 rounded border border-yellow-400/40 hover:bg-yellow-400/10 transition-colors disabled:opacity-50"
                    >
                      {isTriggeringEmbed ? <Loader2 className="w-3 h-3 animate-spin" /> : 'Re-embed'}
                    </button>
                  </div>
                )}

                {/* Search input */}
                <div className="p-3 border-b border-border-subtle">
                  <div className="flex items-center gap-2">
                    <input
                      type="text"
                      value={semanticQuery}
                      onChange={e => setSemanticQuery(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && handleSemanticSearch()}
                      placeholder={t('searchPlaceholder')}
                      className="flex-1 px-3 py-2 bg-elevated border border-border-subtle rounded-lg text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/20 transition-all"
                    />
                    <button
                      onClick={handleSemanticSearch}
                      disabled={!semanticQuery.trim() || isSemanticSearching}
                      className="p-2 bg-accent text-white rounded-lg disabled:opacity-50 disabled:cursor-not-allowed hover:bg-accent/90 transition-colors"
                    >
                      {isSemanticSearching ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
                    </button>
                  </div>
                  <p className="text-[11px] text-text-muted mt-1.5">
                    Describe functionality or logic in natural language, and semantic search will find relevant code.
                  </p>
                </div>

                {/* Results */}
                <div className="flex-1 overflow-y-auto p-3 space-y-2">
                  {semanticResults.length === 0 && semanticQuery && !isSemanticSearching && (
                    <div className="text-center text-text-muted text-sm py-8">
                      <Search className="w-8 h-8 mx-auto mb-2 opacity-40" />
                      <p>No relevant results found</p>
                    </div>
                  )}
                  {semanticResults.map(result => (
                    <div
                      key={result.id}
                      className="p-3 bg-elevated rounded-lg border border-border-subtle hover:border-accent/50 transition-colors cursor-pointer"
                      onClick={() => {
                        window.dispatchEvent(new CustomEvent('navigate-to-file', {
                          detail: { path: result.file, line: result.line }
                        }));
                      }}
                    >
                      <div className="flex items-center gap-2 mb-1">
                        <FileText className="w-3.5 h-3.5 text-accent flex-shrink-0" />
                        <span className="font-medium text-text-primary text-sm flex-1 truncate">{result.name}</span>
                        <span className="text-xs px-1.5 py-0.5 bg-accent/10 text-accent rounded flex-shrink-0">{result.kind}</span>
                        {/* Relevance score */}
                        <span className="text-[10px] text-text-muted flex-shrink-0" title={t('relevanceScore')}>
                          {result.score.toFixed(2)}
                        </span>
                      </div>
                      <div className="text-xs text-text-muted truncate">
                        {result.file}:{result.line}
                        {result.end_line && result.end_line > result.line && `-${result.end_line}`}
                      </div>
                      {result.content && (
                        <div className="mt-1.5 text-xs text-text-secondary font-mono bg-surface px-2 py-1 rounded truncate">
                          {result.content.slice(0, 150)}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </>
            )}
          </>
        )}
      </div>

      {/* Agent Manager Modal */}
      {showAgentManager && (
        <AgentManagerModal onClose={() => { setShowAgentManager(false); }} />
      )}

      {/* Lightbox */}
      <Modal isOpen={!!lightboxSrc} onClose={() => setLightboxSrc(null)} overlayOpacity="none" backdropBlur={false} size="full" closeOnOverlayClick={true}>
        {lightboxSrc && (
          <img
            src={lightboxSrc}
            alt="preview"
            className="max-w-[90vw] max-h-[90vh] rounded-lg shadow-2xl object-contain cursor-zoom-out"
          />
        )}
      </Modal>

      {/* Delete Conversation Confirm Dialog */}
      <ConfirmDialog
        isOpen={deleteConvId !== null}
        title={t('deleteConversation')}
        message={t('confirmDelete')}
        confirmLabel="Delete"
        onConfirm={confirmDeleteConversation}
        onCancel={() => setDeleteConvId(null)}
      />
    </div>
  );
});

// ---- Helpers ----
function formatRelativeTime(date: Date): string {
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  return `${diffDays}d ago`;
}

// ---- Inline Agent Manager Modal ----
import { AgentManagerPanel } from './AgentManagerPanel';

function AgentManagerModal({ onClose }: { onClose: () => void }) {
  return (
    <Modal isOpen={true} onClose={onClose} size="md" overlayOpacity="none" backdropBlur={false} className="h-[80vh] flex flex-col">
      <AgentManagerPanel onClose={onClose} />
    </Modal>
  );
}