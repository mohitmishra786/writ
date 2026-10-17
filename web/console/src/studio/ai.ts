// Studio Agent Framework wiring: Studio's built-in agents (the "Writ
// Auditor" assistant) answer questions about the ledger by building widgets.
// Turns go through the Responses adapter to Writ's server proxy.

import { createAiHarness, type AgAiHarnessSetup, type AgAiModel, type AgAiPromptStarter } from 'ag-studio';
import { API_BASE } from '../api';
import { groqAdapter } from '../ai/groqAdapter';

const adapter = groqAdapter(`${API_BASE}/v1/ai/responses`);

// The server picks the route (Claude, then Gemini, NVIDIA and Groq as fallbacks; see
// internal/api/ai.go), so the panel offers one entry.
const MODELS: AgAiModel[] = [{ id: 'auto', label: 'Claude Haiku 5.5' }];

const PROMPT_STARTERS: AgAiPromptStarter[] = [
  { label: 'What was blocked?', prompt: 'Add a grid of every blocked agent intent with its payee, amount and reasons.' },
  { label: 'Spend by supplier', prompt: 'Add a chart of captured spend by payee.' },
  { label: 'Explain the data', prompt: 'What tables and fields does this dashboard have, and what does each one track?' },
];

export const aiSetup: AgAiHarnessSetup = ({ api }) =>
  createAiHarness(api, { adapter, models: MODELS, promptStarters: PROMPT_STARTERS });
