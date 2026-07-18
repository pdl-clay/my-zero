# Especificação: Provedor StepFun e entradas de catálogo — versão atualizada

## 1. Objetivo

Adicionar suporte ao provedor **StepFun** no `my-zero`, reaproveitando o adaptador **OpenAI-compatible**, mapeando corretamente o esforço de pensamento (`thinking_effort`) para `reasoning_effort` da API StepFun e registrando os modelos principais no catálogo, incluindo `stepfun-step-3.5-flash` e `stepfun-step-3.7-flash`.

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
- Suportar duas famílias de URL base:
  - API padrão: `https://api.stepfun.com/v1`
  - StepPlan: `https://api.stepfun.ai/step_plan/v1`
- Mapear:
  - `thinking_effort: low` → `reasoning_effort: "minimal"`
  - `thinking_effort: medium` → `reasoning_effort: "medium"`
  - `thinking_effort: high` → `reasoning_effort: "high"`
- Adicionar header `Authorization: Bearer <api_key>`.
- Tratar erros 4xx específicos de reasoning como erros de configuração de esforço.

### 3.2 Registro do provedor
- Em `internal/config/types.go`, adicionar `ProviderKindStepFun = "stepfun"` na enumeração de provedores.

### 3.3 Catálogo de modelos
- Em `internal/modelregistry/catalog.go`, adicionar entradas:
  - `stepfun-step-2-16k`
  - `stepfun-step-2-flash`
  - `stepfun-step-3`
  - `stepfun-step-3-flash`
  - `stepfun-step-3.5-flash`
  - `stepfun-step-3.7-flash`

### 3.4 Tratamento OAuth
- Suportar `api_key` direto via header Authorization.
- Não suportar OAuth no momento.

## 4. Testes e verificação

- Testar factory do provedor (`NewProvider` retorna StepFunProvider).
- Testar transporte HTTP: URL base padrão e StepPlan, endpoint `/chat/completions`.
- Testar mapeamento de `thinking_effort` → `reasoning_effort`.
- Testar erro handling: 401 (chave inválida), 400 (parâmetro inválido), timeout.
- Testar catálogo: resolver cada modelo para configuração correta.
- Rodar `go test ./...`, `go fmt ./...` e `go vet ./...`.

## 5. Riscos e casos de borda

- Mudança futura na API StepFun (nomes de parâmetros, headers).
- Modelos descontinuados sem aviso prévio.
- Falta de OAuth limitada para cenários enterprise.
- Diferenças de comportamento entre modelos flash e full em relação a reasoning.
- Documentação pública da StepFun indisponível no momento da escrita deste plano.

## 6. Fora do escopo

- Suporte a OAuth / service account StepFun.
- Suporte a visão (multimodal) neste momento.
- Modificação de adaptadores de provedores existentes.
