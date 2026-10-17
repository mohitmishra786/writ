// AG Studio LLM adapter for Groq's OpenAI-compatible Responses API, reached
// through Mandate's server proxy (POST /v1/ai/responses) so the key never
// reaches the browser. Written against the AgLlmAdapter contract in
// ag-studio 3.0.0 (dist/types/api/ai/agLlmAdapter.d.ts, agAiMessage.d.ts,
// agAiProtocol.d.ts).
//
// Studio's conversation items are not raw OpenAI items: they carry a `kind`,
// camelCase `callId`, and `{type:'text'}` content parts. This adapter
// translates both directions. The request is non-streaming; the completed
// output is replayed as AG-UI *_CHUNK events, which the docs state Studio
// reads interchangeably with start/content/end sequences.

import type {
  AgAiConversationItem,
  AgAiEvent,
  AgAiOutputItem,
  AgLlmAdapter,
  AgLlmRequest,
  AgLlmResponse,
  AgLlmResponseHandler,
} from 'ag-studio';

type OpenAiItem = Record<string, unknown>;

function toOpenAiInput(items: AgAiConversationItem[]): OpenAiItem[] {
  const out: OpenAiItem[] = [];
  for (const item of items) {
    switch (item.type) {
      case 'message':
        if (item.kind === 'input') {
          const text = item.content.filter((c) => c.type === 'text').map((c) => (c as { text: string }).text).join('\n');
          out.push({ type: 'message', role: item.role, content: [{ type: 'input_text', text }] });
        } else {
          const text = item.content.map((c) => (c.type === 'text' ? c.text : c.refusal)).join('\n');
          out.push({ type: 'message', role: 'assistant', content: [{ type: 'output_text', text }] });
        }
        break;
      case 'function_call':
        out.push({ type: 'function_call', call_id: item.callId, name: item.name, arguments: item.arguments });
        break;
      case 'function_call_output':
        out.push({ type: 'function_call_output', call_id: item.callId, output: item.output });
        break;
      case 'reasoning':
        // Reasoning is display-only; it is not replayed to the provider.
        break;
    }
  }
  return out;
}

function toOpenAiBody(request: AgLlmRequest): Record<string, unknown> {
  const body: Record<string, unknown> = {
    input: toOpenAiInput(request.input),
    instructions: request.instructions,
  };
  if (request.model?.id) body.model = request.model.id;
  if (request.tools?.length) {
    body.tools = request.tools.map((t) =>
      t.kind === 'provided' ? t.provider : { type: 'function', name: t.name, description: t.description, parameters: t.parameters },
    );
  }
  const choice = request.toolChoice;
  if (choice) body.tool_choice = typeof choice === 'string' ? choice : { type: 'function', name: choice.name };
  if (request.responseFormat?.type === 'json') {
    body.text = {
      format: { type: 'json_schema', name: request.responseFormat.name, schema: request.responseFormat.schema, strict: false },
    };
  }
  return body;
}

interface OpenAiResponse {
  id?: string;
  status?: string;
  model?: string;
  output?: Array<Record<string, any>>;
  error?: { code?: string; message?: string } | null;
  incomplete_details?: { reason?: string } | null;
  usage?: { input_tokens?: number; output_tokens?: number; total_tokens?: number; output_tokens_details?: { reasoning_tokens?: number } };
}

function fromOpenAiOutput(output: Array<Record<string, any>>): { items: AgAiOutputItem[]; events: AgAiEvent[] } {
  const items: AgAiOutputItem[] = [];
  const events: AgAiEvent[] = [];
  for (const o of output) {
    const id: string = o.id ?? crypto.randomUUID();
    if (o.type === 'message') {
      const text = (o.content ?? []).filter((c: any) => c.type === 'output_text').map((c: any) => c.text).join('');
      items.push({ id, kind: 'output', type: 'message', role: 'assistant', status: 'completed', content: [{ type: 'text', text, annotations: [] }] });
      events.push({ type: 'TEXT_MESSAGE_CHUNK', messageId: id, role: 'assistant', delta: text } as AgAiEvent);
    } else if (o.type === 'function_call') {
      items.push({ id, kind: 'output', type: 'function_call', callId: o.call_id, name: o.name, arguments: o.arguments ?? '{}', status: 'completed' });
      events.push({ type: 'TOOL_CALL_CHUNK', toolCallId: o.call_id, toolCallName: o.name, delta: o.arguments ?? '{}' } as AgAiEvent);
    } else if (o.type === 'reasoning') {
      const text = (o.content ?? []).map((c: any) => c.text ?? '').join('');
      items.push({ id, kind: 'output', type: 'reasoning', summary: [], content: text ? [{ type: 'text', text }] : [] });
      if (text) events.push({ type: 'REASONING_MESSAGE_CHUNK', messageId: id, role: 'reasoning', delta: text } as AgAiEvent);
    }
  }
  return { items, events };
}

const RESPONSE_STATUSES = ['completed', 'failed', 'in_progress', 'cancelled', 'queued', 'incomplete'] as const;

export function groqAdapter(endpoint: string): AgLlmAdapter {
  return {
    executeTurn(request, options): AgLlmResponseHandler {
      const result: Promise<{ response: AgLlmResponse; events: AgAiEvent[] }> = (async () => {
        const failed = (code: string, message: string) => ({
          response: { id: crypto.randomUUID(), createdAt: Date.now(), status: 'failed' as const, output: [], error: { code, message } },
          events: [] as AgAiEvent[],
        });
        let res: Response;
        try {
          res = await fetch(endpoint, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(toOpenAiBody(request)),
            signal: options?.signal,
          });
        } catch (e) {
          if (options?.signal?.aborted) {
            return { response: { id: crypto.randomUUID(), createdAt: Date.now(), status: 'cancelled' as const, output: [] }, events: [] };
          }
          return failed('network_error', String(e));
        }
        const data: OpenAiResponse = await res.json().catch(() => ({}));
        if (!res.ok) {
          return failed(String(res.status), data.error?.message ?? `AI request failed (${res.status})`);
        }
        const { items, events } = fromOpenAiOutput(data.output ?? []);
        const status = (RESPONSE_STATUSES as readonly string[]).includes(data.status ?? '') ? (data.status as AgLlmResponse['status']) : 'completed';
        const response: AgLlmResponse = {
          id: data.id ?? crypto.randomUUID(),
          createdAt: Date.now(),
          status,
          output: items,
          model: data.model,
          usage: data.usage
            ? {
                inputTokens: data.usage.input_tokens ?? 0,
                outputTokens: data.usage.output_tokens ?? 0,
                totalTokens: data.usage.total_tokens,
                reasoningTokens: data.usage.output_tokens_details?.reasoning_tokens,
              }
            : undefined,
        };
        if (data.error?.message) response.error = { code: data.error.code ?? 'provider_error', message: data.error.message };
        if (data.incomplete_details?.reason === 'max_output_tokens' || data.incomplete_details?.reason === 'content_filter') {
          response.incompleteDetails = { reason: data.incomplete_details.reason };
        }
        return { response, events };
      })();

      return {
        stream: {
          async *[Symbol.asyncIterator]() {
            const { events } = await result;
            for (const e of events) yield e;
          },
        },
        complete: result.then((r) => r.response),
      };
    },
  };
}
