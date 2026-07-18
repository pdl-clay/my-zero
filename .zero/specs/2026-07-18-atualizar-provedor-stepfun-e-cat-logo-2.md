# Especificação: Provedor StepFun e entradas de catálogo (atualizado)

## 1. Objetivo

Adicionar suporte ao provedor **StepFun** no `my-zero`, reaproveitando o adaptador **OpenAI-compatible**, mapeando corretamente o esforço de pensamento (`thinking_effort`) para `reasoning_effort` da API StepFun, suportando múltiplas URLs base (API normal e Step Plan) e registrando os modelos principais no catálogo.

## 2. Arquivos e componentes relevantes

- `internal/providers/stepfun/provider.go` — novo pacote do provedor.
- `internal/providers/openai/` — adaptador base reutilizado.
- `internal/config/types.go` — registro de `ProviderKindStepFun`.
- `internal/modelregistry/catalog.go` — entradas de catálogo dos modelos StepFun.
- `internal/modelregistry/catalog_test.go` — testes do catálogo.
- `internal/providers/stepfun/provider_test.go` — testes do provedor.

## 3. Implementação proposta

### 3.1 Provedor StepFun
- Criar `internal/providers/stepfun/provider.go`.
- Reutilizar o adaptador OpenAI-compatible para transporte HTTP.
- **URLs base configuráveis**:
  - Padrão API: `https://api.stepfun.com/v1`
  - Step Plan: `https://api.stepfun.ai/step_plan/v1`
  - Permitir seleção via configuração ou variável de ambiente.
- Mapear `thinking_effort` → `reasoning_effort`:
  - `low` → `"minimal"`
  - `medium` → `"medium"`
  - `high` → `"high"`
- Header de autenticação: `Authorization: Bearer <api_key>` (ou `x-api-key` conforme documentação StepFun).
- Tratar erros 4xx específicos de reasoning como erros de configuração de esforço.

### 3.2 Registro do provedor
- Em `internal/config/types.go`, adicionar `ProviderKindStepFun = "stepfun"` na enumeração de provedores.

### 3.3 Catálogo de modelos
- Em `internal/modelregistry/catalog.go`, adicionar entradas (lista baseada em conhecimento atual; **verificar contra documentação StepFun para confirmar disponibilidade**):
  - `stepfun-step-1-8k` — 8k contexto, custo baixo, thinking_effort médio.
  - `stepfun-step-1-32k` — 32k contexto, custo médio, thinking_effort médio.
  - `stepfun-step-1-128k` — 128k contexto, custo médio, thinking_effort alto.
  - `stepfun-step-1-256k` — 256k contexto, custo alto, thinking_effort alto.
  - `stepfun-step-2-16k` — 16k contexto, custo baixo, thinking_effort médio.
  - `stepfun-step-2-flash` — 128k contexto, custo baixo, thinking_effort baixo.
  - `stepfun-step-3` — 128k contexto, custo médio, thinking_effort alto.
  - `stepfun-step-3-flash` — 128k contexto, custo médio-baixo, thinking_effort médio.
  - `stepfun-step-3.5-flash` — 128k contexto, custo baixo, thinking_effort médio.
  - `stepfun-step-3o` — 128k contexto, custo médio-alto, thinking_effort alto (modelo omni).
- Cada entrada deve incluir: `id`, `name`, `context_window`, `pricing` (input/output por 1k tokens), `thinking_effort` padrão, `base_url` (API normal ou Step Plan).

### 3.4 Tratamento OAuth
- Suportar `api_key` direto via header Authorization.
- Não suportar OAuth no momento (sem workspace adicional).

## 4. Testes e verificação

- Testar factory do provedor (`NewProvider` retorna StepFunProvider).
- Testar transporte HTTP: URL base configurável, endpoint `/chat/completions`.
- Testar mapeamento de `thinking_effort` → `reasoning_effort`.
- Testar erro handling: 401 (chave inválida), 400 (parâmetro inválido), timeout.
- Testar catálogo: resolver cada modelo para configuração correta.
- Rodar `go test ./...`, `go fmt ./...` e `go vet ./...`.

## 5. Riscos e casos de borda

- **Modelos desatualizados**: a lista acima pode não refletir lançamentos recentes; verificar documentação oficial da StepFun antes de finalizar.
- Mudança futura na API StepFun (nomes de parâmetros, headers).
- Falta de OAuth limitada para cenários enterprise.
- Diferenças de comportamento entre modelos flash e full em relação a reasoning.
- Seleção incorreta de URL base (API normal vs Step Plan) causando erros de roteamento.

## 6. Fora do escopo

- Suporte a OAuth / service account StepFun.
- Suporte a visão (multimodal) neste momento.
- Modificação de adaptadores de provedores existentes.
- Pesquisa automática de modelos via API ( scraping não implementado).

## Nota importante

Esta especificação pressupõe uma lista de modelos baseada em conhecimento anterior. **Antes de implementar, confirme a lista atualizada de modelos StepFun na documentação oficial** para evitar mapear modelos descontinuados ou faltar lançamentos recentes.
