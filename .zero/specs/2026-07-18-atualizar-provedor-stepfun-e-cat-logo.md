# Especificação: Provedor StepFun e entradas de catálogo

## 1. Objetivo

Atualizar o suporte ao provedor **StepFun** no `my-zero`, reaproveitando o adaptador **OpenAI-compatible**, mapeando corretamente o esforço de pensamento (`thinking_effort`) para `reasoning_effort` da API StepFun e registrando/ajustando os modelos no catálogo.

## 2. Arquivos e componentes relevantes

- `internal/providercatalog/catalog.go` — ajustar a URL base padrão do StepFun para `https://api.stepfun.com/v1` (API normal) e adicionar suporte ao endpoint de planos `https://api.stepfun.ai/step_plan/v1`.
- `internal/providers/stepfun/provider.go` — novo pacote do provedor.
- `internal/providers/openai/` — adaptador base reutilizado.
- `internal/config/types.go` — registro de `ProviderKindStepFun`.
- `internal/modelregistry/catalog.go` — entradas de catálogo dos modelos StepFun.
- `internal/modelregistry/catalog_test.go` — testes do catálogo.
- `internal/providers/stepfun/provider_test.go` — testes do provedor.
- `internal/providermodelcatalog/catalog.go` — garantir que o StepFun apareça no catálogo de modelos.

## 3. Implementação proposta

### 3.1 Catálogo de provedores
- Em `internal/providercatalog/catalog.go`, atualizar o descritor `stepfun`:
  - `DefaultBaseURL`: alterar de `https://api.stepfun.ai/v1` para `https://api.stepfun.com/v1`.
  - Manter `Transport: TransportOpenAICompatible`, `AuthEnvVars: []string{"STEPFUN_API_KEY"}` e aliases `"step fun"`, `"step-fun"`.
- Adicionar um novo descritor `stepfun-plan` para o endpoint de planos:
  - `ID: "stepfun-plan"`
  - `Name: "StepFun AI Plan"`
  - `DefaultBaseURL: "https://api.stepfun.ai/step_plan/v1"`
  - `Transport: TransportOpenAICompatible`
  - `AuthEnvVars: []string{"STEPFUN_API_KEY"}`
  - Aliases: `"step fun plan"`, `"stepfun ai plan"`

### 3.2 Provedor StepFun
- Criar `internal/providers/stepfun/provider.go`.
- Reutilizar o adaptador OpenAI-compatible para transporte HTTP.
- Mapear:
  - `thinking_effort: low` → `reasoning_effort: "minimal"`
  - `thinking_effort: medium` → `reasoning_effort: "medium"`
  - `thinking_effort: high` → `reasoning_effort: "high"`
- Adicionar header `Authorization: Bearer <api_key>`.
- Tratar erros 4xx específicos de reasoning como erros de configuração de esforço.

### 3.3 Registro do provedor
- Em `internal/config/types.go`, adicionar `ProviderKindStepFun = "stepfun"` na enumeração de provedores.

### 3.4 Catálogo de modelos
- Em `internal/modelregistry/catalog.go`, adicionar entradas:
  - `stepfun-step-2-16k` — 16k contexto, custo baixo, thinking_effort médio.
  - `stepfun-step-2-flash` — 128k contexto, custo baixo, thinking_effort baixo.
  - `stepfun-step-3` — 128k contexto, custo médio, thinking_effort alto.
  - `stepfun-step-3-flash` — 128k contexto, custo médio-baixo, thinking_effort médio.
  - `stepfun-step-3.5-flash` — 128k contexto, custo baixo, thinking_effort médio.
  - com limites de contexto, custos e esforços de pensamento coerentes.

### 3.5 Tratamento OAuth
- Suportar `api_key` direto via header Authorization.
- Não suportar OAuth no momento (sem workspace adicional).

## 4. Testes e verificação

- Testar factory do provedor (`NewProvider` retorna StepFunProvider).
- Testar transporte HTTP: URL base `https://api.stepfun.com/v1` (normal) e `https://api.stepfun.ai/step_plan/v1` (plan), endpoint `/chat/completions`.
- Testar mapeamento de `thinking_effort` → `reasoning_effort`.
- Testar erro handling: 401 (chave inválida), 400 (parâmetro inválido), timeout.
- Testar catálogo: resolver cada modelo para configuração correta.
- Rodar `go test ./...`, `go fmt ./...` e `go vet ./...`.

## 5. Riscos e casos de borda

- Mudança futura na API StepFun (nomes de parâmetros, headers).
- Modelos descontinuados sem aviso prévio.
- Falta de OAuth limitada para cenários enterprise.
- Diferenças de comportamento entre modelos flash e full em relação a reasoning.
- A troca de domínio da API (`stepfun.ai` → `stepfun.com`) pode quebrar configurações existentes que usam a URL antiga; preservar compatibilidade com `profile.BaseURL` explícito.

## 6. Fora do escopo

- Suporte a OAuth / service account StepFun.
- Suporte a visão (multimodal) neste momento.
- Modificação de adaptadores de provedores existentes.
